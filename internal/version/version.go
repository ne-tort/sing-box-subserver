package version

import (
	"strings"

	"github.com/sagernet/sing-box/constant"
)

// Injected via -ldflags -X at release build time.
var (
	AgentVersion  = "0.0.0-dev"
	AgentCommit   = "unknown"
	SingBoxCommit = "unknown"
)

// DefaultBuildTags must stay in sync with build/tags.server (enforced by tags_test.go).
const DefaultBuildTags = "with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_purego,badlinkname,tfogo_checklinkname0,with_xhttp,with_mieru,with_balancer,with_ssh,with_acme,with_grpc,with_awg,with_carrier,with_demux,with_derp"

// BuildTags lists tags this binary was built with (defaults to DefaultBuildTags).
var BuildTags = strings.Split(DefaultBuildTags, ",")

// SingBoxVersion returns the lx constant.Version string (often set via ldflags
// on github.com/sagernet/sing-box/constant.Version).
func SingBoxVersion() string {
	return constant.Version
}
