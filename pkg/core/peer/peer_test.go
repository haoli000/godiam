// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package peer provides Diameter peer management and state machine.
// Tests for RFC 6733 Section 5.6 Peer State Machine implementation.
package peer

import (
	"bytes"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// mockConn is a mock net.Conn for testing.
type mockConn struct {
	readBuf    *bytes.Buffer
	writeBuf   *bytes.Buffer
	closed     bool
	mu         sync.Mutex
	readDelay  time.Duration
	localAddr  net.Addr
	remoteAddr net.Addr
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:    bytes.NewBuffer(nil),
		writeBuf:   bytes.NewBuffer(nil),
		localAddr:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3868},
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2"), Port: 3868},
	}
}

func (c *mockConn) Read(b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	if c.readDelay > 0 {
		time.Sleep(c.readDelay)
	}
	return c.readBuf.Read(b)
}

func (c *mockConn) Write(b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	return c.writeBuf.Write(b)
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *mockConn) LocalAddr() net.Addr  { return c.localAddr }
func (c *mockConn) RemoteAddr() net.Addr { return c.remoteAddr }

func (c *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *mockConn) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *mockConn) GetWrittenData() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeBuf.Bytes()
}

func (c *mockConn) WriteMessage(msg *message.Message) error {
	data, err := msg.Encode()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.readBuf.Write(data)
	return err
}

// testDict returns a minimal dictionary for testing.
func testDict() *dictionary.Dictionary {
	dict := dictionary.New()
	_ = dict.LoadBaseProtocol()
	return dict
}

// testConfig returns a peer config for testing.
func testConfig(identity string) Config {
	return Config{
		DiameterIdentity:  types.DiamID(identity),
		Realm:             "test.realm",
		Addresses:         []string{"127.0.0.1"},
		Port:              3868,
		Network:           "tcp",
		WatchdogInterval:  30 * time.Second,
		ConnectTimeout:    5 * time.Second,
		ReconnectInterval: 5 * time.Second,
		Persistent:        false,
	}
}

// createTestCER creates a CER message for testing.
func createTestCER(originHost, originRealm string) *message.Message {
	msg := message.NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	msg.HopByHopID = 1
	msg.EndToEndID = 1

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(originHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(originRealm)))
	msg.AddAVP(message.NewAddressAVP(
		types.AVPCodeHostIPAddress, types.AVPFlagMandatory, types.NewAddressFromIP(net.ParseIP("127.0.0.1"))))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeVendorID, types.AVPFlagMandatory, 0))
	msg.AddAVP(message.NewUTF8StringAVP(
		types.AVPCodeProductName, 0, "TestPeer"))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, 1))

	return msg
}

// createTestCEA creates a CEA message for testing.
func createTestCEA(req *message.Message, originHost, originRealm string, resultCode types.ResultCode) *message.Message {
	msg := message.NewAnswer(req)
	msg.SetResultCode(resultCode)

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(originHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(originRealm)))
	msg.AddAVP(message.NewAddressAVP(
		types.AVPCodeHostIPAddress, types.AVPFlagMandatory, types.NewAddressFromIP(net.ParseIP("127.0.0.1"))))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeVendorID, types.AVPFlagMandatory, 0))
	msg.AddAVP(message.NewUTF8StringAVP(
		types.AVPCodeProductName, 0, "TestPeer"))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, 1))

	return msg
}

// createTestDWR creates a DWR message for testing.
func createTestDWR(originHost string) *message.Message {
	msg := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	msg.HopByHopID = 2
	msg.EndToEndID = 2

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(originHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID("realm")))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, 1))

	return msg
}

// createTestDWA creates a DWA message for testing.
func createTestDWA(req *message.Message, originHost, originRealm string) *message.Message {
	msg := message.NewAnswer(req)
	msg.SetResultCode(types.ResultSuccess)

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(originHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(originRealm)))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, 1))

	return msg
}

// createTestDPR creates a DPR message for testing.
func createTestDPR(originHost, originRealm string, cause types.DisconnectCause) *message.Message {
	msg := message.NewRequest(types.CmdCodeDisconnectPeer, types.AppIDCommon)
	msg.HopByHopID = 3
	msg.EndToEndID = 3

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(originHost)))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(originRealm)))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeDisconnectCause, types.AVPFlagMandatory, uint32(cause)))

	return msg
}

// ============================================================================
// State Tests
// ============================================================================

func TestStateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "Closed"},
		{StateWaitConnAck, "Wait-Conn-Ack"},
		{StateWaitICEA, "Wait-I-CEA"},
		{StateWaitConnAckElect, "Wait-Conn-Ack/Elect"},
		{StateWaitReturns, "Wait-Returns"},
		{StateROpen, "R-Open"},
		{StateIOpen, "I-Open"},
		{StateClosing, "Closing"},
		{StateSuspect, "Suspect"},
		{StateZombie, "Zombie"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEventString(t *testing.T) {
	tests := []struct {
		event    Event
		expected string
	}{
		{EventStart, "Start"},
		{EventRConnCER, "R-Conn-CER"},
		{EventIRcvConnAck, "I-Rcv-Conn-Ack"},
		{EventIRcvConnNack, "I-Rcv-Conn-Nack"},
		{EventWinElection, "Win-Election"},
		{EventStop, "Stop"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.event.String(); got != tt.expected {
				t.Errorf("Event.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Peer Creation Tests
// ============================================================================

func TestNewPeer(t *testing.T) {
	dict := testDict()
	cfg := testConfig("test.peer.example.com")

	p := New(cfg, dict)
	if p == nil {
		t.Fatal("New() returned nil")
	}

	if p.State() != StateClosed {
		t.Errorf("Initial state = %v, want %v", p.State(), StateClosed)
	}

	if p.DiameterID() != "" {
		t.Errorf("Initial DiameterID = %v, want empty", p.DiameterID())
	}

	if p.IsOpen() {
		t.Error("IsOpen() = true, want false for new peer")
	}
}

func TestPeerIsOpen(t *testing.T) {
	dict := testDict()
	cfg := testConfig("test.peer.example.com")

	p := New(cfg, dict)

	// Test various states
	tests := []struct {
		state    State
		expected bool
	}{
		{StateClosed, false},
		{StateWaitConnAck, false},
		{StateWaitICEA, false},
		{StateWaitConnAckElect, false},
		{StateWaitReturns, false},
		{StateROpen, true},
		{StateIOpen, true},
		{StateClosing, false},
		{StateSuspect, false},
		{StateZombie, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			p.setState(tt.state)
			if got := p.IsOpen(); got != tt.expected {
				t.Errorf("IsOpen() with state %v = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestAnswerDeliveredWhenPendingMatches(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)
	p.setState(StateROpen)
	p.rConn = newMockConn()

	var delivered int
	p.SetOnMessage(func(_ *Peer, msg *message.Message) {
		if !msg.IsRequest() {
			delivered++
		}
	})

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))

	if err := p.sendMessageOnConn(p.rConn, req); err != nil {
		t.Fatalf("sendMessageOnConn failed: %v", err)
	}

	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)

	if err := p.handleStateROpen(eventMessage{event: EventRRcvMessage, msg: ans}); err != nil {
		t.Fatalf("handleStateROpen returned error: %v", err)
	}

	if delivered != 1 {
		t.Fatalf("expected 1 delivered answer, got %d", delivered)
	}
}

func TestAnswerDroppedWhenNoPending(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)
	p.setState(StateROpen)

	var delivered int
	p.SetOnMessage(func(_ *Peer, msg *message.Message) {
		if !msg.IsRequest() {
			delivered++
		}
	})

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.HopByHopID = 111
	req.EndToEndID = 222

	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)

	if err := p.handleStateROpen(eventMessage{event: EventRRcvMessage, msg: ans}); err != nil {
		t.Fatalf("handleStateROpen returned error: %v", err)
	}

	if delivered != 0 {
		t.Fatalf("expected 0 delivered answers, got %d", delivered)
	}
}

func TestDuplicateAnswerDropped(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)
	p.setState(StateROpen)
	p.rConn = newMockConn()

	var delivered int
	p.SetOnMessage(func(_ *Peer, msg *message.Message) {
		if !msg.IsRequest() {
			delivered++
		}
	})

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))

	if err := p.sendMessageOnConn(p.rConn, req); err != nil {
		t.Fatalf("sendMessageOnConn failed: %v", err)
	}

	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)

	if err := p.handleStateROpen(eventMessage{event: EventRRcvMessage, msg: ans}); err != nil {
		t.Fatalf("first answer error: %v", err)
	}
	if err := p.handleStateROpen(eventMessage{event: EventRRcvMessage, msg: ans}); err != nil {
		t.Fatalf("second answer error: %v", err)
	}

	if delivered != 1 {
		t.Fatalf("expected 1 delivered answer after duplicate, got %d", delivered)
	}
}

func TestAnswerDroppedOnEndToEndMismatch(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)
	p.setState(StateROpen)
	p.rConn = newMockConn()

	var delivered int
	p.SetOnMessage(func(_ *Peer, msg *message.Message) {
		if !msg.IsRequest() {
			delivered++
		}
	})

	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))

	if err := p.sendMessageOnConn(p.rConn, req); err != nil {
		t.Fatalf("sendMessageOnConn failed: %v", err)
	}

	ans := message.NewAnswer(req)
	ans.SetResultCode(types.ResultSuccess)
	ans.EndToEndID = req.EndToEndID + 1 // force mismatch

	if err := p.handleStateROpen(eventMessage{event: EventRRcvMessage, msg: ans}); err != nil {
		t.Fatalf("handleStateROpen returned error: %v", err)
	}

	if delivered != 0 {
		t.Fatalf("expected 0 delivered answers due to end-to-end mismatch, got %d", delivered)
	}
}

