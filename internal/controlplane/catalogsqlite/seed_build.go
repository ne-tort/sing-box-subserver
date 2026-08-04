//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BuildSeedFile creates a SQLite catalog file from embedded ref JSON (generator input).
// Runtime Open uses the committed data/catalog.sqlite blob — not this path.
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
	entries, err := fs.ReadDir(catalogRefFS, "ref")
	if err != nil {
		return fmt.Errorf("list ref: %w", err)
	}
	var imported []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		proto := e.Name()
		if proto == "demux" {
			continue // imported after protocols
		}
		if err := importProtocolRef(conn, proto); err != nil {
			return fmt.Errorf("import %s: %w", proto, err)
		}
		imported = append(imported, proto)
	}
	if len(imported) == 0 {
		return fmt.Errorf("no protocol dirs under ref/")
	}
	demuxN, err := importDemuxRefs(conn)
	if err != nil {
		return fmt.Errorf("import demux: %w", err)
	}
	metaProto := strings.Join(imported, ",")
	if _, err := conn.Exec(`INSERT INTO meta(key,value) VALUES('schema_version','1'),('owned_protocols',?),('demux_groups',?)`,
		metaProto, fmt.Sprintf("%d", demuxN)); err != nil {
		return err
	}
	return nil
}
