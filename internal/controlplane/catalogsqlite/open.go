//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

//go:embed sql/schema.sql
var schemaSQL string

//go:embed ref/vless
var vlessRefFS embed.FS

var (
	bootOnce sync.Once
	bootErr  error
	db       *sql.DB
)

// DB returns the read-only in-memory catalog. Safe for concurrent queries.
func DB() (*sql.DB, error) {
	bootOnce.Do(func() {
		bootErr = openAndSeed()
	})
	return db, bootErr
}

func openAndSeed() error {
	conn, err := sql.Open("sqlite", "file:catalogv2?mode=memory&cache=shared")
	if err != nil {
		return fmt.Errorf("catalogsqlite open: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(schemaSQL); err != nil {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite schema: %w", err)
	}
	if err := importVlessRef(conn); err != nil {
		_ = conn.Close()
		return fmt.Errorf("catalogsqlite seed vless: %w", err)
	}
	if _, err := conn.Exec(`INSERT INTO meta(key,value) VALUES('schema_version','1'),('pilot_protocol','vless')`); err != nil {
		_ = conn.Close()
		return err
	}
	// Enforce read-only after seed.
	if _, err := conn.Exec(`PRAGMA query_only = ON`); err != nil {
		_ = conn.Close()
		return err
	}
	db = conn
	return nil
}