// ============================================================================
// State Machine Tests - Closed State
// ============================================================================

func TestHandleStateClosed_RConnCER(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Save and restore global state
	savedConfig := GetLocalConfig()
	defer SetLocalConfig(savedConfig)

	SetLocalConfig(LocalConfig{
		DiameterIdentity: "local.peer.example.com",
		Realm:            "test.realm",
		VendorID:         0,
		ProductName:      "TestProduct",
		OriginStateID:    1,
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	})

	// Create a mock connection and CER
	conn := newMockConn()
	cer := createTestCER("remote.peer.example.com", "test.realm")

	// Directly test the state handler instead of using StartWithConnection
	// which involves read loops that fail with mock connections
	err := p.acceptResponderConnection(conn)
	if err != nil {
		t.Fatalf("acceptResponderConnection() error = %v", err)
	}

	// Process the CER manually
	err = p.processCER(cer)
	if err != nil {
		t.Fatalf("processCER() error = %v", err)
	}

	// Check peer identity was set
	if p.DiameterID() != "remote.peer.example.com" {
		t.Errorf("DiameterID = %v, want remote.peer.example.com", p.DiameterID())
	}

	// Check realm was set
	if p.Realm() != "test.realm" {
		t.Errorf("Realm = %v, want test.realm", p.Realm())
	}
}

func TestHandleStateClosed_EventRConnCER(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Save and restore global state
	savedConfig := GetLocalConfig()
	defer SetLocalConfig(savedConfig)

	SetLocalConfig(LocalConfig{
		DiameterIdentity: "local.peer.example.com",
		Realm:            "test.realm",
		VendorID:         0,
		ProductName:      "TestProduct",
		OriginStateID:    1,
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	})

	// Verify initial state is Closed
	if p.State() != StateClosed {
		t.Fatalf("Initial state = %v, want %v", p.State(), StateClosed)
	}

	// Create event
	conn := newMockConn()
	cer := createTestCER("remote.peer.example.com", "test.realm")
	ev := eventMessage{
		event: EventRConnCER,
		conn:  conn,
		msg:   cer,
	}

	// Handle the event directly
	err := p.handleStateClosed(ev)
	if err != nil {
		t.Fatalf("handleStateClosed() error = %v", err)
	}

	// State should transition to R-Open
	if p.State() != StateROpen {
		t.Errorf("State = %v, want %v", p.State(), StateROpen)
	}

	// Clean up
	p.Stop()
}

// ============================================================================
// Election Process Tests (RFC 6733 Section 5.6.4)
// ============================================================================

func TestElectionLocalWins(t *testing.T) {
	// Test case: local Origin-Host > remote Origin-Host (lexicographically)
	// Local should win and keep R-connection

	dict := testDict()
	// "z-local" > "a-remote" lexicographically, so local wins
	cfg := testConfig("z-local.example.com")

	p := New(cfg, dict)
	p.diameterID = "a-remote.example.com" // Simulate received from CER

	savedConfig := GetLocalConfig()
	defer SetLocalConfig(savedConfig)

	SetLocalConfig(LocalConfig{
		DiameterIdentity: "z-local.example.com",
		Realm:            "test.realm",
		VendorID:         0,
		ProductName:      "TestProduct",
		OriginStateID:    1,
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	})

	// Manually trigger state machine and check election logic
	// This is a unit test of the election comparison logic

	localHost := string(p.getEffectiveLocalConfig().DiameterIdentity)
	peerHost := string(p.diameterID)

	if localHost <= peerHost {
		t.Errorf("Expected local (%s) > peer (%s)", localHost, peerHost)
	}
}

