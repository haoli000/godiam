// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_rewrite

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

type rewriteInitContext struct {
	router *routing.Router
	dict   *dictionary.Dictionary
	cfg    *config.Config
}

func (m *rewriteInitContext) GetDictionary() *dictionary.Dictionary { return m.dict }
func (m *rewriteInitContext) GetRouter() *routing.Router            { return m.router }
func (m *rewriteInitContext) GetConfig() *config.Config             { return m.cfg }
func (m *rewriteInitContext) GetPeers() []*peer.Peer                { return nil }
func (m *rewriteInitContext) GetStartTime() time.Time               { return time.Now() }
func (m *rewriteInitContext) GetExtensionManager() *extension.Manager {
	return nil
}
func (m *rewriteInitContext) GetEdgeRegistry() *edge.Registry { return edge.NewRegistry(m.cfg) }

func newRewriteContext(t testing.TB) *rewriteInitContext {
	t.Helper()
	dict := dictionary.New()
	if err := dict.LoadBaseProtocol(); err != nil {
		t.Fatalf("loading dictionary: %v", err)
	}
	return &rewriteInitContext{
		router: routing.NewRouter(),
		dict:   dict,
		cfg:    &config.Config{Identity: "edge.example.com", Realm: "example.com"},
	}
}

func newRewriteExt(t testing.TB, cfg map[string]interface{}) (*rtRewrite, *rewriteInitContext) {
	t.Helper()
	ext := &rtRewrite{}
	ctx := newRewriteContext(t)
	if err := ext.Init(ctx, cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return ext, ctx
}

func rewriteRequest() *message.Message {
	msg := message.NewRequest(316, 16777251)
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "sid;1"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "mme.partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "partnera.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, "hss.example.com"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	return msg
}

func TestNameInitWithoutRulesAndStop(t *testing.T) {
	ext := &rtRewrite{}
	if ext.Name() != "rt_rewrite" {
		t.Fatalf("Name = %q, want rt_rewrite", ext.Name())
	}
	ctx := newRewriteContext(t)
	if err := ext.Init(ctx, map[string]interface{}{}); err != nil {
		t.Fatalf("Init without rules failed: %v", err)
	}
	if len(ext.rules) != 0 {
		t.Fatalf("rules = %d, want none", len(ext.rules))
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestInitLoadsValidRulesAndSkipsInvalidOnes(t *testing.T) {
	ext, _ := newRewriteExt(t, map[string]interface{}{
		"rules": []interface{}{
			"not-a-rule",
			map[string]interface{}{"map": map[string]interface{}{"destination": "Destination-Realm"}},
			map[string]interface{}{"map": map[string]interface{}{"source": "Origin-Realm"}},
			map[string]interface{}{"map": map[string]interface{}{"source": "No-Such-AVP", "destination": "Destination-Realm"}},
			map[string]interface{}{"map": map[string]interface{}{"source": "Origin-Realm", "destination": "No-Such-AVP"}},
			map[string]interface{}{"map": map[string]interface{}{"source": "Origin-Realm", "destination": "Destination-Realm"}},
			map[string]interface{}{"drop": "No-Such-AVP"},
			map[string]interface{}{"drop": "Destination-Host"},
		},
	})
	if len(ext.rules) != 2 {
		t.Fatalf("loaded rules = %d, want valid map and drop", len(ext.rules))
	}
	if ext.rules[0].isDrop || !ext.rules[1].isDrop {
		t.Fatalf("rule kinds = map:%v drop:%v, want map then drop", ext.rules[0].isDrop, ext.rules[1].isDrop)
	}
}

func TestRoutingAppliesMapAndDropRules(t *testing.T) {
	ext, ctx := newRewriteExt(t, map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{"map": map[string]interface{}{"source": "Origin-Realm", "destination": "Destination-Realm"}},
			map[string]interface{}{"drop": "Destination-Host"},
		},
	})
	msg := rewriteRequest()
	routes := []routing.Route{{PeerIdentity: "hss.example.com", Score: 10}}

	gotRoutes, err := ext.handleRouting(nil, msg, routes)
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if len(gotRoutes) != len(routes) || gotRoutes[0] != routes[0] {
		t.Fatalf("routes changed: got %+v want %+v", gotRoutes, routes)
	}
	if got, ok := msg.GetDestinationRealm(); !ok || got != "partnera.com" {
		t.Fatalf("Destination-Realm = %q (present=%v), want copied Origin-Realm", got, ok)
	}
	if _, ok := msg.GetDestinationHost(); ok {
		t.Fatal("Destination-Host survived DROP rule")
	}
	metrics := ext.Metrics()
	if metrics[0].Value != 1 || metrics[1].Value != 1 {
		t.Fatalf("metrics = map %v drop %v, want 1/1", metrics[0].Value, metrics[1].Value)
	}

	msg = rewriteRequest()
	ctx.router.AddRealmRoute("example.com", "next-hop.example.com")
	_ = ctx.router.RouteIn(peer.New(peer.Config{}, dictionary.New()), msg)
	if _, ok := msg.GetDestinationHost(); ok {
		t.Fatal("registered router did not apply DROP rule")
	}
}

func TestMapRuleCreatesMissingDestinationPath(t *testing.T) {
	ext := &rtRewrite{dict: newRewriteContext(t).dict}
	msg := rewriteRequest()
	for i, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeDestinationRealm {
			msg.AVPs = append(msg.AVPs[:i], msg.AVPs[i+1:]...)
			break
		}
	}

	ext.applyMapRule(msg, rule{sourceAVPPath: []string{"Origin-Realm"}, destAVPPath: []string{"Destination-Realm"}})
	if got, ok := msg.GetDestinationRealm(); !ok || got != "partnera.com" {
		t.Fatalf("Destination-Realm = %q (present=%v), want created from Origin-Realm", got, ok)
	}
}

