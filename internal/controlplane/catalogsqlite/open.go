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

//go:embed ref
var catalogRefFS embed.FS

//go:embed data/catalog.sqlite
var catalogSQLite []byte

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
	if len(catalogSQLite) == 0 {
		return fmt.Errorf("catalogsqlite: embedded data/catalog.sqlite is empty — run: go run -tags with_controlplane ./cmd/gen-catalogsqlite")
	}
	dir, err := os.MkdirTemp("", "cp-catalogsqlite-*")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "catalog.sqlite")
	if err := os.WriteFile(path, catalogSQLite, 0o444); err != nil {
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
	rows, err := conn.Query(`SELECT tag FROM protocols ORDER BY tag`)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite probe protocols: %w", err)
	}
	var protocols []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			_ = rows.Close()
			_ = conn.Close()
			return err
		}
		protocols = append(protocols, tag)
	}
	_ = rows.Close()
	if len(protocols) < 1 {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite: no protocols — regenerate data/catalog.sqlite")
	}
	for _, proto := range protocols {
		var bases, readies int
		if err := conn.QueryRow(`SELECT COUNT(1) FROM preset_bases WHERE protocol=?`, proto).Scan(&bases); err != nil {
			_ = conn.Close()
			return fmt.Errorf("catalogsqlite probe preset_bases(%s): %w", proto, err)
		}
		if err := conn.QueryRow(`SELECT COUNT(1) FROM ready_presets WHERE protocol=?`, proto).Scan(&readies); err != nil {
			_ = conn.Close()
			return fmt.Errorf("catalogsqlite probe ready_presets(%s): %w", proto, err)
		}
		if bases < 1 || readies < 1 {
			_ = conn.Close()
			return fmt.Errorf("catalogsqlite: protocol %q incomplete (bases=%d ready=%d) — regenerate data/catalog.sqlite", proto, bases, readies)
		}
	}
	var demuxN int
	if err := conn.QueryRow(`SELECT COUNT(1) FROM demux_groups`).Scan(&demuxN); err != nil {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite probe demux_groups: %w", err)
	}
	if demuxN < 1 {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite: demux_groups empty — dump ref/demux + regenerate")
	}
	db = conn
	return nil
}
