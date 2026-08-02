//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

//go:embed sql/schema.sql
var schemaSQL string

//go:embed ref/vless
var vlessRefFS embed.FS

//go:embed data/vless.sqlite
var vlessSQLite []byte

var (
	bootOnce sync.Once
	bootErr  error
	db       *sql.DB
)

// DB returns the read-only catalog. Safe for concurrent queries.
func DB() (*sql.DB, error) {
	bootOnce.Do(func() {
		bootErr = openCatalog()
	})
	return db, bootErr
}

func openCatalog() error {
	if len(vlessSQLite) == 0 {
		return fmt.Errorf("catalogsqlite: embedded data/vless.sqlite is empty — run: go run -tags with_controlplane ./cmd/gen-catalogsqlite-vless")
	}
	dir, err := os.MkdirTemp("", "cp-catalogsqlite-*")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "vless.sqlite")
	if err := os.WriteFile(path, vlessSQLite, 0o444); err != nil {
		return err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=query_only(ON)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("catalogsqlite open: %w", err)
	}
	conn.SetMaxOpenConns(1)
	var n int
	if err := conn.QueryRow(`SELECT COUNT(1) FROM ready_presets`).Scan(&n); err != nil {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite probe: %w", err)
	}
	if n < 5 {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite: ready_presets too small (%d)", n)
	}
	db = conn
	return nil
}
