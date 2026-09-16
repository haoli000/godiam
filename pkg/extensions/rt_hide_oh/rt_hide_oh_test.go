// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_hide_oh

import (
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type hideOHInitContext struct {
	router *routing.Router
	dict   *dictionary.Dictionary
	cfg    *config.Config
}

func (m *hideOHInitContext) GetDictionary() *dictionary.Dictionary { return m.dict }
func (m *hideOHInitContext) GetRouter() *routing.Router            { return m.router }
func (m *hideOHInitContext) GetConfig() *config.Config             { return m.cfg }
func (m *hideOHInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *hideOHInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *hideOHInitContext) GetExtensionManager() *extension.Manager {
	return nil
}
func (m *hideOHInitContext) GetEdgeRegistry() *edge.Registry { return edge.NewRegistry(m.cfg) }

func newHideOHContext(t testing.TB, identity string) *hideOHInitContext {
	t.Helper()
	dict := dictionary.New()
	if err := dict.LoadBaseProtocol(); err != nil {
		t.Fatalf("loading dictionary: %v", err)
	}
	return &hideOHInitContext{
		router: routing.NewRouter(),
		dict:   dict,
		cfg:    &config.Config{Identity: identity, Realm: "example.com"},
	}
}

func newHideOHPeer(t testing.TB) *peer.Peer {
	t.Helper()
	p := peer.New(peer.Config{DiameterIdentity: "peer.example.com", Realm: "example.com"}, dictionary.New())
	t.Cleanup(p.Stop)
	return p
}

func hideOHRequest(originHost string) *message.Message {
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(originHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	return msg
}

func findProxyInfoByState(msg *message.Message, marker string) *message.AVP {
	for _, avp := range msg.FindAllAVPs(types.AVPCodeProxyInfo, 0) {
		state := avp.FindChild(types.AVPCodeProxyState, 0)
		if state != nil && state.GetUTF8String() == marker {
			return avp
		}
	}
	return nil
}

func TestNameAndInit(t *testing.T) {
	ext := &rtHideOH{}
	if ext.Name() != "rt_hide_oh" {
		t.Fatalf("Name = %q, want rt_hide_oh", ext.Name())
	}

	ctx := newHideOHContext(t, "edge.example.com")
	if err := ext.Init(ctx, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if ext.localIdentity != "edge.example.com" {
		t.Fatalf("local identity = %q, want edge.example.com", ext.localIdentity)
	}

	msg := hideOHRequest("ue.partnera.com")
	ctx.router.AddRealmRoute("example.com", "next-hop.example.com")
	_ = ctx.router.RouteIn(newHideOHPeer(t), msg)
	if got, ok := msg.GetOriginHost(); !ok || got != "edge.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want edge identity", got, ok)
	}
}

func TestRequestOriginHostIsHiddenAndMarked(t *testing.T) {
	ext := &rtHideOH{localIdentity: "edge.example.com"}
	msg := hideOHRequest("mme.partnera.com")
	routes := []routing.Route{{PeerIdentity: "next-hop.example.com", Score: 10}}

	gotRoutes, err := ext.handleRouting(newHideOHPeer(t), msg, routes)
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if len(gotRoutes) != len(routes) || gotRoutes[0] != routes[0] {
		t.Fatalf("routes changed: got %+v want %+v", gotRoutes, routes)
	}
	if got, ok := msg.GetOriginHost(); !ok || got != "edge.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want edge identity", got, ok)
	}
	marker := findProxyInfoByState(msg, proxyStateMarker)
	if marker == nil {
		t.Fatal("Proxy-Info marker missing; answers would leak hop-by-hop state back to partners")
	}
	if host := marker.FindChild(types.AVPCodeProxyHost, 0); host == nil || host.GetUTF8String() != "hidden" {
		t.Fatalf("Proxy-Host marker = %#v, want hidden", host)
	}
	if got := ext.Metrics()[0].Value; got != 1 {
		t.Fatalf("headers_replaced metric = %v, want 1", got)
	}
}

func TestRequestWithoutOriginHostIsLeftAlone(t *testing.T) {
	ext := &rtHideOH{localIdentity: "edge.example.com"}
	msg := message.NewRequest(316, 16777251)

	if _, err := ext.handleRouting(newHideOHPeer(t), msg, nil); err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if _, ok := msg.GetOriginHost(); ok {
		t.Fatal("Origin-Host was invented for a malformed request")
	}
	if got := ext.Metrics()[0].Value; got != 0 {
		t.Fatalf("headers_replaced metric = %v, want 0", got)
	}
}

func TestAnswerMarkerIsRemovedFromGroupedAVP(t *testing.T) {
	ext := &rtHideOH{}
	msg := message.NewAnswer(hideOHRequest("edge.example.com"))
	foreign := message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "other-extension"),
	)
	ours := message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, proxyStateMarker),
	)
	msg.AddAVP(foreign)
	msg.AddAVP(ours)

	if _, err := ext.handleRouting(newHideOHPeer(t), msg, nil); err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if findProxyInfoByState(msg, proxyStateMarker) != nil {
		t.Fatal("rt_hide_oh marker was forwarded in an answer")
	}
	if findProxyInfoByState(msg, "other-extension") == nil {
		t.Fatal("foreign Proxy-Info marker was removed")
	}
	if got := ext.Metrics()[1].Value; got != 1 {
		t.Fatalf("markers_removed metric = %v, want 1", got)
	}
}

func TestAnswerMarkerIsRemovedAfterDecodingProxyInfoData(t *testing.T) {
	ext := &rtHideOH{}
	encoded, err := message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, proxyStateMarker),
	).Encode()
	if err != nil {
		t.Fatalf("encoding marker: %v", err)
	}
	msg := message.NewAnswer(hideOHRequest("edge.example.com"))
	msg.AddAVP(message.NewAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0, encoded[8:]))

	if _, err := ext.handleRouting(newHideOHPeer(t), msg, nil); err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if len(msg.FindAllAVPs(types.AVPCodeProxyInfo, 0)) != 0 {
		t.Fatal("encoded rt_hide_oh marker was not removed")
	}
}

func TestOutgoingIsNoOpAndStopSucceeds(t *testing.T) {
	ext := &rtHideOH{}
	routes := []routing.Route{{PeerIdentity: "next-hop.example.com", Score: 10}}
	got, err := ext.handleOutgoing(hideOHRequest("edge.example.com"), routes)
	if err != nil {
		t.Fatalf("outgoing failed: %v", err)
	}
	if len(got) != len(routes) || got[0] != routes[0] {
		t.Fatalf("routes changed: got %+v want %+v", got, routes)
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
