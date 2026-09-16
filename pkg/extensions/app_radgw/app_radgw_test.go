// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package app_radgw

import (
	"context"
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
	"github.com/haoli000/godiam/pkg/proto/radius"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type radgwInitContext struct{ router *routing.Router }

func (c radgwInitContext) GetDictionary() *dictionary.Dictionary   { return nil }
func (c radgwInitContext) GetRouter() *routing.Router              { return c.router }
func (c radgwInitContext) GetConfig() *config.Config               { return &config.Config{} }
func (c radgwInitContext) GetPeers() []*peer.Peer                  { return nil }
func (c radgwInitContext) GetStartTime() time.Time                 { return time.Now() }
func (c radgwInitContext) GetExtensionManager() *extension.Manager { return nil }
func (c radgwInitContext) GetEdgeRegistry() *edge.Registry         { return nil }

func TestNameAndStopWithoutStart(t *testing.T) {
	ext := &appRadGW{}
	if ext.Name() != "app_radgw" {
		t.Fatal("unexpected extension name")
	}
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestInitDefaultsAndRegistersNASREQHandler(t *testing.T) {
	peer.SetLocalConfig(peer.LocalConfig{Realm: "example.com"})
	router := routing.NewRouter()
	ext := &appRadGW{}
	if err := ext.Init(radgwInitContext{router: router}, map[string]interface{}{"radius_port": float64(0)}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop() })

	if ext.radiusPort != 0 || ext.sharedSecret != "secret" || ext.destRealm != "example.com" {
		t.Fatalf("defaults = port %d secret %q realm %q", ext.radiusPort, ext.sharedSecret, ext.destRealm)
	}
	answer := message.NewAnswer(message.NewRequest(265, types.AppIDNASREQ))
	if err := router.Dispatch(nil, answer); err != nil {
		t.Fatalf("registered NASREQ answer handler failed: %v", err)
	}
}

func TestInitHonorsConfiguration(t *testing.T) {
	router := routing.NewRouter()
	ext := &appRadGW{}
	cfg := map[string]interface{}{
		"radius_port":   float64(0),
		"shared_secret": "topsecret",
		"dest_realm":    "diameter.example.com",
	}
	if err := ext.Init(radgwInitContext{router: router}, cfg); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop() })

	if ext.sharedSecret != "topsecret" || ext.destRealm != "diameter.example.com" {
		t.Fatalf("configured values = secret %q realm %q", ext.sharedSecret, ext.destRealm)
	}
}

func TestHandleRadiusRequestRejectsPacketsItCannotRoute(t *testing.T) {
	ext := &appRadGW{destRealm: "diameter.example.com", router: routing.NewRouter()}
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18120}

	ext.handleRadiusRequest(addr, &radius.Packet{Code: radius.CodeStatusServer})
	if countSyncMap(&ext.pendingRequests) != 0 {
		t.Fatal("unsupported RADIUS packet created a pending Diameter request")
	}

	pkt := &radius.Packet{Code: radius.CodeAccessRequest, Identifier: 1}
	pkt.AddAttribute(radius.TypeUserName, []byte("alice"))
	ext.handleRadiusRequest(addr, pkt)
	if countSyncMap(&ext.pendingRequests) != 0 {
		t.Fatal("unroutable RADIUS packet was retained as pending")
	}
}

func TestHandleDiameterAnswerIgnoresRequestsAndUnknownAnswers(t *testing.T) {
	ext := &appRadGW{}
	if err := ext.handleDiameterAnswer(nil, message.NewRequest(265, types.AppIDNASREQ)); err != nil {
		t.Fatalf("request handling returned error: %v", err)
	}
	if err := ext.handleDiameterAnswer(nil, message.NewAnswer(message.NewRequest(265, types.AppIDNASREQ))); err != nil {
		t.Fatalf("unknown answer handling returned error: %v", err)
	}
}

func TestHandleDiameterAnswerTranslatesSuccessfulAAAnswer(t *testing.T) {
	ext, client := radgwWithUDPClient(t)
	reqAuth := [16]byte{1, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 1, 2, 3, 4}
	ext.pendingRequests.Store(types.HopByHopID(77), pendingRequest{
		addr:          client.LocalAddr().(*net.UDPAddr),
		id:            9,
		authenticator: reqAuth,
		expire:        time.Now().Add(time.Second),
	})
	ans := message.NewAnswer(message.NewRequest(265, types.AppIDNASREQ))
	ans.HopByHopID = 77
	ans.SetResultCode(types.ResultSuccess)
	ans.AddAVP(message.NewAVP(types.AVPCode(radius.TypeReplyMessage), types.AVPFlagMandatory, 0, []byte("welcome")))

	if err := ext.handleDiameterAnswer(nil, ans); err != nil {
		t.Fatalf("Diameter answer handling failed: %v", err)
	}
	resp := readRadius(t, client, "shared")
	if resp.Code != radius.CodeAccessAccept || resp.Identifier != 9 {
		t.Fatalf("RADIUS response = code %d id %d, want Access-Accept id 9", resp.Code, resp.Identifier)
	}
	if got, ok := resp.GetAttribute(radius.TypeReplyMessage); !ok || string(got) != "welcome" {
		t.Fatalf("Reply-Message = %q (present=%v), want welcome", got, ok)
	}
	if countSyncMap(&ext.pendingRequests) != 0 {
		t.Fatal("completed Diameter answer remained pending")
	}
}

