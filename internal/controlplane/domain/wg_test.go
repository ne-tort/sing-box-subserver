//go:build with_controlplane

package domain

import "testing"

func TestWgHubPeerHostIP(t *testing.T) {
	t.Parallel()
	h := WgHub{Subnet: "10.8.0.0/24"}
	ip, err := h.PeerHostIP(7)
	if err != nil || ip != "10.8.0.7" {
		t.Fatalf("%q %v", ip, err)
	}
	iface, err := h.PeerInterfaceAddress(7)
	if err != nil || iface != "10.8.0.7" {
		t.Fatalf("iface %q %v", iface, err)
	}
	hub, err := h.HubAddress()
	if err != nil || hub != "10.8.0.1" {
		t.Fatalf("%q %v", hub, err)
	}
	if err := (WgHub{Profile: "nope"}).Validate(); err == nil {
		t.Fatal("want invalid profile")
	}
}

func TestWgHubExitForcesPeerRelay(t *testing.T) {
	t.Parallel()
	h := WgHub{ExitUserID: "u1", Profile: "wg", Subnet: "10.8.0.0/24"}
	h.Normalize()
	if !h.PeerRelay || h.ExitUserID != "u1" {
		t.Fatalf("%+v", h)
	}
}

func TestHostIPOnly(t *testing.T) {
	t.Parallel()
	if HostIPOnly("10.8.0.5/24") != "10.8.0.5" {
		t.Fatal(HostIPOnly("10.8.0.5/24"))
	}
	if HostIPOnly("10.8.0.5") != "10.8.0.5" {
		t.Fatal(HostIPOnly("10.8.0.5"))
	}
}

func TestWgHubLegacyForwardMigratesToPeerRelay(t *testing.T) {
	t.Parallel()
	h := WgHub{LegacyForwardAllow: true, Profile: "wg", Subnet: "10.8.0.0/24"}
	h.Normalize()
	if !h.PeerRelay || h.LegacyForwardAllow {
		t.Fatalf("%+v", h)
	}
}
