// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package app_cx

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

type cxInitContext struct{ router *routing.Router }

func (c cxInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (c cxInitContext) GetRouter() *routing.Router              { return c.router }
func (c cxInitContext) GetConfig() *config.Config               { return nil }
func (c cxInitContext) GetPeers() []*peer.Peer                  { return nil }
func (c cxInitContext) GetStartTime() time.Time                 { return time.Now() }
func (c cxInitContext) GetExtensionManager() *extension.Manager { return nil }
func (c cxInitContext) GetEdgeRegistry() *edge.Registry         { return nil }

func TestNameAndStop(t *testing.T) {
	ext := &appCx{}
	if ext.Name() != "app_cx" {
		t.Fatalf("Name() = %q, want app_cx", ext.Name())
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}
}

func TestInitRegistersCxAndNASREQApplications(t *testing.T) {
	r := routing.NewRouter()
	ext := &appCx{}
	if err := ext.Init(cxInitContext{router: r}, nil); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	p, remote := openCxPeer(t)
	uar := cxRequest(dictionary.CmdCode3GPPUserAuthorization, true)
	if err := r.Dispatch(p, uar); err != nil {
		t.Fatalf("dispatching UAR through registered 3GPP Cx application: %v", err)
	}
	uaa := readDiameterMessage(t, remote)
	assertBaseAnswer(t, uaa, uar, types.AppID3GPPCx)
	assertOrigin(t, uaa)
	assertVendorSpecificAppID(t, uaa, types.AppID3GPPCx)

	aar := message.NewRequest(types.CmdCodeAAReq, types.AppIDNASREQ)
	aar.HopByHopID = 0x3333
	aar.EndToEndID = 0x4444
	aar.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "nasreq-session;1"))
	if err := r.Dispatch(p, aar); err != nil {
		t.Fatalf("dispatching AAR through registered NASREQ application: %v", err)
	}
	aaa := readDiameterMessage(t, remote)
	assertBaseAnswer(t, aaa, aar, types.AppIDNASREQ)
	assertOrigin(t, aaa)
}

func TestCxCommandCatalogue(t *testing.T) {
	p, remote := openCxPeer(t)
	for _, code := range []types.CommandCode{dictionary.CmdCode3GPPMultimediaAuthentication, dictionary.CmdCode3GPPServerAssignment, dictionary.CmdCode3GPPLocationInfo} {
		t.Run("with Session-ID", func(t *testing.T) {
			req := cxRequest(code, true)
			if err := handleCx(p, req); err != nil {
				t.Fatalf("handling Cx command %d: %v", code, err)
			}
			ans := readDiameterMessage(t, remote)
			assertBaseAnswer(t, ans, req, types.AppID3GPPCx)
			assertAnswerIdentity(t, ans, "cx-session;1", true)
		})

		t.Run("without Session-ID", func(t *testing.T) {
			req := cxRequest(code, false)
			if err := handleCx(p, req); err != nil {
				t.Fatalf("handling Cx command %d without Session-ID: %v", code, err)
			}
			ans := readDiameterMessage(t, remote)
			assertBaseAnswer(t, ans, req, types.AppID3GPPCx)
			assertAnswerIdentity(t, ans, "", false)
		})
	}
}

func TestCxIgnoresAnswersAndUnhandledRequests(t *testing.T) {
	p, remote := openCxPeer(t)
	req := cxRequest(dictionary.CmdCode3GPPUserAuthorization, true)
	if err := handleCx(p, message.NewAnswer(req)); err != nil {
		t.Fatalf("handling UAA: %v", err)
	}
	assertNoDiameterMessage(t, remote)

	unknown := cxRequest(dictionary.CmdCode3GPPRegistrationTermination, true)
	if err := handleCx(p, unknown); err != nil {
		t.Fatalf("handling unsupported Cx request: %v", err)
	}
	assertNoDiameterMessage(t, remote)
}

func TestNASREQIgnoresAnswers(t *testing.T) {
	p, remote := openCxPeer(t)
	req := message.NewRequest(types.CmdCodeAAReq, types.AppIDNASREQ)
	if err := handleNASREQ(p, message.NewAnswer(req)); err != nil {
		t.Fatalf("handling AAA: %v", err)
	}
	assertNoDiameterMessage(t, remote)
}

func cxRequest(code types.CommandCode, withSession bool) *message.Message {
	msg := message.NewRequest(code, types.AppID3GPPCx)
	msg.HopByHopID = 0x01020304 + types.HopByHopID(code)
	msg.EndToEndID = 0x05060708 + types.EndToEndID(code)
	if withSession {
		msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "cx-session;1"))
	}
	return msg
}

func openCxPeer(t *testing.T) (*peer.Peer, net.Conn) {
	t.Helper()
	return openAppPeer(t, []types.ApplicationID{types.AppID3GPPCx, types.AppIDNASREQ}, nil)
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
	assertUnsigned32(t, vapp.FindChild(types.AVPCodeVendorID, 0), uint32(types.VendorID3GPP), "Vendor-ID")
	assertUnsigned32(t, vapp.FindChild(types.AVPCodeAuthApplicationID, 0), uint32(appID), "Auth-Application-ID")
}

func assertUnsigned32(t *testing.T, avp *message.AVP, want uint32, name string) {
	t.Helper()
	if avp == nil {
		t.Fatalf("%s missing", name)
	}
	got, err := avp.GetUnsigned32()
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}
