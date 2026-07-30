//go:build with_controlplane

package demuxgroups

import "testing"

func TestDemuxCompatTransportRealityAndShadowQUICAreLab(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{"vless_reality", "full"},
		{"hy2", "full"},
		{"vless_ws_reality", "full"},
		{"vless_grpc_reality", "demux_lab"},
		{"trojan_ws_reality", "demux_lab"},
		{"vmess_ws_reality", "demux_lab"},
		{"trojan_httpupgrade_reality", "demux_lab"},
		{"shadowquic_jls", "demux_lab"},
		{"shadowquic_0rtt", "demux_lab"},
		{"trusttunnel_h3", "demux_lab"},
		{"hy2_salamander", "demux_unsupported"},
		{"hy2_gecko", "demux_unsupported"},
		{"hy1_obfs", "demux_unsupported"},
	}
	for _, tc := range cases {
		got := demuxCompatForPreset(tc.tag, RoleTCPReality, "sni_pool")
		if got != tc.want {
			t.Fatalf("%s: demuxCompat=%q want %q", tc.tag, got, tc.want)
		}
	}
}
