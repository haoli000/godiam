// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package app_s6a

import (
	"io"
	"net"
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

type s6aInitContext struct{ router *routing.Router }

func (c s6aInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (c s6aInitContext) GetRouter() *routing.Router              { return c.router }
func (c s6aInitContext) GetConfig() *config.Config               { return nil }
func (c s6aInitContext) GetPeers() []*peer.Peer                  { return nil }
func (c s6aInitContext) GetStartTime() time.Time                 { return time.Now() }
func (c s6aInitContext) GetExtensionManager() *extension.Manager { return nil }
func (c s6aInitContext) GetEdgeRegistry() *edge.Registry         { return nil }

func TestNameAndStop(t *testing.T) {
	ext := &appS6a{}
	if ext.Name() != "app_s6a" {
		t.Fatalf("Name() = %q, want app_s6a", ext.Name())
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}
}

func TestInitRegistersS6aApplication(t *testing.T) {
	r := routing.NewRouter()
	ext := &appS6a{}
	if err := ext.Init(s6aInitContext{router: r}, nil); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	p, remote := openS6aPeer(t)
	req := s6aRequest(dictionary.CmdCode3GPPUpdateLocation, true)
	if err := r.Dispatch(p, req); err != nil {
		t.Fatalf("dispatching ULR through registered 3GPP S6a application: %v", err)
	}
	ans := readDiameterMessage(t, remote)

	assertBaseAnswer(t, ans, req, types.AppID3GPPS6a)
	assertOrigin(t, ans)
	assertVendorSpecificAppID(t, ans, types.AppID3GPPS6a)
	ulaFlags := ans.FindAVP(dictionary.AVPCode3GPPULAFlags, types.VendorID3GPP)
	assertVendorUnsigned32(t, ulaFlags, 0, dictionary.AVPCode3GPPULAFlags)
	sub := ans.FindAVP(dictionary.AVPCode3GPPSubscriptionData, types.VendorID3GPP)
	if sub == nil {
		t.Fatal("ULA missing 3GPP Subscription-Data")
	}
	if err := sub.DecodeGrouped(); err != nil {
		t.Fatalf("decoding Subscription-Data: %v", err)
	}
	assertVendorUnsigned32(t, sub.FindChild(dictionary.AVPCode3GPPSubscriberStatus, types.VendorID3GPP), 0, dictionary.AVPCode3GPPSubscriberStatus)
	assertVendorUnsigned32(t, sub.FindChild(dictionary.AVPCode3GPPNetworkAccessMode, types.VendorID3GPP), 2, dictionary.AVPCode3GPPNetworkAccessMode)
}

func TestS6aCommandCatalogue(t *testing.T) {
	p, remote := openS6aPeer(t)
	tests := []struct {
		name string
		code types.CommandCode
		want func(*testing.T, *message.Message)
	}{
		{
			name: "AIR returns one E-UTRAN authentication vector",
			code: dictionary.CmdCode3GPPAuthenticationInformation,
			want: func(t *testing.T, ans *message.Message) {
				authInfo := ans.FindAVP(dictionary.AVPCode3GPPAuthenticationInfo, types.VendorID3GPP)
				if authInfo == nil {
					t.Fatal("AIA missing 3GPP Authentication-Info")
				}
				if err := authInfo.DecodeGrouped(); err != nil {
					t.Fatalf("decoding Authentication-Info: %v", err)
				}
				vector := authInfo.FindChild(dictionary.AVPCode3GPPEUTRANVector, types.VendorID3GPP)
				if vector == nil {
					t.Fatal("AIA missing E-UTRAN-Vector")
				}
				if err := vector.DecodeGrouped(); err != nil {
					t.Fatalf("decoding E-UTRAN-Vector: %v", err)
				}
				for _, code := range []types.AVPCode{dictionary.AVPCode3GPPRand, dictionary.AVPCode3GPPAUTN, dictionary.AVPCode3GPPXRES, dictionary.AVPCode3GPPKASME} {
					if vector.FindChild(code, types.VendorID3GPP) == nil {
						t.Fatalf("E-UTRAN-Vector missing AVP code %d", code)
					}
				}
			},
		},
		{
			name: "PUR answers with the same 3GPP command identifiers",
			code: dictionary.CmdCode3GPPPurgeUE,
			want: func(t *testing.T, ans *message.Message) {
				if ans.FindAVP(types.AVPCodeResultCode, 0) == nil {
					t.Fatal("PUA missing Result-Code")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := s6aRequest(tc.code, true)
			if err := handleS6a(p, req); err != nil {
				t.Fatalf("handling S6a command %d: %v", tc.code, err)
			}
			ans := readDiameterMessage(t, remote)
			assertBaseAnswer(t, ans, req, types.AppID3GPPS6a)
			if tc.code == dictionary.CmdCode3GPPAuthenticationInformation {
				assertOrigin(t, ans)
			} else {
				assertAnswerIdentity(t, ans, "s6a-session;1", true)
			}
			tc.want(t, ans)
		})
	}
}

func TestPurgeAnswerIdentityWithoutSessionID(t *testing.T) {
	p, remote := openS6aPeer(t)
	req := s6aRequest(dictionary.CmdCode3GPPPurgeUE, false)
	if err := handleS6a(p, req); err != nil {
		t.Fatalf("handling PUR without Session-ID: %v", err)
	}
	ans := readDiameterMessage(t, remote)
	assertBaseAnswer(t, ans, req, types.AppID3GPPS6a)
	assertAnswerIdentity(t, ans, "", false)
}

func TestS6aIgnoresAnswersAndUnhandledRequests(t *testing.T) {
	p, remote := openS6aPeer(t)
	req := s6aRequest(dictionary.CmdCode3GPPUpdateLocation, true)
	if err := handleS6a(p, message.NewAnswer(req)); err != nil {
		t.Fatalf("handling ULA: %v", err)
	}
	assertNoDiameterMessage(t, remote)

	unknown := s6aRequest(dictionary.CmdCode3GPPCancelLocation, true)
	if err := handleS6a(p, unknown); err != nil {
		t.Fatalf("handling unsupported S6a request: %v", err)
	}
	assertNoDiameterMessage(t, remote)
}

func s6aRequest(code types.CommandCode, withSession bool) *message.Message {
	msg := message.NewRequest(code, types.AppID3GPPS6a)
	msg.HopByHopID = 0x01020304 + types.HopByHopID(code)
	msg.EndToEndID = 0x05060708 + types.EndToEndID(code)
	if withSession {
		msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "s6a-session;1"))
	}
	return msg
}

func openS6aPeer(t *testing.T) (*peer.Peer, net.Conn) {
	t.Helper()
	return openAppPeer(t, []types.ApplicationID{types.AppID3GPPS6a}, nil)
}

func openAppPeer(t *testing.T, authApps []types.ApplicationID, acctApps []types.ApplicationID) (*peer.Peer, net.Conn) {
	t.Helper()
	saved := peer.GetLocalConfig()
	peer.SetLocalConfig(peer.LocalConfig{DiameterIdentity: "agent.example.com", Realm: "example.com", ProductName: "godiam-test", OriginStateID: 42, AuthAppIDs: authApps, AcctAppIDs: acctApps})
	t.Cleanup(func() { peer.SetLocalConfig(saved) })

	p := peer.New(peer.Config{DiameterIdentity: "remote.example.com", Realm: "remote.example.com"}, dictionary.New())
	serverSide, remoteSide := net.Pipe()
	cer := message.NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	cer.HopByHopID = 0x1000
	cer.EndToEndID = 0x2000
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "remote.example.com"))
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "remote.example.com"))
	if err := p.StartWithConnection(serverSide, cer); err != nil {
		t.Fatalf("StartWithConnection: %v", err)
	}
	t.Cleanup(func() { p.Stop(); _ = remoteSide.Close() })
	cea := readDiameterMessage(t, remoteSide)
	if cea.CommandCode != types.CmdCodeCapabilitiesExchange || cea.IsRequest() {
		t.Fatalf("opening peer read command %d request=%v, want CEA", cea.CommandCode, cea.IsRequest())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !p.IsOpen() {
		time.Sleep(time.Millisecond)
	}
	if !p.IsOpen() {
		t.Fatalf("peer state = %s, want open before dispatching application traffic", p.State())
	}
	return p, remoteSide
}