func TestGroupedMapDeepCopiesChildren(t *testing.T) {
	ext := &rtRewrite{dict: newRewriteContext(t).dict}
	msg := rewriteRequest()
	source := message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "peer.example.com"),
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "opaque-state"),
	)
	msg.AddAVP(source)

	ext.applyMapRule(msg, rule{sourceAVPPath: []string{"Proxy-Info"}, destAVPPath: []string{"Failed-AVP"}})
	dest := msg.FindAVP(types.AVPCodeFailedAVP, 0)
	if dest == nil {
		t.Fatal("Failed-AVP was not created for grouped rewrite")
	}
	if len(dest.Children()) != 2 {
		t.Fatalf("copied child count = %d, want 2", len(dest.Children()))
	}
	source.Children()[0].Data = []byte("mutated-after-copy")
	if got := dest.Children()[0].GetDiameterIdentity(); got != "peer.example.com" {
		t.Fatalf("copied child changed with source mutation: %q", got)
	}
}

func TestDropNestedAVPAndMissingPathsAreNoOp(t *testing.T) {
	ext := &rtRewrite{dict: newRewriteContext(t).dict}
	msg := rewriteRequest()
	proxyInfo := message.NewGroupedAVP(
		types.AVPCodeProxyInfo,
		types.AVPFlagMandatory,
		0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "peer.example.com"),
		message.NewUTF8StringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, "state"),
	)
	msg.AddAVP(proxyInfo)

	ext.applyDropRule(msg, rule{sourceAVPPath: []string{"Proxy-Info", "Proxy-Host"}})
	if child := proxyInfo.FindChild(types.AVPCodeProxyHost, 0); child != nil {
		t.Fatal("nested Proxy-Host survived DROP rule")
	}
	if child := proxyInfo.FindChild(types.AVPCodeProxyState, 0); child == nil {
		t.Fatal("DROP rule removed a sibling child")
	}
	before := len(msg.AVPs)
	ext.applyDropRule(msg, rule{sourceAVPPath: nil})
	ext.applyDropRule(msg, rule{sourceAVPPath: []string{"Proxy-Info", "No-Such-AVP"}})
	ext.applyMapRule(msg, rule{sourceAVPPath: nil, destAVPPath: []string{"Destination-Realm"}})
	ext.applyMapRule(msg, rule{sourceAVPPath: []string{"No-Such-AVP"}, destAVPPath: []string{"Destination-Realm"}})
	if len(msg.AVPs) != before {
		t.Fatalf("missing-path no-op changed AVPs: got %d want %d", len(msg.AVPs), before)
	}
}

func TestDictionaryHelpersValidateTypesAndVendors(t *testing.T) {
	ext := &rtRewrite{dict: newRewriteContext(t).dict}
	if got := parseAVPPath("Proxy-Info:Proxy-State"); len(got) != 2 || got[0] != "Proxy-Info" || got[1] != "Proxy-State" {
		t.Fatalf("parseAVPPath = %#v, want two elements", got)
	}
	if !ext.validateAVPPath([]string{"Proxy-Info", "Proxy-State"}) {
		t.Fatal("valid Proxy-Info path was rejected")
	}
	if ext.validateAVPPath([]string{"Proxy-Info", "No-Such-AVP"}) {
		t.Fatal("unknown AVP path was accepted")
	}
	if !ext.checkTypeCompatibility([]string{"Origin-Realm"}, []string{"Destination-Realm"}) {
		t.Fatal("DiameterIdentity map was rejected")
	}
	if ext.checkTypeCompatibility([]string{"Origin-Realm"}, []string{"Result-Code"}) {
		t.Fatal("DiameterIdentity to Unsigned32 map was accepted")
	}
	if !ext.checkTypeCompatibility([]string{"Proxy-State"}, []string{"Result-Code"}) {
		t.Fatal("OctetString source should be accepted as a raw rewrite")
	}
	if ext.checkTypeCompatibility([]string{"No-Such-AVP"}, []string{"Result-Code"}) {
		t.Fatal("missing source type was accepted")
	}
	if code := ext.getAVPCodeByName("Destination-Host"); code != types.AVPCodeDestinationHost {
		t.Fatalf("Destination-Host code = %d, want %d", code, types.AVPCodeDestinationHost)
	}
	if code := ext.getAVPCodeByName("No-Such-AVP"); code != 0 {
		t.Fatalf("unknown AVP code = %d, want 0", code)
	}
	vendorAVP := message.NewVendorAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, 10415, []byte("mme.partnera.com"))
	if ext.avpMatchesName(vendorAVP, "Origin-Host") {
		t.Fatal("vendor-specific Origin-Host matched the base Origin-Host rule")
	}
}

func TestEnsurePathAndRemoveGuardInvalidInput(t *testing.T) {
	ext := &rtRewrite{dict: newRewriteContext(t).dict}
	msg := rewriteRequest()
	if got := ext.ensureAVPPath(msg, nil); got != nil {
		t.Fatalf("ensure empty path = %#v, want nil", got)
	}
	if got := ext.ensureAVPPath(msg, []string{"No-Such-AVP"}); got != nil {
		t.Fatalf("ensure unknown path = %#v, want nil", got)
	}
	before := len(msg.AVPs)
	ext.removeAVPFromMessage(msg, -1)
	ext.removeAVPFromMessage(msg, before)
	if len(msg.AVPs) != before {
		t.Fatalf("invalid remove changed AVPs: got %d want %d", len(msg.AVPs), before)
	}
}
