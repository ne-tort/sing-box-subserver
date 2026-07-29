package app_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/app"
	"github.com/ne-tort/sing-box-subserver/internal/version"
)

// When pull is disabled, Run must keep serving (regression: puller used to exit and tear down API).
//
// With with_controlplane the management listener is always HTTPS (CP TLS profile);
// without the tag it is plain HTTP.
func TestRun_PullDisabledKeepsServing(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfgPath := filepath.Join(dir, "agent.yaml")
	yaml := "node_id: life-1\n" +
		"token: test-token\n" +
		"listen: \"" + addr + "\"\n" +
		"data_dir: \"" + filepath.ToSlash(filepath.Join(dir, "data")) + "\"\n" +
		"probe_ms: 50\n" +
		"log:\n  level: error\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	runCtx, stop := context.WithCancel(context.Background())
	defer stop()

	done := make(chan error, 1)
	go func() {
		done <- app.Run(app.Options{ConfigPath: cfgPath, Context: runCtx})
	}()

	client := &http.Client{
		Timeout: 500 * time.Millisecond,
		Transport: &http.Transport{
			// CP mgmt uses self-signed safety PEMs in tests.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	schemes := healthSchemes()

	// Readiness is normally ~250ms; 5s is only slack for slow CI, not a retry of a broken probe.
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := probeHealth(client, addr, schemes)
		if err != nil {
			lastErr = err
		}
		if ok {
			// Still alive after pull would have returned.
			time.Sleep(200 * time.Millisecond)
			ok2, err2 := probeHealth(client, addr, schemes)
			if err2 != nil || !ok2 {
				t.Fatalf("API died after pull idle: ok=%v err=%v", ok2, err2)
			}
			stop()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("Run did not stop after cancel")
			}
			return
		}
		select {
		case err := <-done:
			t.Fatalf("Run exited early: %v (last probe: %v)", err, lastErr)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("health never became ready: %v", lastErr)
}

func healthSchemes() []string {
	for _, tag := range version.BuildTags {
		if tag == "with_controlplane" {
			return []string{"https"}
		}
	}
	return []string{"http"}
}

func probeHealth(client *http.Client, addr string, schemes []string) (bool, error) {
	var last error
	for _, scheme := range schemes {
		reqCtx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, scheme+"://"+addr+"/v1/health", nil)
		if err != nil {
			cancel()
			return false, err
		}
		resp, err := client.Do(req)
		cancel()
		if err != nil {
			last = err
			continue
		}
		code := resp.StatusCode
		_ = resp.Body.Close()
		if code == http.StatusOK {
			return true, nil
		}
		last = &statusError{code: code}
	}
	return false, last
}

type statusError struct{ code int }

func (e *statusError) Error() string {
	return "unexpected health status: " + http.StatusText(e.code)
}
