// Package box will own sing-box registry wiring and Box lifecycle for the dataplane.
//
// Implementation lands after the skeleton; supervisor will call into this package.
// Build with tags from build/tags.server (server inbound + WireGuard/AWG profile).
package box
