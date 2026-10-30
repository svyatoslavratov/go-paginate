// Command server is the go-paginate demo on a real stack: chi, GORM and Postgres. It serves the
// demo page at / and the products at /api/products.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/examples/catalog"
	"github.com/svyatoslavratov/go-paginate/examples/catalog/web"
	"github.com/svyatoslavratov/go-paginate/gormx"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/catalog?sslmode=disable")
	addr := getenv("ADDR", ":8080")

	db, err := connect(dsn)
	if err != nil {
		return err
	}
	if err := db.Exec(catalog.SeedSQL).Error; err != nil {
		return err
	}
	if err := db.Callback().Query().After("gorm:query").Register("demo:capture", captureSQL); err != nil {
		return err
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Logger, middleware.Recoverer)
	r.Get("/api/schema", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, catalog.Fields())
	})
	r.Get(catalog.Path, listProducts(db))
	r.Get(catalog.Path+"/sql", productsSQL(db))
	r.Handle("/*", http.FileServerFS(web.FS))

	server := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("the demo is on http://localhost%s", addr)
	return server.ListenAndServe()
}

// listProducts is the list endpoint: parse, paginate, answer.
func listProducts(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := paginate.ParseRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		cfg := catalog.ConfigFor(r.URL.Query().Get("format"))
		page, err := gormx.Paginate[catalog.Product](r.Context(), *req, cfg, db.Model(&catalog.Product{}))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

type capturedKey struct{}

// productsSQL runs the same request in GORM's dry-run mode and returns the SQL it would run.
func productsSQL(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := paginate.ParseRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		var statements []string
		ctx := context.WithValue(r.Context(), capturedKey{}, &statements)
		dry := db.Session(&gorm.Session{DryRun: true}).Model(&catalog.Product{})
		cfg := catalog.ConfigFor(r.URL.Query().Get("format"))
		if _, err := gormx.Paginate[catalog.Product](ctx, *req, cfg, dry); err != nil {
			writeError(w, err)
			return
		}
		answer := map[string]string{"count": statements[0]}
		if len(statements) > 1 {
			answer["select"] = statements[1]
		}
		writeJSON(w, http.StatusOK, answer)
	}
}

// captureSQL records the statements of dry-run queries, with the values inlined for reading.
func captureSQL(tx *gorm.DB) {
	if !tx.DryRun {
		return
	}
	if statements, ok := tx.Statement.Context.Value(capturedKey{}).(*[]string); ok {
		*statements = append(*statements, tx.Explain(tx.Statement.SQL.String(), tx.Statement.Vars...))
	}
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, paginate.ErrInvalidRequest) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	log.Printf("list products: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(body)
}

// connect opens Postgres, waiting up to a minute for it to start.
func connect(dsn string) (*gorm.DB, error) {
	var err error
	for range 30 {
		var db *gorm.DB
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
		if err == nil {
			return db, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, err
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