func readDiameterMessage(t *testing.T, conn net.Conn) *message.Message {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting read deadline: %v", err)
	}
	header := make([]byte, types.DiameterHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatalf("reading Diameter header: %v", err)
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	body := make([]byte, length-types.DiameterHeaderSize)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatalf("reading Diameter body: %v", err)
	}
	msg, err := message.DecodeMessage(append(header, body...))
	if err != nil {
		t.Fatalf("decoding Diameter frame: %v", err)
	}
	return msg
}

func assertNoDiameterMessage(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("setting read deadline: %v", err)
	}
	buf := make([]byte, 1)
	if n, err := conn.Read(buf); n != 0 || err == nil {
		t.Fatalf("read %d byte(s), want no Diameter frame", n)
	}
}

func assertBaseAnswer(t *testing.T, got, req *message.Message, appID types.ApplicationID) {
	t.Helper()
	if got.IsRequest() || got.CommandCode != req.CommandCode || got.ApplicationID != appID {
		t.Fatalf("answer header command=%d app=%d request=%v, want command=%d app=%d answer", got.CommandCode, got.ApplicationID, got.IsRequest(), req.CommandCode, appID)
	}
	if got.HopByHopID != req.HopByHopID || got.EndToEndID != req.EndToEndID {
		t.Fatalf("answer identifiers hbh=%d e2e=%d, want hbh=%d e2e=%d", got.HopByHopID, got.EndToEndID, req.HopByHopID, req.EndToEndID)
	}
	if code, ok := got.GetResultCode(); !ok || code != types.ResultSuccess {
		t.Fatalf("Result-Code = %d (present=%v), want DIAMETER_SUCCESS", code, ok)
	}
}