func TestElectionRemoteWins(t *testing.T) {
	// Test case: local Origin-Host < remote Origin-Host (lexicographically)
	// Remote should win, local keeps I-connection

	dict := testDict()
	// "a-local" < "z-remote" lexicographically, so remote wins
	cfg := testConfig("a-local.example.com")

	p := New(cfg, dict)
	p.diameterID = "z-remote.example.com" // Simulate received from CER

	savedConfig := GetLocalConfig()
	defer SetLocalConfig(savedConfig)

	SetLocalConfig(LocalConfig{
		DiameterIdentity: "a-local.example.com",
		Realm:            "test.realm",
		VendorID:         0,
		ProductName:      "TestProduct",
		OriginStateID:    1,
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	})

	localHost := string(p.getEffectiveLocalConfig().DiameterIdentity)
	peerHost := string(p.diameterID)

	if localHost >= peerHost {
		t.Errorf("Expected local (%s) < peer (%s)", localHost, peerHost)
	}
}

func TestElectionCaseInsensitive(t *testing.T) {
	// RFC 6733: ASCII comparison should be case-insensitive

	dict := testDict()
	cfg := testConfig("AAA.example.com")

	p := New(cfg, dict)
	p.diameterID = "aaa.example.com"

	savedConfig := GetLocalConfig()
	defer SetLocalConfig(savedConfig)

	SetLocalConfig(LocalConfig{
		DiameterIdentity: "AAA.example.com",
		Realm:            "test.realm",
		VendorID:         0,
		ProductName:      "TestProduct",
		OriginStateID:    1,
		HostIPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	})

	// Both should be equal when compared case-insensitively
	localHost := string(p.getEffectiveLocalConfig().DiameterIdentity)
	peerHost := string(p.diameterID)

	// The comparison in performElection uses strings.ToLower
	if !strings.EqualFold(localHost, peerHost) {
		t.Errorf("Expected equal after case normalization: %s vs %s", localHost, peerHost)
	}
}

// ============================================================================
// Dual Connection Tests
// ============================================================================

func TestDualConnectionTracking(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Initially no connections
	if p.iConn != nil {
		t.Error("Initial iConn should be nil")
	}
	if p.rConn != nil {
		t.Error("Initial rConn should be nil")
	}

	// Simulate accepting responder connection
	rConn := newMockConn()
	err := p.acceptResponderConnection(rConn)
	if err != nil {
		t.Fatalf("acceptResponderConnection() error = %v", err)
	}

	if p.rConn == nil {
		t.Error("rConn should be set after acceptResponderConnection")
	}

	// Try to accept another R connection - should fail
	rConn2 := newMockConn()
	err = p.acceptResponderConnection(rConn2)
	if err == nil {
		t.Error("Expected error when accepting second R connection")
	}

	// Close R connection
	p.closeRConn()
	if p.rConn != nil {
		t.Error("rConn should be nil after closeRConn")
	}
}

func TestCloseIConn(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Set up I connection
	iConn := newMockConn()
	p.mu.Lock()
	p.iConn = iConn
	p.mu.Unlock()

	// Close I connection
	p.closeIConn()

	if p.iConn != nil {
		t.Error("iConn should be nil after closeIConn")
	}

	if !iConn.IsClosed() {
		t.Error("Connection should be closed")
	}
}

func TestGetActiveConn(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// No connections - should return nil
	if conn := p.getActiveConn(); conn != nil {
		t.Error("getActiveConn() should return nil when no connections")
	}

	// Add I connection
	iConn := newMockConn()
	p.mu.Lock()
	p.iConn = iConn
	p.mu.Unlock()

	// In I-Open state, should return I connection
	p.setState(StateIOpen)
	if conn := p.getActiveConn(); conn != iConn {
		t.Error("getActiveConn() should return iConn in I-Open state")
	}

	// Add R connection
	rConn := newMockConn()
	p.mu.Lock()
	p.rConn = rConn
	p.mu.Unlock()

	// In R-Open state, should return R connection
	p.setState(StateROpen)
	if conn := p.getActiveConn(); conn != rConn {
		t.Error("getActiveConn() should return rConn in R-Open state")
	}
}

