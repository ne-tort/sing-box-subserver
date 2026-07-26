// Package version exposes agent and embedded sing-box-lx version metadata for
// CLI (-version) and future GET /v1/status / /v1/version handlers.
package version

import "github.com/sagernet/sing-box/constant"

// Injected via -ldflags -X at release build time.
var (
	AgentVersion = "0.0.0-dev"
	AgentCommit  = "unknown"
	SingBoxCommit = "unknown"
)

// SingBoxVersion returns the lx constant.Version string (often set via ldflags
// on github.com/sagernet/sing-box/constant.Version).
func SingBoxVersion() string {
	return constant.Version
}

// BuildTags is filled at init from a generated or hand-maintained list when needed.
// For the skeleton, callers may pass tags from the build environment separately.
var BuildTags []string