func assertOrigin(t *testing.T, msg *message.Message) {
	t.Helper()
	if host, ok := msg.GetOriginHost(); !ok || host != "agent.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want local agent identity", host, ok)
	}
	if realm, ok := msg.GetOriginRealm(); !ok || realm != "example.com" {
		t.Fatalf("Origin-Realm = %q (present=%v), want local realm", realm, ok)
	}
}

func assertAnswerIdentity(t *testing.T, msg *message.Message, wantSession string, wantSessionPresent bool) {
	t.Helper()
	assertOrigin(t, msg)
	sessionID, ok := msg.GetSessionID()
	if ok != wantSessionPresent {
		t.Fatalf("Session-ID present = %v, want %v", ok, wantSessionPresent)
	}
	if ok && sessionID != wantSession {
		t.Fatalf("Session-ID = %q, want %q", sessionID, wantSession)
	}
}

func assertVendorSpecificAppID(t *testing.T, msg *message.Message, appID types.ApplicationID) {
	t.Helper()
	vapp := msg.FindAVP(types.AVPCodeVendorSpecificAppID, 0)
	if vapp == nil {
		t.Fatal("Vendor-Specific-Application-ID missing")
	}
	if err := vapp.DecodeGrouped(); err != nil {
		t.Fatalf("decoding Vendor-Specific-Application-ID: %v", err)
	}
	assertVendorUnsigned32(t, vapp.FindChild(types.AVPCodeVendorID, 0), uint32(types.VendorID3GPP), types.AVPCodeVendorID)
	assertVendorUnsigned32(t, vapp.FindChild(types.AVPCodeAuthApplicationID, 0), uint32(appID), types.AVPCodeAuthApplicationID)
}

func assertVendorUnsigned32(t *testing.T, avp *message.AVP, want uint32, code types.AVPCode) {
	t.Helper()
	if avp == nil {
		t.Fatalf("AVP code %d missing", code)
	}
	if avp.VendorID != 0 && avp.VendorID != types.VendorID3GPP {
		t.Fatalf("AVP code %d vendor = %d, want 3GPP or base", code, avp.VendorID)
	}
	got, err := avp.GetUnsigned32()
	if err != nil {
		t.Fatalf("reading AVP code %d: %v", code, err)
	}
	if got != want {
		t.Fatalf("AVP code %d = %d, want %d", code, got, want)
	}
}