// ============================================================================
// Watchdog Tests
// ============================================================================

func TestWatchdogStart(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	cfg.WatchdogInterval = 100 * time.Millisecond

	p := New(cfg, dict)

	p.startWatchdog()

	if p.watchdogTimer == nil {
		t.Error("watchdogTimer should be set after startWatchdog")
	}

	// Cleanup
	p.stopWatchdog()
}

func TestWatchdogReset(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	cfg.WatchdogInterval = 100 * time.Millisecond

	p := New(cfg, dict)

	p.startWatchdog()
	p.resetWatchdog()

	if p.watchdogTimer == nil {
		t.Error("watchdogTimer should still be set after resetWatchdog")
	}

	// Cleanup
	p.stopWatchdog()
}

func TestWatchdogStop(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	cfg.WatchdogInterval = 100 * time.Millisecond

	p := New(cfg, dict)

	p.startWatchdog()
	p.stopWatchdog()

	if p.watchdogTimer != nil {
		t.Error("watchdogTimer should be nil after stopWatchdog")
	}
}

// ============================================================================
// Timeout Tests
// ============================================================================

func TestTimeoutStart(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	cfg.ConnectTimeout = 100 * time.Millisecond

	p := New(cfg, dict)

	p.startTimeout()

	p.mu.RLock()
	timer := p.timeoutTimer
	p.mu.RUnlock()

	if timer == nil {
		t.Error("timeoutTimer should be set after startTimeout")
	}

	// Cleanup
	p.stopTimeout()
}

func TestTimeoutStop(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	cfg.ConnectTimeout = 100 * time.Millisecond

	p := New(cfg, dict)

	p.startTimeout()
	p.stopTimeout()

	p.mu.RLock()
	timer := p.timeoutTimer
	p.mu.RUnlock()

	if timer != nil {
		t.Error("timeoutTimer should be nil after stopTimeout")
	}
}

// ============================================================================
// Message Classification Tests
// ============================================================================

