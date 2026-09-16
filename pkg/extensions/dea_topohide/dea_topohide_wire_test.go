// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Tests for topology hiding applied to messages in the form they actually
// arrive in: decoded off the wire, where a grouped AVP is an undecoded blob
// until something asks for its children.
//
// The in-memory constructors used elsewhere in this package hand back AVPs
// that are already grouped, which skips the on-demand decode entirely. A
// message that came from a socket does not, and Proxy-Info is inspected rather
// than merely counted -- the decision to strip it or leave it rests on reading
// the Proxy-Host inside.

package dea_topohide

import (
	"testing"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// overTheWire encodes a message and decodes it again, producing the AVP shapes
// the extension sees in production.
func overTheWire(t *testing.T, msg *message.Message) *message.Message {
	t.Helper()

	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	decoded, err := message.DecodeMessage(data)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return decoded
}

// dropAVPs removes every AVP with the given code, so a test can replace a
// fixture's Proxy-Info with one of its own shape.
func dropAVPs(msg *message.Message, code types.AVPCode) {
	kept := msg.AVPs[:0]
	for _, avp := range msg.AVPs {
		if avp.Code != code {
			kept = append(kept, avp)
		}
	}
	msg.AVPs = kept
}

func proxyHosts(msg *message.Message) []string {
	var out []string
	for _, avp := range msg.FindAllAVPs(types.AVPCodeProxyInfo, 0) {
		for _, child := range groupedChildren(avp) {
			if child.Code == types.AVPCodeProxyHost {
				out = append(out, string(child.Data))
			}
		}
	}
	return out
}

// A Proxy-Info naming an internal proxy must be stripped even when it arrives
// undecoded. If the on-demand decode failed here, the Proxy-Host would be
// unreadable and every Proxy-Info would be removed -- which hides the leak but
// breaks the partner's own return routing.
func TestProxyInfoIsInspectedAfterDecoding(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-wire")
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "proxy.partnera.com"),
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("partner-state")),
	))

	wire := overTheWire(t, msg)
	if got := len(proxyHosts(wire)); got != 2 {
		t.Fatalf("the fixture carried %d readable Proxy-Host AVPs, want 2", got)
	}

	ext.handlePostRoute(wire, "dea1.static.com")

	hosts := proxyHosts(wire)
	if len(hosts) != 1 || hosts[0] != "proxy.partnera.com" {
		t.Fatalf("Proxy-Info hosts = %v, want only the partner's own proxy retained", hosts)
	}
}

// A Proxy-Info whose contents cannot be read must be removed. It cannot be
// proven safe, and a grouped AVP is exactly where an internal identity would
// be smuggled past a scrubber that only looks at the top level.
func TestUnreadableProxyInfoIsStripped(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-malformed")
	dropAVPs(msg, types.AVPCodeProxyInfo)
	// Too short to hold an AVP header, so it can never be decoded.
	msg.AddAVP(message.NewOctetStringAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, []byte{0x00, 0x01}))

	wire := overTheWire(t, msg)
	ext.handlePostRoute(wire, "dea1.static.com")

	if wire.FindAVP(types.AVPCodeProxyInfo, 0) != nil {
		t.Fatal("a Proxy-Info whose contents could not be read was forwarded to the partner")
	}
}

// A Proxy-Info carrying no Proxy-Host at all is equally unproven and must go
// the same way.
func TestProxyInfoWithoutAProxyHostIsStripped(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	msg := internalRequest("sess-noproxyhost")
	dropAVPs(msg, types.AVPCodeProxyInfo)
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("state-only")),
	))

	wire := overTheWire(t, msg)
	ext.handlePostRoute(wire, "dea1.static.com")

	if wire.FindAVP(types.AVPCodeProxyInfo, 0) != nil {
		t.Fatal("a Proxy-Info with no Proxy-Host was forwarded to the partner")
	}
}

// The whole scrub must survive the wire form, not just Proxy-Info: an
// identity that only leaks once the message has been through a socket would
// never be caught by the in-memory tests.
func TestStaticHidingScrubsAWireMessage(t *testing.T) {
	ext, _ := newExt(t, testConfigYAML, nil)

	wire := overTheWire(t, internalRequest("sess-wire-scrub"))
	ext.handlePostRoute(wire, "dea1.static.com")

	if got := avpValue(wire, types.AVPCodeOriginHost); got != "edge.example.com" {
		t.Errorf("Origin-Host = %q, want the edge identity", got)
	}
	if hasAVP(wire, types.AVPCodeOriginStateID) {
		t.Error("Origin-State-Id survived the wire form")
	}
	if hasAVP(wire, types.AVPCodeHostIPAddress) {
		t.Error("Host-IP-Address survived the wire form")
	}
	for _, r := range wire.FindAllAVPs(types.AVPCodeRouteRecord, 0) {
		if host := string(r.Data); host == "mme1.example.com" {
			t.Errorf("internal Route-Record %q survived the wire form", host)
		}
	}
}
