package paginate

import (
	"slices"
	"strings"
)

// searchColumns returns the columns the search term is matched against: the searchBy columns of
// the request narrowed to SearchableColumns, or all SearchableColumns when the request has no
// searchBy or IgnoreSearchByInQueryParam is set.
func searchColumns(searchBy []string, cfg *Config) []string {
	if len(cfg.SearchableColumns) == 0 {
		return nil
	}
	if len(searchBy) == 0 || cfg.IgnoreSearchByInQueryParam {
		return slices.Clone(cfg.SearchableColumns)
	}
	columns := make([]string, 0, len(searchBy))
	for _, column := range searchBy {
		if slices.Contains(cfg.SearchableColumns, column) && !slices.Contains(columns, column) {
			columns = append(columns, column)
		}
	}
	return columns
}

// searchTerms returns what to search for: the words of the term with MultiWordSearch, otherwise
// the term as a whole; nil without a search.
func searchTerms(search string, cfg *Config) []string {
	if search == "" {
		return nil
	}
	if cfg.MultiWordSearch {
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