func TestClassifyMessageI(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	tests := []struct {
		name     string
		msg      *message.Message
		expected Event
	}{
		{
			name:     "CER",
			msg:      createTestCER("peer", "realm"),
			expected: EventIRcvCER,
		},
		{
			name:     "CEA",
			msg:      createTestCEA(createTestCER("peer", "realm"), "peer", "realm", types.ResultSuccess),
			expected: EventIRcvCEA,
		},
		{
			name:     "DWR",
			msg:      createTestDWR("peer"),
			expected: EventIRcvDWR,
		},
		{
			name:     "DWA",
			msg:      createTestDWA(createTestDWR("peer"), "peer", "realm"),
			expected: EventIRcvDWA,
		},
		{
			name:     "DPR",
			msg:      createTestDPR("peer", "realm", types.DisconnectRebooting),
			expected: EventIRcvDPR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.classifyMessageI(tt.msg)
			if got != tt.expected {
				t.Errorf("classifyMessageI() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClassifyMessageR(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	tests := []struct {
		name     string
		msg      *message.Message
		expected Event
	}{
		{
			name:     "CER",
			msg:      createTestCER("peer", "realm"),
			expected: EventRRcvCER,
		},
		{
			name:     "CEA",
			msg:      createTestCEA(createTestCER("peer", "realm"), "peer", "realm", types.ResultSuccess),
			expected: EventRRcvCEA,
		},
		{
			name:     "DWR",
			msg:      createTestDWR("peer"),
			expected: EventRRcvDWR,
		},
		{
			name:     "DWA",
			msg:      createTestDWA(createTestDWR("peer"), "peer", "realm"),
			expected: EventRRcvDWA,
		},
		{
			name:     "DPR",
			msg:      createTestDPR("peer", "realm", types.DisconnectRebooting),
			expected: EventRRcvDPR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.classifyMessageR(tt.msg)
			if got != tt.expected {
				t.Errorf("classifyMessageR() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Cleanup Tests
// ============================================================================

func TestErrorCleanup(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	cfg.WatchdogInterval = 100 * time.Millisecond
	cfg.ConnectTimeout = 100 * time.Millisecond

	p := New(cfg, dict)

	// Set up connections
	iConn := newMockConn()
	rConn := newMockConn()
	p.mu.Lock()
	p.iConn = iConn
	p.rConn = rConn
	p.pendingCER = createTestCER("peer", "realm")
	p.mu.Unlock()

	p.startWatchdog()
	p.startTimeout()

	// Call error cleanup
	p.errorCleanup()

	// Verify all resources cleaned up
	if p.iConn != nil {
		t.Error("iConn should be nil after errorCleanup")
	}
	if p.rConn != nil {
		t.Error("rConn should be nil after errorCleanup")
	}
	if p.pendingCER != nil {
		t.Error("pendingCER should be nil after errorCleanup")
	}
	if p.watchdogTimer != nil {
		t.Error("watchdogTimer should be nil after errorCleanup")
	}

	p.mu.RLock()
	timer := p.timeoutTimer
	p.mu.RUnlock()
	if timer != nil {
		t.Error("timeoutTimer should be nil after errorCleanup")
	}
}

func TestErrorCleanupClearsPending(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	p := New(cfg, dict)

	// Simulate pending requests
	for i := 1; i <= 5; i++ {
		msg := message.NewRequest(265, 4)
		msg.HopByHopID = types.HopByHopID(i)
		msg.EndToEndID = types.EndToEndID(i * 100)
		p.trackPending(msg)
	}
	if p.PendingCount() != 5 {
		t.Fatalf("expected 5 pending, got %d", p.PendingCount())
	}

	p.errorCleanup()

	if p.PendingCount() != 0 {
		t.Errorf("expected 0 pending after errorCleanup, got %d", p.PendingCount())
	}
}

func TestClearPendingReturnsCount(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")
	p := New(cfg, dict)

	for i := 1; i <= 3; i++ {
		msg := message.NewRequest(265, 4)
		msg.HopByHopID = types.HopByHopID(i)
		msg.EndToEndID = types.EndToEndID(i * 100)
		p.trackPending(msg)
	}

	n := p.clearPending()
	if n != 3 {
		t.Errorf("clearPending returned %d, want 3", n)
	}
	if p.PendingCount() != 0 {
		t.Errorf("expected 0 pending after clearPending, got %d", p.PendingCount())
	}
}

// ============================================================================
// Callbacks Tests
// ============================================================================

func TestOnStateChangeCallback(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	var oldState, newState State
	callCount := 0

	p.SetOnStateChange(func(_ *Peer, old, newSt State) {
		oldState = old
		newState = newSt
		callCount++
	})

	// Change state
	p.setState(StateWaitConnAck)

	if callCount != 1 {
		t.Errorf("Callback called %d times, want 1", callCount)
	}
	if oldState != StateClosed {
		t.Errorf("oldState = %v, want %v", oldState, StateClosed)
	}
	if newState != StateWaitConnAck {
		t.Errorf("newState = %v, want %v", newState, StateWaitConnAck)
	}
}

func TestOnStateChangeNotCalledForSameState(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	callCount := 0
	p.SetOnStateChange(func(_ *Peer, _, _ State) {
		callCount++
	})

	// Set same state
	p.setState(StateClosed)

	if callCount != 0 {
		t.Errorf("Callback called %d times, want 0 for same state", callCount)
	}
}

func TestOnMessageCallback(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	var receivedMsg *message.Message
	callCount := 0

	p.SetOnMessage(func(_ *Peer, msg *message.Message) {
		receivedMsg = msg
		callCount++
	})

	// Deliver a message
	testMsg := createTestDWR("peer")
	p.deliverMessage(testMsg)

	if callCount != 1 {
		t.Errorf("Callback called %d times, want 1", callCount)
	}
	if receivedMsg != testMsg {
		t.Error("Received message doesn't match sent message")
	}
}

// ============================================================================
// Send Tests
// ============================================================================

func TestSendWhenNotOpen(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	msg := createTestDWR("local")
	err := p.Send(msg)

	if err == nil {
		t.Error("Send() should return error when peer not open")
	}
}

func TestSendWhenOpen(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)
	p.setState(StateIOpen)

	msg := createTestDWR("local")

	// This should not block because the channel has buffer
	// Note: In real scenario, writeLoop would process the message
	err := p.Send(msg)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
}

// ============================================================================
// Statistics Tests
// ============================================================================

func TestStatisticsUpdate(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Initial stats should be zero
	stats := p.Stats()
	if stats.MessagesSent != 0 {
		t.Errorf("Initial MessagesSent = %v, want 0", stats.MessagesSent)
	}
	if stats.MessagesReceived != 0 {
		t.Errorf("Initial MessagesReceived = %v, want 0", stats.MessagesReceived)
	}

	// Simulate updating stats
	p.mu.Lock()
	p.stats.MessagesSent = 10
	p.stats.MessagesReceived = 20
	p.stats.BytesSent = 1000
	p.stats.BytesReceived = 2000
	p.mu.Unlock()

	stats = p.Stats()
	if stats.MessagesSent != 10 {
		t.Errorf("MessagesSent = %v, want 10", stats.MessagesSent)
	}
	if stats.MessagesReceived != 20 {
		t.Errorf("MessagesReceived = %v, want 20", stats.MessagesReceived)
	}
	if stats.BytesSent != 1000 {
		t.Errorf("BytesSent = %v, want 1000", stats.BytesSent)
	}
	if stats.BytesReceived != 2000 {
		t.Errorf("BytesReceived = %v, want 2000", stats.BytesReceived)
	}
}

// ============================================================================
// Accept Policy Tests
// ============================================================================

func TestAcceptPolicy(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Set accept policy that only accepts specific host
	p.SetAcceptPolicy(func(originHost, _ types.DiamID) (bool, types.ResultCode) {
		if string(originHost) == "allowed.peer.example.com" {
			return true, types.ResultSuccess
		}
		return false, types.ResultUnknownPeer
	})

	// Test allowed peer
	allowed, code := p.acceptPolicy("allowed.peer.example.com", "realm")
	if !allowed || code != types.ResultSuccess {
		t.Error("Should accept allowed.peer.example.com")
	}

	// Test denied peer
	allowed, code = p.acceptPolicy("denied.peer.example.com", "realm")
	if allowed || code != types.ResultUnknownPeer {
		t.Error("Should reject denied.peer.example.com")
	}
}

// ============================================================================
// Integration-like Tests
// ============================================================================

func TestStateTransitionSequence(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Track state transitions
	var transitions []State
	p.SetOnStateChange(func(_ *Peer, _, newSt State) {
		transitions = append(transitions, newSt)
	})

	// Simulate state transitions for successful initiator connection
	// Closed -> Wait-Conn-Ack -> Wait-I-CEA -> I-Open
	p.setState(StateWaitConnAck)
	p.setState(StateWaitICEA)
	p.setState(StateIOpen)

	expected := []State{StateWaitConnAck, StateWaitICEA, StateIOpen}
	if len(transitions) != len(expected) {
		t.Fatalf("Got %d transitions, want %d", len(transitions), len(expected))
	}

	for i, state := range expected {
		if transitions[i] != state {
			t.Errorf("Transition %d = %v, want %v", i, transitions[i], state)
		}
	}
}

func TestStateTransitionSequenceResponder(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Track state transitions
	var transitions []State
	p.SetOnStateChange(func(_ *Peer, _, newSt State) {
		transitions = append(transitions, newSt)
	})

	// Simulate state transitions for successful responder connection
	// Closed -> R-Open (direct from R-Conn-CER)
	p.setState(StateROpen)

	expected := []State{StateROpen}
	if len(transitions) != len(expected) {
		t.Fatalf("Got %d transitions, want %d", len(transitions), len(expected))
	}

	for i, state := range expected {
		if transitions[i] != state {
			t.Errorf("Transition %d = %v, want %v", i, transitions[i], state)
		}
	}
}

// ============================================================================
// Event Message Struct Tests
// ============================================================================

func TestEventMessage(t *testing.T) {
	msg := createTestCER("peer", "realm")
	conn := newMockConn()

	ev := eventMessage{
		event: EventRConnCER,
		msg:   msg,
		conn:  conn,
		err:   nil,
	}

	if ev.event != EventRConnCER {
		t.Errorf("event = %v, want %v", ev.event, EventRConnCER)
	}
	if ev.msg != msg {
		t.Error("msg mismatch")
	}
	if ev.conn != conn {
		t.Error("conn mismatch")
	}
}

// ============================================================================
// State Handler Tests
// ============================================================================

func TestHandleEventUnhandledState(t *testing.T) {
	dict := testDict()
	cfg := testConfig("local.peer.example.com")

	p := New(cfg, dict)

	// Set an invalid state (using a value beyond defined states)
	p.mu.Lock()
	p.state = State(100) // Invalid state
	p.mu.Unlock()

	// Should return error for unhandled state
	err := p.handleEvent(eventMessage{event: EventStart})
	if err == nil {
		t.Error("Expected error for unhandled state")
	}
}
