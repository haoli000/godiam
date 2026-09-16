// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package app_gx

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

type gxInitContext struct{ router *routing.Router }

func (c gxInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (c gxInitContext) GetRouter() *routing.Router              { return c.router }
func (c gxInitContext) GetConfig() *config.Config               { return nil }
func (c gxInitContext) GetPeers() []*peer.Peer                  { return nil }
func (c gxInitContext) GetStartTime() time.Time                 { return time.Now() }
func (c gxInitContext) GetExtensionManager() *extension.Manager { return nil }
func (c gxInitContext) GetEdgeRegistry() *edge.Registry         { return nil }

func TestNameAndStop(t *testing.T) {
	ext := &appGx{}
	if ext.Name() != "app_gx" {
		t.Fatalf("Name() = %q, want app_gx", ext.Name())
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}
}

func TestInitRegistersGxApplication(t *testing.T) {
	r := routing.NewRouter()
	ext := &appGx{}
	if err := ext.Init(gxInitContext{router: r}, nil); err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	p, remote := openGxPeer(t)
	req := gxCCR()
	if err := r.Dispatch(p, req); err != nil {
		t.Fatalf("dispatching CCR through registered 3GPP Gx application: %v", err)
	}
	ans := readDiameterMessage(t, remote)

	assertBaseAnswer(t, ans, req, types.AppID3GPPGx)
	assertVendorSpecificAppID(t, ans, types.AppID3GPPGx)
	assertUnsigned32(t, ans.FindAVP(types.AVPCodeCCRequestType, 0), 1, "CC-Request-Type")
	assertUnsigned32(t, ans.FindAVP(types.AVPCodeCCRequestNumber, 0), 23, "CC-Request-Number")
}

func TestGxIgnoresAnswersAndUnhandledRequests(t *testing.T) {
	p, remote := openGxPeer(t)
	req := gxCCR()
	if err := handleGx(p, message.NewAnswer(req)); err != nil {
		t.Fatalf("handling Gx CCA: %v", err)
	}
	assertNoDiameterMessage(t, remote)

	unknown := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppID3GPPGx)
	if err := handleGx(p, unknown); err != nil {
		t.Fatalf("handling unsupported Gx request: %v", err)
	}
	assertNoDiameterMessage(t, remote)
}

func TestGxDoesNotInventOptionalEchoes(t *testing.T) {
	p, remote := openGxPeer(t)
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppID3GPPGx)
	req.HopByHopID = 0x01020304
	req.EndToEndID = 0x05060708

	if err := handleGx(p, req); err != nil {
		t.Fatalf("handling minimal Gx CCR: %v", err)
	}
	ans := readDiameterMessage(t, remote)

	assertBaseAnswer(t, ans, req, types.AppID3GPPGx)
	assertVendorSpecificAppID(t, ans, types.AppID3GPPGx)
	if ans.FindAVP(types.AVPCodeSessionID, 0) != nil || ans.FindAVP(types.AVPCodeCCRequestType, 0) != nil || ans.FindAVP(types.AVPCodeCCRequestNumber, 0) != nil {
		t.Fatal("Gx CCA invented request AVPs that were absent from the CCR")
	}
}

func gxCCR() *message.Message {
	msg := message.NewRequest(types.CmdCodeCreditControl, types.AppID3GPPGx)
	msg.HopByHopID = 0x01020304
	msg.EndToEndID = 0x05060708
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "gx-session;1"))
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestType, types.AVPFlagMandatory, 1))
	msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeCCRequestNumber, types.AVPFlagMandatory, 23))
	return msg
}

func openGxPeer(t *testing.T) (*peer.Peer, net.Conn) {
	t.Helper()
	return openAppPeer(t, []types.ApplicationID{types.AppID3GPPGx}, nil)
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
	if host, ok := got.GetOriginHost(); !ok || host != "agent.example.com" {
		t.Fatalf("Origin-Host = %q (present=%v), want local agent identity", host, ok)
	}
	if realm, ok := got.GetOriginRealm(); !ok || realm != "example.com" {
		t.Fatalf("Origin-Realm = %q (present=%v), want local realm", realm, ok)
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
