package paginate

import "strings"

// searchTerms returns what to search for: the words of the term with multiWord, otherwise the term
// as a whole; nil without a search.
func searchTerms(search string, multiWord bool) []string {
	if search == "" {
		return nil
	}
	if multiWord {
		return strings.Fields(search)
	}
	return []string{search}
}

// likeEscaper makes user input literal inside LIKE/ILIKE patterns: backslash is the default
// escape character of Postgres, so no ESCAPE clause is needed.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern matches value anywhere in the text.
func containsPattern(value string) string {
	return "%" + likeEscaper.Replace(value) + "%"
}

// prefixPattern matches text that starts with value.
func prefixPattern(value string) string {
	return likeEscaper.Replace(value) + "%"
}