func TestHandleDiameterAnswerDropsFailedAccountingAnswer(t *testing.T) {
	ext, client := radgwWithUDPClient(t)
	ext.pendingRequests.Store(types.HopByHopID(99), pendingRequest{addr: client.LocalAddr().(*net.UDPAddr), id: 4, expire: time.Now().Add(time.Second)})
	ans := message.NewAnswer(message.NewRequest(271, types.AppIDBaseAccounting))
	ans.HopByHopID = 99
	ans.SetResultCode(types.ResultUnableToComply)

	if err := ext.handleDiameterAnswer(nil, ans); err != nil {
		t.Fatalf("Diameter accounting answer handling failed: %v", err)
	}
	buf := make([]byte, 128)
	_ = client.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if n, _, err := client.ReadFromUDP(buf); err == nil {
		t.Fatalf("unexpected RADIUS response for failed accounting answer: %x", buf[:n])
	}
	if countSyncMap(&ext.pendingRequests) != 0 {
		t.Fatal("dropped accounting answer remained pending")
	}
}

func TestCleanupLoopRemovesExpiredPendingRequests(t *testing.T) {
	ext := &appRadGW{}
	ext.pendingRequests.Store(types.HopByHopID(1), pendingRequest{expire: time.Now().Add(-time.Second)})
	ext.pendingRequests.Store(types.HopByHopID(2), pendingRequest{expire: time.Now().Add(time.Minute)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		ext.cleanupLoop(ctx)
		close(done)
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, expiredPresent := ext.pendingRequests.Load(types.HopByHopID(1))
		_, futurePresent := ext.pendingRequests.Load(types.HopByHopID(2))
		if !expiredPresent && futurePresent {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expired pending request was not removed")
}

func radgwWithUDPClient(t *testing.T) (*appRadGW, *net.UDPConn) {
	t.Helper()
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("starting RADIUS gateway UDP socket: %v", err)
	}
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		_ = server.Close()
		t.Fatalf("starting RADIUS client UDP socket: %v", err)
	}
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	return &appRadGW{conn: server, sharedSecret: "shared"}, client
}

func readRadius(t *testing.T, conn *net.UDPConn, secret string) *radius.Packet {
	t.Helper()
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("reading RADIUS response: %v", err)
	}
	pkt, err := radius.Parse(buf[:n], secret)
	if err != nil {
		t.Fatalf("parsing RADIUS response: %v", err)
	}
	return pkt
}

func countSyncMap(m interface{ Range(func(any, any) bool) }) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func TestHandleDiameterAnswerTranslatesRejectAndAccountingSuccess(t *testing.T) {
	tests := []struct {
		name       string
		command    types.CommandCode
		result     types.ResultCode
		wantRadius uint8
	}{
		{name: "access reject", command: 265, result: types.ResultUnableToComply, wantRadius: radius.CodeAccessReject},
		{name: "accounting success", command: 271, result: types.ResultSuccess, wantRadius: radius.CodeAccountingResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, client := radgwWithUDPClient(t)
			ext.pendingRequests.Store(types.HopByHopID(123), pendingRequest{addr: client.LocalAddr().(*net.UDPAddr), id: 6, expire: time.Now().Add(time.Second)})
			ans := message.NewAnswer(message.NewRequest(tt.command, types.AppIDNASREQ))
			ans.HopByHopID = 123
			ans.SetResultCode(tt.result)

			if err := ext.handleDiameterAnswer(nil, ans); err != nil {
				t.Fatalf("Diameter answer handling failed: %v", err)
			}
			resp := readRadius(t, client, "shared")
			if resp.Code != tt.wantRadius || resp.Identifier != 6 {
				t.Fatalf("RADIUS response = code %d id %d, want code %d id 6", resp.Code, resp.Identifier, tt.wantRadius)
			}
		})
	}
}

func TestHandleRadiusRequestDropsWhenSelectedPeerIsUnavailable(t *testing.T) {
	router := routing.NewRouter()
	router.AddRealmRoute("diameter.example.com", "diameter-peer.example.com")
	ext := &appRadGW{destRealm: "diameter.example.com", router: router}
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18120}
	pkt := &radius.Packet{Code: radius.CodeAccessRequest, Identifier: 2}
	pkt.AddAttribute(radius.TypeUserName, []byte("alice"))

	ext.handleRadiusRequest(addr, pkt)
	if countSyncMap(&ext.pendingRequests) != 0 {
		t.Fatal("request without a live Diameter peer was retained as pending")
	}
}

func TestHandleRadiusRequestDropsInvalidPeerObject(t *testing.T) {
	router := routing.NewRouter()
	router.AddRealmRoute("diameter.example.com", "diameter-peer.example.com")
	router.SetPeerLookup(func(types.DiamID) (interface{}, bool) { return struct{}{}, true })
	ext := &appRadGW{destRealm: "diameter.example.com", router: router}
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18120}
	pkt := &radius.Packet{Code: radius.CodeAccountingRequest, Identifier: 3}
	pkt.AddAttribute(radius.TypeAcctSessionID, []byte("acct-1"))

	ext.handleRadiusRequest(addr, pkt)
	if countSyncMap(&ext.pendingRequests) != 0 {
		t.Fatal("request routed to a non-peer object was retained as pending")
	}
}
