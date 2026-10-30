package paginate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Order is the direction of a sort key.
type Order string

const (
	// Asc sorts ascending.
	Asc Order = "ASC"
	// Desc sorts descending.
	Desc Order = "DESC"
)

// parseOrder reads asc or desc in any case.
func parseOrder(value string) (Order, bool) {
	switch order := Order(strings.ToUpper(value)); order {
	case Asc, Desc:
		return order, true
	}
	return "", false
}

func (o Order) reverse() Order {
	if o == Desc {
		return Asc
	}
	return Desc
}

// Sort is a sort key in API terms: sortBy=createdAt:DESC is {Field: "createdAt", Order: Desc}. Its
// JSON is a pair, ["createdAt", "DESC"], as nestjs-paginate writes meta.sortBy.
type Sort struct {
	Field string
	Order Order
}

// MarshalJSON writes the pair [field, order].
func (s Sort) MarshalJSON() ([]byte, error) {
	return marshalJSON([2]string{s.Field, string(s.Order)})
}

// UnmarshalJSON reads the pair [field, order]; the order may be in any case.
func (s *Sort) UnmarshalJSON(data []byte) error {
	var pair [2]string
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	order, ok := parseOrder(pair[1])
	if !ok {
		return fmt.Errorf("paginate: sort order of %s must be ASC or DESC, got %q", pair[0], pair[1])
	}
	*s = Sort{Field: pair[0], Order: order}
	return nil
}

// SortEntry is a key of ORDER BY in SQL terms. The first entry is the main key; the next ones
// order rows with equal values.
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

// nullsLast tells where the NULL values of a key go: where NullSort says, otherwise where Postgres
// puts them, last for ASC and first for DESC.
func nullsLast(order Order, nullSort NullSort) bool {
	switch nullSort {
	case NullsFirst:
		return false
	case NullsLast:
		return true
	}
	return order == Asc
}

// appendTieBreak appends the tie-breaker (a unique column, ascending) as the last key, so rows with
// equal values of the other keys keep their order between pages. A column that is already a key
// is not added twice.
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

// orderByExprs renders the ORDER BY expressions in entry order. Read forward, a key gets NULLS
// FIRST or LAST when NullSort says so. Read backward (paging back from a cursor), every key is
// flipped together with its NULL placement, which is then always written out.
func orderByExprs(entries []SortEntry, nullSort NullSort, backward bool) []string {
	orderBy := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !backward {
			orderBy = append(orderBy, entry.Column+" "+string(entry.Order)+nullsSuffix(nullSort))
			continue
		}
		placement := " NULLS LAST"
		if nullsLast(entry.Order, nullSort) {
			placement = " NULLS FIRST"
		}
		orderBy = append(orderBy, entry.Column+" "+string(entry.Order.reverse())+placement)
	}
	return orderBy
}

func nullsSuffix(nullSort NullSort) string {
	switch nullSort {
	case NullsFirst:
		return " NULLS FIRST"
	case NullsLast:
		return " NULLS LAST"
	}
	return ""
}
