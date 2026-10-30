package benchdata

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"gorm.io/gorm"
)

// Product is a row of the products table.
type Product struct {
	ID         int64     `db:"id"`
	Name       string    `db:"name"`
	Brand      string    `db:"brand"`
	Category   string    `db:"category"`
	Price      float64   `db:"price"`
	Rating     *float64  `db:"rating"`
	Stock      int       `db:"stock"`
	ReleasedAt time.Time `db:"released_at"`
}

// TableName names the products table for GORM.
func (Product) TableName() string { return "products" }

var sortColumns = map[string]string{"price": "products.price", "name": "products.name", "id": "products.id"}

// page reads page and limit the way a hand-written endpoint would.
func page(query url.Values) (number, limit uint64) {
	number, _ = strconv.ParseUint(query.Get("page"), 10, 64)
	limit, _ = strconv.ParseUint(query.Get("limit"), 10, 64)
	if limit == 0 || limit > 100 {
		limit = 20
	}
	return max(number, 1), limit
}

func orderBy(query url.Values) []string {
	order := make([]string, 0, 3)
	for _, value := range query["sortBy"] {
		field, direction, _ := strings.Cut(value, ":")
		if column, ok := sortColumns[field]; ok && (direction == "asc" || direction == "desc") {
			order = append(order, column+" "+direction)
		}
	}
	return append(order, "products.id asc")
}

// HandwrittenSelect builds the count and the page of Query on top of qb by hand: what an endpoint
// without the library does for this one query. It reads four filters and two sort keys, checks no
// operators and allows no other fields.
func HandwrittenSelect(query url.Values, qb squirrel.SelectBuilder) (countSQL string, countArgs []any, pageSQL string, pageArgs []any, err error) {
	number, limit := page(query)
	if search := query.Get("search"); search != "" {
		pattern := "%" + search + "%"
		qb = qb.Where(squirrel.Or{
			squirrel.ILike{"CAST(products.name AS TEXT)": pattern},
			squirrel.ILike{"CAST(products.brand AS TEXT)": pattern},
		})
	}
	if v, ok := strings.CutPrefix(query.Get("filter.category"), "$in:"); ok {
		qb = qb.Where(squirrel.Eq{"products.category": strings.Split(v, ",")})
	}
	if v, ok := strings.CutPrefix(query.Get("filter.price"), "$btw:"); ok {
		from, to, _ := strings.Cut(v, ",")
		qb = qb.Where(squirrel.And{squirrel.GtOrEq{"products.price": from}, squirrel.LtOrEq{"products.price": to}})
	}
	if v, ok := strings.CutPrefix(query.Get("filter.tags"), "$contains:"); ok {
		qb = qb.Where("products.tags @> ?", "{"+v+"}")
	}
	if query.Get("filter.discontinuedAt") == "$null" {
		qb = qb.Where("products.discontinued_at IS NULL")
	}
	countSQL, countArgs, err = squirrel.Select("COUNT(*)").FromSelect(qb, "c").PlaceholderFormat(squirrel.Dollar).ToSql()
	if err != nil {
		return "", nil, "", nil, err
	}
	pageSQL, pageArgs, err = qb.OrderBy(orderBy(query)...).Limit(limit).Offset((number - 1) * limit).PlaceholderFormat(squirrel.Dollar).ToSql()
	return countSQL, countArgs, pageSQL, pageArgs, err
}

// HandwrittenGORM runs Query on db by hand, the GORM way: count, then the page into rows.
func HandwrittenGORM(ctx context.Context, query url.Values, db *gorm.DB, rows any) (int64, error) {
	number, limit := page(query)
	q := db.WithContext(ctx)
	if search := query.Get("search"); search != "" {
		pattern := "%" + search + "%"
		q = q.Where("CAST(products.name AS TEXT) ILIKE ? OR CAST(products.brand AS TEXT) ILIKE ?", pattern, pattern)
	}
	if v, ok := strings.CutPrefix(query.Get("filter.category"), "$in:"); ok {
		q = q.Where("products.category IN ?", strings.Split(v, ","))
	}
	if v, ok := strings.CutPrefix(query.Get("filter.price"), "$btw:"); ok {
		from, to, _ := strings.Cut(v, ",")
		q = q.Where("products.price >= ? AND products.price <= ?", from, to)
	}
	if v, ok := strings.CutPrefix(query.Get("filter.tags"), "$contains:"); ok {
		q = q.Where("products.tags @> ?", "{"+v+"}")
	}
	if query.Get("filter.discontinuedAt") == "$null" {
		q = q.Where("products.discontinued_at IS NULL")
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return 0, err
	}
	for _, order := range orderBy(query) {
		q = q.Order(order)
	}
	return total, q.Limit(int(limit)).Offset(int((number - 1) * limit)).Find(rows).Error
}
