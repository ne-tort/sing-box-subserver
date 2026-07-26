package agentcfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDefaultsAndRequired(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`node_id: x`))
	if err == nil {
		t.Fatal("expected error for missing token")
	}

	cfg, err := Parse([]byte(`
node_id: edge-1
token: secret
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Fatalf("listen default: got %q", cfg.Listen)
	}
	if cfg.DataDir != "/var/lib/subserver" {
		t.Fatalf("data_dir default: got %q", cfg.DataDir)
	}
	if !cfg.HealthPublicEnabled() {
		t.Fatal("health_public should default true")
	}
}

func TestPullURLImpliesEnabled(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`
node_id: edge-1
token: secret
pull:
  url: https://panel.example/desired
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Pull.Enabled {
		t.Fatal("expected pull enabled when url set")
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(`
node_id: edge-1
token: secret
listen: 127.0.0.1:9090
data_dir: /tmp/sub
log:
  level: debug
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9090" || cfg.Log.Level != "debug" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestPublicBindRequiresTLSOrFlag(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`
node_id: edge-1
token: secret
listen: 0.0.0.0:8080
`))
	if err == nil {
		t.Fatal("expected bind policy error")
	}
	cfg, err := Parse([]byte(`
node_id: edge-1
token: secret
listen: 0.0.0.0:8080
insecure_public_bind: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecurePublicBind {
		t.Fatal("expected insecure flag")
	}
}

func TestInvalidLogLevel(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`
node_id: edge-1
token: secret
log:
  level: verbose
`))
	if err == nil {
		t.Fatal("expected invalid log level error")
	}
}
