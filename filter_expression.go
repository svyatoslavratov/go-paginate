package paginate

import "strings"

// A filter expression is the filter=<expression> parameter of nestjs-paginate 15+:
//
//	filter=(status=$in:paid,shipped OR total=$gte:1000) AND NOT comment=$null
//
// Grammar (precedence NOT > AND > OR, keywords case-insensitive):
//
//	expr    := or
//	or      := and (OR and)*
//	and     := not (AND not)*
//	not     := NOT not | primary
//	primary := '(' expr ')' | term
//	term    := field '=' value      // value is a filter value: [$not:][$operator:]value
//
// Tokens are separated by whitespace; '(' and ')' stand alone. A value with whitespace, parentheses
// or a keyword is quoted with " or '; inside quotes a backslash escapes a quote or a backslash, other
// backslashes are literal. Every term, AND, OR, NOT and parenthesized group counts toward
// Config.MaxFilterExpressionComplexity, which bounds the recursion of the parser.

type filterNodeKind int

const (
	filterTerm filterNodeKind = iota
	filterAnd
	filterOr
	filterNot
)

// filterNode is a node of a parsed filter expression.
type filterNode struct {
	kind filterNodeKind
	// children are the operands of AND and OR (two or more) and of NOT (one).
	children []*filterNode
	// field and value are the API field and the filter value of a term.
	field string
	value string
}

type expressionTokenKind int

const (
	tokenTerm expressionTokenKind = iota
	tokenLeftParen
	tokenRightParen
	tokenAnd
	tokenOr
	tokenNot
)

type expressionToken struct {
	kind expressionTokenKind
	text string
}

func (t expressionToken) String() string {
	switch t.kind {
	case tokenLeftParen:
		return "("
	case tokenRightParen:
		return ")"
	case tokenAnd:
		return "AND"
	case tokenOr:
		return "OR"
	case tokenNot:
		return "NOT"
	}
	return t.text
}

func expressionError(format string, args ...any) error {
	return invalidRequest(CodeInvalidExpression, "", format, args...)
}

func isExpressionSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// tokenizeFilterExpression splits an expression into terms, keywords and parentheses. It works on
// bytes: every special character is ASCII, and UTF-8 never encodes other characters with ASCII bytes.
func tokenizeFilterExpression(input string) ([]expressionToken, error) {
	var tokens []expressionToken
	for i := 0; i < len(input); {
		c := input[i]
		switch {
		case isExpressionSpace(c):
			i++
			continue
		case c == '(':
			tokens = append(tokens, expressionToken{kind: tokenLeftParen})
			i++
			continue
		case c == ')':
			tokens = append(tokens, expressionToken{kind: tokenRightParen})
			i++
			continue
		}

		var term strings.Builder
		quoted := false
		for i < len(input) {
			c := input[i]
			if isExpressionSpace(c) || c == '(' || c == ')' {
				break
			}
			if c != '"' && c != '\'' {
				term.WriteByte(c)
				i++
				continue
			}
			quote := c
			quoted = true
			i++
			for i < len(input) && input[i] != quote {
				if input[i] == '\\' && i+1 < len(input) {
					if next := input[i+1]; next == '"' || next == '\'' || next == '\\' {
						term.WriteByte(next)
						i += 2
						continue
					}
				}
				term.WriteByte(input[i])
				i++
			}
			if i >= len(input) {
				return nil, expressionError("unterminated quote in filter expression")
			}
			i++
		}

		text := term.String()
		if !quoted {
			switch strings.ToUpper(text) {
			case "AND":
				tokens = append(tokens, expressionToken{kind: tokenAnd})
				continue
			case "OR":
				tokens = append(tokens, expressionToken{kind: tokenOr})
				continue
			case "NOT":
				tokens = append(tokens, expressionToken{kind: tokenNot})
				continue
			}
		}
		tokens = append(tokens, expressionToken{kind: tokenTerm, text: text})
	}
	return tokens, nil
}

// parseFilterExpression parses an expression into a tree of at most maxComplexity nodes.
func parseFilterExpression(input string, maxComplexity uint64) (*filterNode, error) {
	tokens, err := tokenizeFilterExpression(input)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, expressionError("empty filter expression")
	}
	p := &expressionParser{tokens: tokens, maxComplexity: maxComplexity}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.tokens) {
		return nil, expressionError("unexpected %q after the end of the filter expression", p.tokens[p.pos].String())
	}
	return node, nil
}

