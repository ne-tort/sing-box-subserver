//go:build with_controlplane

package domain

import "testing"

func TestWgHubPeerAllowedIP(t *testing.T) {
	t.Parallel()
	h := WgHub{Subnet: "10.8.0.0/24"}
	ip, err := h.PeerAllowedIP(7)
	if err != nil || ip != "10.8.0.7/32" {
		t.Fatalf("%q %v", ip, err)
	}
	hub, err := h.HubAddress()
	if err != nil || hub != "10.8.0.1/24" {
		t.Fatalf("%q %v", hub, err)
	}
	if err := (WgHub{Profile: "nope"}).Validate(); err == nil {
		t.Fatal("want invalid profile")
	}
	if err := (WgHub{ForwardAllow: true, System: false, Profile: "wg", Subnet: "10.8.0.0/24"}).Validate(); err == nil {
		t.Fatal("want forward requires system")
	}
}
