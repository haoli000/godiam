// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_ignore_dh

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

type ignoreDHInitContext struct {
	router *routing.Router
	dict   *dictionary.Dictionary
	cfg    *config.Config
}

func (m *ignoreDHInitContext) GetDictionary() *dictionary.Dictionary { return m.dict }
func (m *ignoreDHInitContext) GetRouter() *routing.Router            { return m.router }
func (m *ignoreDHInitContext) GetConfig() *config.Config             { return m.cfg }
func (m *ignoreDHInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *ignoreDHInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *ignoreDHInitContext) GetExtensionManager() *extension.Manager {
	return nil
}
func (m *ignoreDHInitContext) GetEdgeRegistry() *edge.Registry { return edge.NewRegistry(m.cfg) }

func newIgnoreDHContext(t testing.TB) *ignoreDHInitContext {
	t.Helper()
	dict := dictionary.New()
	if err := dict.LoadBaseProtocol(); err != nil {
		t.Fatalf("loading dictionary: %v", err)
	}
	return &ignoreDHInitContext{
		router: routing.NewRouter(),
		dict:   dict,
		cfg:    &config.Config{Identity: "edge.example.com", Realm: "example.com"},
	}
}

func ignoreDHRequest(destHost string) *message.Message {
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "mme.partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, types.DiamID(destHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	return msg
}

func ignoreDHMarker(msg *message.Message) *message.AVP {
	for _, avp := range msg.FindAllAVPs(types.AVPCodeProxyInfo, 0) {
		state := avp.FindChild(types.AVPCodeProxyState, 0)
		if state != nil && state.GetUTF8String() == "rt_ignore_dh:dest-host" {
			return avp
		}
	}
	return nil
}

func TestNameAndInit(t *testing.T) {
	ext := &rtIgnoreDH{}
	if ext.Name() != "rt_ignore_dh" {
		t.Fatalf("Name = %q, want rt_ignore_dh", ext.Name())
	}
	ctx := newIgnoreDHContext(t)
	if err := ext.Init(ctx, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	ctx.router.AddRealmRoute("example.com", "realm-peer.example.com")
	msg := ignoreDHRequest("direct-peer.example.com")
	_ = ctx.router.RouteIn(peer.New(peer.Config{}, dictionary.New()), msg)
	if _, ok := msg.GetDestinationHost(); ok {
		t.Fatal("Destination-Host survived incoming routing")
	}
}

func TestIncomingRequestDropsDestinationHostAndStashesIt(t *testing.T) {
	ext := &rtIgnoreDH{}
	msg := ignoreDHRequest("hss.example.com")
	routes := []routing.Route{{PeerIdentity: "hss.example.com", Score: 500}}

	gotRoutes, err := ext.handleRouting(nil, msg, routes)
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if len(gotRoutes) != len(routes) || gotRoutes[0] != routes[0] {
		t.Fatalf("routes changed: got %+v want %+v", gotRoutes, routes)
	}
	if _, ok := msg.GetDestinationHost(); ok {
		t.Fatal("Destination-Host was still present; realm routing would remain bypassed")
	}
	marker := ignoreDHMarker(msg)
	if marker == nil {
		t.Fatal("original Destination-Host was not stashed for restoration")
	}
	if host := marker.FindChild(types.AVPCodeProxyHost, 0); host == nil || host.GetDiameterIdentity() != "hss.example.com" {
		t.Fatalf("stashed host = %#v, want hss.example.com", host)
	}
	if got := ext.Metrics()[0].Value; got != 1 {
		t.Fatalf("headers_dropped metric = %v, want 1", got)
	}
}

func TestIncomingAnswerAndRequestWithoutDestinationHostAreLeftAlone(t *testing.T) {
	ext := &rtIgnoreDH{}
	answer := message.NewAnswer(ignoreDHRequest("hss.example.com"))
	answer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "client.example.com"))
	if _, err := ext.handleRouting(nil, answer, nil); err != nil {
		t.Fatalf("answer routing failed: %v", err)
	}
	if got, ok := answer.GetDestinationHost(); !ok || got != "client.example.com" {
		t.Fatalf("answer Destination-Host = %q (present=%v), want untouched", got, ok)
	}

	request := message.NewRequest(316, 16777251)
	if _, err := ext.handleRouting(nil, request, nil); err != nil {
		t.Fatalf("request routing failed: %v", err)
	}
	if marker := ignoreDHMarker(request); marker != nil {
		t.Fatal("Proxy-Info marker was added when there was no Destination-Host")
	}
}