type expressionParser struct {
	tokens        []expressionToken
	pos           int
	complexity    uint64
	maxComplexity uint64
}

func (p *expressionParser) peek() (expressionToken, bool) {
	if p.pos >= len(p.tokens) {
		return expressionToken{}, false
	}
	return p.tokens[p.pos], true
}

// spend charges one node and rejects the expression past the cap, before it can nest deeper.
func (p *expressionParser) spend() error {
	p.complexity++
	if p.complexity > p.maxComplexity {
		return expressionError("filter expression is too complex (max %d nodes)", p.maxComplexity)
	}
	return nil
}

func (p *expressionParser) parseOr() (*filterNode, error) {
	return p.parseChain(tokenOr, filterOr, p.parseAnd)
}

func (p *expressionParser) parseAnd() (*filterNode, error) {
	return p.parseChain(tokenAnd, filterAnd, p.parseNot)
}

// parseChain parses operands joined by one keyword into a single node.
func (p *expressionParser) parseChain(keyword expressionTokenKind, kind filterNodeKind, operand func() (*filterNode, error)) (*filterNode, error) {
	first, err := operand()
	if err != nil {
		return nil, err
	}
	children := []*filterNode{first}
	for {
		token, ok := p.peek()
		if !ok || token.kind != keyword {
			break
		}
		p.pos++
		if err := p.spend(); err != nil {
			return nil, err
		}
		next, err := operand()
		if err != nil {
			return nil, err
		}
		children = append(children, next)
	}
	if len(children) == 1 {
		return first, nil
	}
	return &filterNode{kind: kind, children: children}, nil
}

func (p *expressionParser) parseNot() (*filterNode, error) {
	if token, ok := p.peek(); ok && token.kind == tokenNot {
		p.pos++
		if err := p.spend(); err != nil {
			return nil, err
		}
		child, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &filterNode{kind: filterNot, children: []*filterNode{child}}, nil
	}
	return p.parsePrimary()
}

func (p *expressionParser) parsePrimary() (*filterNode, error) {
	token, ok := p.peek()
	if !ok {
		return nil, expressionError("unexpected end of filter expression")
	}
	switch token.kind {
	case tokenLeftParen:
		p.pos++
		if err := p.spend(); err != nil {
			return nil, err
		}
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if closing, ok := p.peek(); !ok || closing.kind != tokenRightParen {
			return nil, expressionError(`expected ")" in filter expression`)
		}
		p.pos++
		return node, nil
	case tokenTerm:
		p.pos++
		if err := p.spend(); err != nil {
			return nil, err
		}
		field, value, found := strings.Cut(token.text, "=")
		if !found || field == "" {
			return nil, expressionError(`invalid filter expression term %q, expected "field=value"`, token.text)
		}
		return &filterNode{kind: filterTerm, field: field, value: value}, nil
	}
	return nil, expressionError("unexpected %q in filter expression", token.String())
}

// render turns the tree into SQL. Terms are always checked, Lenient or not: dropping a term would
// change what the expression means.
func (n *filterNode) render(e *Endpoint) (sqlExpr, error) {
	switch n.kind {
	case filterNot:
		child, err := n.children[0].render(e)
		if err != nil {
			return sqlExpr{}, err
		}
		return sqlExpr{sql: "NOT " + child.group(), args: child.args}, nil
	case filterAnd, filterOr:
		children := make([]sqlExpr, 0, len(n.children))
		for _, child := range n.children {
			expr, err := child.render(e)
			if err != nil {
				return sqlExpr{}, err
			}
			children = append(children, expr)
		}
		if n.kind == filterAnd {
			return joinExprs(children, " AND "), nil
		}
		return joinExprs(children, " OR "), nil
	}

	field, exists := e.fields[n.field]
	if !exists {
		return sqlExpr{}, invalidRequest(CodeUnknownField, n.field, "unknown filter field %s", n.field)
	}
	if field.Filter == nil {
		return sqlExpr{}, invalidRequest(CodeFilterNotAllowed, n.field, "filter on %s is not allowed", n.field)
	}
	condition, err := parseFilterCondition(n.value, field, e.cfg.MaxFilterValues, true)
	if err != nil {
		return sqlExpr{}, err
	}
	return conditionExpr(field.Column, condition, e.cfg.CompareAsText)
}
