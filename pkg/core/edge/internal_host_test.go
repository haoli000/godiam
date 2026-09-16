// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package edge

import "testing"

// internalHostConfig is an edge agent terminating two realms of its own --
// its local realm and a deeper core realm reached through the internal zone --
// in front of one roaming partner.
const internalHostConfig = `
zones:
  - name: core
    role: internal
    realms: ["core.example.com", "*.epc.example.com"]
    listen:
      port: 3868
  - name: roaming
    role: external
    listen:
      port: 3869
partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com", "*.partnera.com"]
    peers: ["dea1.partnera.com"]
peers:
  - identity: "hss.core.example.com"
    addresses: ["10.0.0.5"]
    zone: core
  - identity: "dea1.partnera.com"
    addresses: ["10.0.0.7"]
    zone: roaming
`

// IsInternalHost is the classification the rest of the edge agent acts on: an
// identity judged internal gets masked on the way out and may be addressed
// from inside, and one judged external must not be. Both mistakes are
// serious and neither is loud -- calling an internal host external leaks the
// operator's topology to a partner, and calling a partner's host internal
// hands it the trust of the operator's own network.
func TestIsInternalHost(t *testing.T) {
	r := NewRegistry(testConfig(t, internalHostConfig))

	tests := []struct {
		name string
		host string
		want bool
		why  string
	}{
		{
			name: "the agent itself",
			host: "dea.example.com",
			want: true,
			why:  "the node's own identity is the most internal identity there is",
		},
		{
			name: "peer in an internal zone",
			host: "hss.core.example.com",
			want: true,
			why:  "a peer placed in an internal zone is internal by configuration",
		},
		{
			name: "peer in an external zone",
			host: "dea1.partnera.com",
			want: false,
			why:  "a peer placed in an external zone belongs to a partner, whatever its realm looks like",
		},
		{
			name: "host in the local realm",
			host: "mme.example.com",
			want: true,
			why:  "the agent's own realm is served by definition",
		},
		{
			name: "host in a realm served behind an internal zone",
			host: "hss2.core.example.com",
			want: true,
			why:  "internal nodes are not all configured as peers; the served realm is what makes them internal",
		},
		{
			name: "host in a served realm reached by suffix",
			host: "sgw.west.epc.example.com",
			want: true,
			why:  "a wildcard served realm covers the hosts beneath it",
		},
		{
			name: "host in a partner realm",
			host: "hss.partnera.com",
			want: false,
			why:  "a host in a partner's realm is the partner's even if it was never listed",
		},
		{
			name: "host in a partner subrealm",
			host: "hss.ims.partnera.com",
			want: false,
			why:  "the partner's wildcard realm covers its subrealms",
		},
		{
			name: "unrelated host",
			host: "hss.stranger.example.org",
			want: false,
			why:  "an identity matching nothing is not ours; defaulting to internal would leak trust",
		},
		{
			name: "a realm that merely ends in a served realm",
			host: "hss.not-example.com",
			want: false,
			why:  "suffix matching must respect label boundaries or any attacker-chosen realm ending in ours would pass",
		},
		{
			name: "empty identity",
			host: "",
			want: false,
			why:  "a missing identity cannot be proven internal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.IsInternalHost(tc.host); got != tc.want {
				t.Fatalf("IsInternalHost(%q) = %v, want %v: %s", tc.host, got, tc.want, tc.why)
			}
		})
	}
}

// Diameter identities are FQDNs, so classification must not depend on the case
// or the trailing root label a peer happens to send.
func TestIsInternalHostNormalizesTheIdentity(t *testing.T) {
	r := NewRegistry(testConfig(t, internalHostConfig))

	for _, host := range []string{
		"HSS.CORE.EXAMPLE.COM",
		"hss.core.example.com.",
		"Hss.Core.Example.Com.",
	} {
		if !r.IsInternalHost(host) {
			t.Errorf("IsInternalHost(%q) = false, want true: identities differing only in case or trailing dot are the same host", host)
		}
	}

	for _, host := range []string{"DEA1.PARTNERA.COM", "dea1.partnera.com."} {
		if r.IsInternalHost(host) {
			t.Errorf("IsInternalHost(%q) = true, want false: a partner peer stays a partner peer in any spelling", host)
		}
	}
}

// A partner realm that overlaps a served realm must resolve to the partner.
// Fail-safe requires the ambiguous case to be treated as external: masking an
// identity that did not need it costs nothing, exposing one costs the leak.
func TestIsInternalHostPrefersThePartnerWhenRealmsOverlap(t *testing.T) {
	const overlapping = `
zones:
  - name: core
    role: internal
    realms: ["shared.example.com"]
    listen:
      port: 3868
  - name: roaming
    role: external
    listen:
      port: 3869
partners:
  - name: partner-a
    zone: roaming
    realms: ["shared.example.com"]
    peers: ["dea1.shared.example.com"]
`
	r := NewRegistry(testConfig(t, overlapping))

	if r.IsInternalHost("anything.shared.example.com") {
		t.Fatal("a host in a realm claimed by both a partner and an internal zone was treated as internal")
	}
}

// Without an explicit model there is no edge, so nothing can be classified as
// internal: the dea_* controls must stay inert rather than start masking
// identities on a plain relay.
func TestIsInternalHostIsInertWithoutAModel(t *testing.T) {
	r := NewRegistry(testConfig(t, "listen:\n  port: 3868\n"))

	if r.Configured() {
		t.Fatal("registry reports a model where none is configured")
	}
	if !r.IsInternalHost("dea.example.com") {
		t.Fatal("the node should still recognise its own identity")
	}
	if r.IsInternalHost("anything.partnera.com") {
		t.Fatal("an unconfigured registry classified a foreign host as internal")
	}
}

// Zone and PartnersForZone back the admin endpoint and the per-zone listener
// setup, so an unknown name has to be reported as absent rather than as an
// empty zone that would silently admit nobody.
func TestZoneAndPartnerLookupByZone(t *testing.T) {
	r := NewRegistry(testConfig(t, sampleEdgeConfig))

	z, ok := r.Zone("roaming")
	if !ok || z.Name != "roaming" {
		t.Fatalf("Zone(\"roaming\") = %v, %v; want the roaming zone", z, ok)
	}
	// Zone matching is exact rather than case folded. That is safe only
	// because configuration validation rejects any reference to a zone name it
	// does not match exactly; see TestPartnerZoneReferenceMustMatchExactly.
	if _, ok := r.Zone("nosuchzone"); ok {
		t.Fatal("an unknown zone name was reported as present")
	}

	partners := r.PartnersForZone("roaming")
	if len(partners) != 2 {
		t.Fatalf("PartnersForZone(\"roaming\") returned %d partners, want 2", len(partners))
	}
	if partners[0].Name != "partner-a" || partners[1].Name != "partner-b" {
		t.Fatalf("partners = %q, %q; want declaration order", partners[0].Name, partners[1].Name)
	}
	if got := r.PartnersForZone("core"); len(got) != 0 {
		t.Fatalf("PartnersForZone(\"core\") returned %d partners, want none on an internal zone", len(got))
	}
	if got := r.PartnersForZone("nosuchzone"); got == nil || len(got) != 0 {
		t.Fatalf("PartnersForZone(\"nosuchzone\") = %v, want an empty non-nil slice", got)
	}
}
