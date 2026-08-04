//go:build with_controlplane

package controlplane

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
)

// Client-shaped payloads matching Flutter buildServerDnsFragment /
// buildServerOutboundsPayload / buildServerRoutePayload.
func TestClientNetFragmentsAgainstAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "n1", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
		Controlplane: agentcfg.ControlplaneConfig{PublicHost: "203.0.113.10", ExpiryTickSec: 60},
	}
	cs, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(cs, &testutil.FakeEngine{}, o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	owner, err := configowner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{Cfg: cfg, DataDir: dir, Supervisor: sup, Owner: owner, Logger: o.Logger})
	svc.Bootstrap(nil)
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	put := func(path string, body []byte) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body)))
		return rr
	}
	del := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, path, nil))
		return rr
	}

	// --- DNS (mirrors buildServerDnsFragment) ---
	dnsBody := []byte(`{"dns":{
		"independent_cache":false,
		"disable_expire":true,
		"final":"dns-remote",
		"servers":[
			{"tag":"dns-local","type":"local"},
			{"tag":"dns-bootstrap-0","type":"udp","server":"1.1.1.1","domain_resolver":"dns-local"},
			{"tag":"dns-bootstrap","type":"group","servers":["dns-bootstrap-0"],"mode":"stable","error_ttl":"2m"},
			{"tag":"dns-remote-0","type":"local","domain_resolver":"dns-bootstrap","detour":"direct"},
			{"tag":"dns-remote","type":"group","servers":["dns-remote-0"],"mode":"stable","error_ttl":"2m"},
			{"tag":"dns-fake","type":"fakeip","inet4_range":"198.18.0.0/15","inet6_range":"fc00::/18"}
		],
		"rules":[
			{"query_type":["A","AAAA"],"server":"dns-fake","strategy":"prefer_ipv4","disable_cache":true},
			{"server":"dns-remote","strategy":"prefer_ipv4"}
		]
	}}`)
	if rr := put("/v1/controlplane/config/dns", dnsBody); rr.Code != 200 {
		t.Fatalf("PUT client dns: %d %s", rr.Code, rr.Body.String())
	}

	// --- Outbounds (mirrors buildServerOutboundsPayload with balancer) ---
	outBody := []byte(`{"outbounds":[
		{"type":"selector","tag":"select","outbounds":["lowest","balance","Exit · a","Exit · b"],"default":"balance","interrupt_exist_connections":true},
		{"type":"balancer","tag":"lowest","outbounds":["Exit · a","Exit · b"],"strategy":"lowest-delay","interrupt_exist_connections":true},
		{"type":"balancer","tag":"balance","outbounds":["Exit · a","Exit · b"],"strategy":"round-robin","interrupt_exist_connections":true},
		{"type":"socks","tag":"Exit · a","server":"1.1.1.1","server_port":1080},
		{"type":"socks","tag":"Exit · b","server":"1.0.0.1","server_port":1080},
		{"type":"direct","tag":"direct"},
		{"type":"block","tag":"block"}
	]}`)
	if rr := put("/v1/controlplane/config/outbounds", outBody); rr.Code != 200 {
		t.Fatalf("PUT client outbounds: %d %s", rr.Code, rr.Body.String())
	}

	// --- Route with rulesets + final=balance ---
	srs := []byte{0x01, 0x02, 0x03, 0x04}
	routeObj := map[string]any{
		"route": map[string]any{
			"final": "balance",
			"rules": []any{
				map[string]any{"protocol": []any{"dns"}, "action": "hijack-dns"},
				map[string]any{"ip_is_private": true, "outbound": "direct"},
				map[string]any{"rule_set": []any{"local-abc"}, "outbound": "balance"},
				map[string]any{"protocol": []any{"quic"}, "action": "reject"},
			},
			"rule_set": []any{
				map[string]any{"type": "local", "tag": "local-abc", "format": "binary", "path": "rp_test_abc.srs"},
			},
			"default_domain_resolver": map[string]any{"server": "dns-bootstrap"},
		},
		"rulesets": []any{
			map[string]any{
				"filename":       "rp_test_abc.srs",
				"content_base64": base64.StdEncoding.EncodeToString(srs),
			},
		},
	}
	routeRaw, _ := json.Marshal(routeObj)
	rr := put("/v1/controlplane/config/route", routeRaw)
	if rr.Code != 200 {
		t.Fatalf("PUT client route+rulesets: %d %s", rr.Code, rr.Body.String())
	}
	var routeResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &routeResp); err != nil {
		t.Fatal(err)
	}
	if n, _ := routeResp.Data["rulesets_written"].(float64); n != 1 {
		t.Fatalf("rulesets_written=%v want 1; data=%v", routeResp.Data["rulesets_written"], routeResp.Data)
	}

	// Soft-patch style: only change final back to direct (Rest→proxy off / Direct egress).
	patch := []byte(`{"route":{"final":"direct","rules":[{"protocol":["dns"],"action":"hijack-dns"},{"ip_is_private":true,"outbound":"direct"},{"rule_set":["local-abc"],"outbound":"balance"},{"protocol":["quic"],"action":"reject"}],"rule_set":[{"type":"local","tag":"local-abc","format":"binary","path":"rp_test_abc.srs"}]}}`)
	if rr := put("/v1/controlplane/config/route", patch); rr.Code != 200 {
		t.Fatalf("PUT route final=direct: %d %s", rr.Code, rr.Body.String())
	}

	// Direct egress: DELETE outbounds
	if rr := del("/v1/controlplane/config/outbounds"); rr.Code != 200 {
		t.Fatalf("DELETE outbounds: %d %s", rr.Code, rr.Body.String())
	}
	if rr := del("/v1/controlplane/config/dns"); rr.Code != 200 {
		t.Fatalf("DELETE dns: %d %s", rr.Code, rr.Body.String())
	}
	if rr := del("/v1/controlplane/config/route"); rr.Code != 200 {
		t.Fatalf("DELETE route: %d %s", rr.Code, rr.Body.String())
	}
}
