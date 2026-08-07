//go:build with_controlplane

package smoke

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// ProbeViaSOCKS GETs urls through a SOCKS5 proxy until one succeeds.
func ProbeViaSOCKS(ctx context.Context, socksAddr string, urls []string, perURL time.Duration) (ok bool, latencyMs int, used string, err error) {
	if perURL <= 0 {
		perURL = defaultTimeout
	}
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{Timeout: perURL})
	if err != nil {
		return false, 0, "", fmt.Errorf("socks dialer: %w", err)
	}
	contextDialer, okDial := dialer.(proxy.ContextDialer)
	if !okDial {
		return false, 0, "", fmt.Errorf("socks dialer missing ContextDialer")
	}
	transport := &http.Transport{
		DialContext:           contextDialer.DialContext,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: perURL,
		TLSHandshakeTimeout:   perURL,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   perURL,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	var lastErr error
	for _, u := range urls {
		select {
		case <-ctx.Done():
			return false, 0, "", ctx.Err()
		default:
		}
		start := time.Now()
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		ms := int(time.Since(start).Milliseconds())
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			continue
		}
		return true, ms, u, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all probe urls failed")
	}
	return false, 0, "", lastErr
}

// ProbeDirect GETs urls without a proxy (control check for egress / URL bank).
func ProbeDirect(ctx context.Context, urls []string, perURL time.Duration) (ok bool, used string, err error) {
	if perURL <= 0 {
		perURL = defaultTimeout
	}
	client := &http.Client{
		Timeout: perURL,
		Transport: &http.Transport{
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: perURL,
			TLSHandshakeTimeout:   perURL,
			DialContext: (&net.Dialer{Timeout: perURL}).DialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	var lastErr error
	for _, u := range urls {
		select {
		case <-ctx.Done():
			return false, "", ctx.Err()
		default:
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			continue
		}
		return true, u, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all probe urls failed")
	}
	return false, "", lastErr
}
