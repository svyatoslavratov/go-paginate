package paginate

import (
	"maps"
	"slices"
)

// Order is the direction of a sort key.
type Order string

const (
	// Asc sorts ascending.
	Asc Order = "asc"
	// Desc sorts descending.
	Desc Order = "desc"
)

// SortBy maps a field (API names in a Request, columns in a Config) to its direction.
type SortBy map[string]Order

// SortEntry is one key of ORDER BY. The order of entries is the order of the keys: the first is the
// main one, the next ones order rows with equal values.
type SortEntry struct {
	Column string
	Order  Order
}

// NullSort places NULL values first or last in every sort key.
type NullSort int

const (
	// NullsFirst puts NULL values first.
	NullsFirst NullSort = iota + 1
	// NullsLast puts NULL values last.
	NullsLast
)

// sortEntries orders the sort keys of a request:
//  1. request keys in Request.SortKeys order (the parsers keep the order of the parameters);
//  2. the other SortBy keys (a request built in code: order unknown) in SortableColumns order;
//  3. without request keys, DefaultSortBy in DefaultSortKeys order, then alphabetically;
//  4. otherwise the first sortable column ascending.
//
// sortBy holds the request keys already mapped to columns. Keys that are not sortable are dropped.
func sortEntries(req *Request, sortBy map[string]Order, cfg *Config) ([]SortEntry, error) {
	if len(cfg.SortableColumns) == 0 {
		return nil, invalidConfig("sortableColumns is required")
	}

	entries := make([]SortEntry, 0, len(sortBy))
	seen := make(map[string]bool, len(sortBy))
	add := func(column string) {
		order, ok := sortBy[column]
		if !ok || seen[column] || !slices.Contains(cfg.SortableColumns, column) {
			return
		}
		seen[column] = true
		entries = append(entries, SortEntry{Column: column, Order: order})
	}
	if len(sortBy) != 0 {
		for _, key := range req.SortKeys {
			if column, ok := cfg.FieldColumns[key]; ok {
				add(column)
			}
		}
		for _, column := range cfg.SortableColumns {
			add(column)
		}
	}

	if len(entries) == 0 {
		entries = defaultSortEntries(cfg)
	}
	return entries, nil
}

// defaultSortEntries is the sort of a request without keys: DefaultSortBy in DefaultSortKeys order,
// the rest of DefaultSortBy alphabetically, otherwise the first sortable column ascending.
func defaultSortEntries(cfg *Config) []SortEntry {
	entries := make([]SortEntry, 0, len(cfg.DefaultSortBy))
	seen := make(map[string]bool, len(cfg.DefaultSortBy))
	for _, column := range cfg.DefaultSortKeys {
		if order, ok := cfg.DefaultSortBy[column]; ok && !seen[column] {
			seen[column] = true
			entries = append(entries, SortEntry{Column: column, Order: order})
		}
	}
	for _, column := range slices.Sorted(maps.Keys(cfg.DefaultSortBy)) {
		if !seen[column] {
			entries = append(entries, SortEntry{Column: column, Order: cfg.DefaultSortBy[column]})
		}
	}
	if len(entries) == 0 {
		entries = append(entries, SortEntry{Column: cfg.SortableColumns[0], Order: Asc})
	}
	return entries
}

// appendTieBreak appends the tie-breaker (a unique column, ascending) as the last key, so rows with
// equal values of the other keys keep their order between LIMIT/OFFSET pages. A column that is
// already a key is not added twice.
func appendTieBreak(entries []SortEntry, column string) []SortEntry {
	if column == "" {
		return entries
	}
	for _, entry := range entries {
		if entry.Column == column {
			return entries
		}
	}
	return append(entries, SortEntry{Column: column, Order: Asc})
}

// orderByExprs renders the ORDER BY expressions in entry order, with NULLS FIRST/LAST.
func orderByExprs(entries []SortEntry, nullSort NullSort) []string {
	suffix := ""
	switch nullSort {
	case NullsFirst:
		suffix = " NULLS FIRST"
	case NullsLast:
		suffix = " NULLS LAST"
	}
	orderBy := make([]string, 0, len(entries))
	for _, entry := range entries {
		orderBy = append(orderBy, entry.Column+" "+string(entry.Order)+suffix)
	}
	return orderBy
}