func TestOutgoingRestoresDestinationHostAndRemovesMarker(t *testing.T) {
	ext := &rtIgnoreDH{}
	msg := ignoreDHRequest("hss.example.com")
	if _, err := ext.handleRouting(nil, msg, nil); err != nil {
		t.Fatalf("incoming routing failed: %v", err)
	}

	if _, err := ext.handleOutgoing(msg, nil); err != nil {
		t.Fatalf("outgoing routing failed: %v", err)
	}
	if got, ok := msg.GetDestinationHost(); !ok || got != "hss.example.com" {
		t.Fatalf("restored Destination-Host = %q (present=%v), want hss.example.com", got, ok)
	}
	if marker := ignoreDHMarker(msg); marker != nil {
		t.Fatal("rt_ignore_dh Proxy-Info marker leaked after restoration")
	}
	metrics := ext.Metrics()
	if metrics[0].Value != 1 || metrics[1].Value != 1 {
		t.Fatalf("metrics = drop %v restore %v, want 1/1", metrics[0].Value, metrics[1].Value)
	}
}

func TestOutgoingSkipsAnswersExistingDestinationHostAndForeignMarkers(t *testing.T) {
	ext := &rtIgnoreDH{}
	answer := message.NewAnswer(ignoreDHRequest("hss.example.com"))
	if _, err := ext.handleOutgoing(answer, nil); err != nil {
		t.Fatalf("answer outgoing failed: %v", err)
	}
	if _, ok := answer.GetDestinationHost(); ok {
		t.Fatal("Destination-Host was added to an answer")
	}

	withHost := ignoreDHRequest("already.example.com")
	withHost.AddAVP(message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "hidden.example.com"),
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "rt_ignore_dh:dest-host"),
	))
	if _, err := ext.handleOutgoing(withHost, nil); err != nil {
		t.Fatalf("outgoing with host failed: %v", err)
	}
	if got, _ := withHost.GetDestinationHost(); got != "already.example.com" {
		t.Fatalf("existing Destination-Host = %q, want unchanged", got)
	}

	foreign := message.NewRequest(316, 16777251)
	foreign.AddAVP(message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "hidden.example.com"),
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "other-extension"),
	))
	if _, err := ext.handleOutgoing(foreign, nil); err != nil {
		t.Fatalf("foreign outgoing failed: %v", err)
	}
	if marker := foreign.FindAVP(types.AVPCodeProxyInfo, 0); marker == nil {
		t.Fatal("foreign Proxy-Info was removed")
	}
}

func TestOutgoingRemovesMarkerEvenWhenHostIsMissing(t *testing.T) {
	ext := &rtIgnoreDH{}
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "rt_ignore_dh:dest-host"),
	))

	if _, err := ext.handleOutgoing(msg, nil); err != nil {
		t.Fatalf("outgoing failed: %v", err)
	}
	if marker := ignoreDHMarker(msg); marker != nil {
		t.Fatal("marker without Proxy-Host was not removed")
	}
	if _, ok := msg.GetDestinationHost(); ok {
		t.Fatal("Destination-Host was fabricated from a marker without Proxy-Host")
	}
}

func TestStopSucceeds(t *testing.T) {
	if err := (&rtIgnoreDH{}).Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
