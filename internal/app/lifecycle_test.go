package app_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/app"
)

// When pull is disabled, Run must keep serving (regression: puller used to exit and tear down API).
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

	deadline := time.Now().Add(8 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		reqCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/v1/health", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				// Still alive after pull would have returned.
				time.Sleep(200 * time.Millisecond)
				resp2, err2 := http.Get("http://" + addr + "/v1/health")
				if err2 != nil {
					t.Fatalf("API died after pull idle: %v", err2)
				}
				_ = resp2.Body.Close()
				stop()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("Run did not stop after cancel")
				}
				return
			}
			lastErr = nil
		} else {
			lastErr = err
		}
		select {
		case err := <-done:
			t.Fatalf("Run exited early: %v (last dial: %v)", err, lastErr)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("health never became ready: %v", lastErr)
}
