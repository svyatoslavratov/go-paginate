package gormx

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/svyatoslavratov/go-paginate"
)

// ConfigOf builds a config from the paginate tags of model T, as paginate.ConfigOf does, with the
// columns taken from the GORM schema: column tags, the naming strategy and embedded structs
// (gorm:"embedded" included) all count. table qualifies the columns, empty for the model's own
// table; the tie-breaker is the field tagged unique or, without one, the single-column primary key.
// Fields GORM knows as NOT NULL or as primary keys get NotNull.
//
//	type Product struct {
//		ID    int64   `json:"id"`
//		Name  string  `json:"name" paginate:"sort;search;filter:$ilike"`
//		Price float64 `json:"price" paginate:"sort;filter:$gte,$lte"`
//	}
//
//	cfg, err := gormx.ConfigOf[Product](db, "")
func ConfigOf[T any](db *gorm.DB, table string) (paginate.Config, error) {
	if db == nil {
		return paginate.Config{}, errors.New("gormx: gorm db is required")
	}
	parsed, err := schema.Parse(reflect.New(reflect.TypeFor[T]()).Interface(), &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return paginate.Config{}, fmt.Errorf("gormx: parse model %s: %w", reflect.TypeFor[T](), err)
	}
	if table == "" {
		table = parsed.Table
	}
	cfg, err := paginate.StructConfig(reflect.TypeFor[T](), func(path []reflect.StructField) (string, error) {
		return columnOf(db, table, parsed, path)
	})
	if err != nil {
		return paginate.Config{}, err
	}
	if cfg.TieBreak == "" {
		cfg.TieBreak = primaryKeyColumn(table, parsed)
	}
	markNotNull(cfg.Fields, table, parsed)
	return cfg, nil
}

// markNotNull sets NotNull on the fields whose columns GORM knows as NOT NULL or primary keys.
func markNotNull(fields []paginate.Field, table string, parsed *schema.Schema) {
	notNull := make(map[string]bool, len(parsed.Fields))
	for _, field := range parsed.Fields {
		if field.DBName != "" && (field.NotNull || field.PrimaryKey) {
			notNull[table+"."+field.DBName] = true
		}
	}
	for i := range fields {
		if notNull[fields[i].Column] {
			fields[i].NotNull = true
		}
	}
}
