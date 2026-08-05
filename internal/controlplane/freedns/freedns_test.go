package freedns

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDashedIPv4Host(t *testing.T) {
	h, err := DashedIPv4Host(net.ParseIP("163.5.180.181"), "sslip.io")
	if err != nil {
		t.Fatal(err)
	}
	if h != "163-5-180-181.sslip.io" {
		t.Fatalf("got %q", h)
	}
}

func TestAddrToolsHostStable(t *testing.T) {
	a := AddrToolsHost("1SuperSecret")
	b := AddrToolsHost("1SuperSecret")
	if a != b {
		t.Fatal("not stable")
	}
	if !strings.HasSuffix(a, ".dyn.addr.tools") {
		t.Fatalf("suffix: %s", a)
	}
	if len(strings.Split(a, ".")[0]) != 56 {
		t.Fatalf("sha224 hex len: %s", a)
	}
}

func TestEnsureSkipOnDNSFail(t *testing.T) {
	dir := t.TempDir()
	ip := net.ParseIP("203.0.113.10").To4()
	st, err := Ensure(context.Background(), Options{
		DataDir: dir,
		IPv4:    ip,
		Lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("198.51.100.1")}, nil
		},
		HTTPDo: func(req *http.Request) (*http.Response, error) {
			return httptest.NewRecorder().Result(), nil
		},
		RandSecret: func() (string, error) { return "test-secret", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Hosts()) != 0 {
		t.Fatalf("expected no hosts, got %v", st.Hosts())
	}
	if st.Providers[ProviderSSLip].Status != StatusSkipped {
		t.Fatalf("sslip: %+v", st.Providers[ProviderSSLip])
	}
}

func TestEnsureSuccessAndPersist(t *testing.T) {
	dir := t.TempDir()
	ip := net.ParseIP("203.0.113.10").To4()
	wantHost := map[string]bool{
		"203-0-113-10.sslip.io": true,
		"203-0-113-10.nip.io":   true,
	}
	secret := "persist-secret-1"
	addrHost := AddrToolsHost(secret)
	wantHost[addrHost] = true

	st, err := Ensure(context.Background(), Options{
		DataDir: dir,
		IPv4:    ip,
		Lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			if wantHost[host] {
				return []net.IP{ip}, nil
			}
			return nil, &net.DNSError{Err: "nxdomain", Name: host, IsNotFound: true}
		},
		HTTPDo: func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.WriteHeader(200)
			_, _ = rec.WriteString("OK")
			return rec.Result(), nil
		},
		RandSecret: func() (string, error) { return secret, nil },
		Now:        func() time.Time { return time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	hosts := st.Hosts()
	if len(hosts) != 3 {
		t.Fatalf("hosts=%v", hosts)
	}
	// Secret must persist across Ensure.
	st2, err := Ensure(context.Background(), Options{
		DataDir: dir,
		IPv4:    ip,
		Lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			return []net.IP{ip}, nil
		},
		HTTPDo: func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.WriteHeader(200)
			_, _ = rec.WriteString("OK")
			return rec.Result(), nil
		},
		RandSecret: func() (string, error) { return "SHOULD-NOT-BE-USED", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if st2.AddrSecret != secret {
		t.Fatalf("secret changed: %q", st2.AddrSecret)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "controlplane", "free_dns.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), secret) {
		t.Fatalf("state missing secret: %s", raw)
	}
}

func TestRefreshAddrToolsNotDue(t *testing.T) {
	dir := t.TempDir()
	ip := net.ParseIP("203.0.113.10").To4()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Hour)
	st := State{
		IPv4:       ip.String(),
		AddrSecret: "sec",
		AddrHost:   AddrToolsHost("sec"),
		Providers: map[string]ProviderStatus{
			ProviderAddrTools: {Host: AddrToolsHost("sec"), Status: StatusOK, LastUpdate: &last},
		},
	}
	if err := SaveState(dir, st); err != nil {
		t.Fatal(err)
	}
	_, did, err := RefreshAddrTools(context.Background(), Options{
		DataDir: dir,
		IPv4:    ip,
		Now:     func() time.Time { return now },
		HTTPDo: func(req *http.Request) (*http.Response, error) {
			t.Fatal("should not HTTP")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if did {
		t.Fatal("expected not due")
	}
}
