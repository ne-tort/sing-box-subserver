//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// BuildSeedFile creates a SQLite catalog file from embedded ref JSON (generator input).
// Runtime Open uses the committed data/vless.sqlite blob — not this path.
func BuildSeedFile(outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(outPath)
	conn, err := sql.Open("sqlite", "file:"+filepath.ToSlash(outPath)+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Exec(schemaSQL); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	if err := importVlessRef(conn); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if _, err := conn.Exec(`INSERT INTO meta(key,value) VALUES('schema_version','1'),('pilot_protocol','vless')`); err != nil {
		return err
	}
	return nil
}
