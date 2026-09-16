// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package peer provides Diameter peer management and state machine.
// This corresponds to libfdcore/p_psm.c from the original freeDiameter.
package peer

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// State represents the state of a Diameter peer per RFC 6733 Section 5.6.
type State int

const (
	// StateClosed - Initial state, no transport connection (RFC 6733: Closed)
	StateClosed State = iota
	// StateWaitConnAck - Initiating transport connection (RFC 6733: Wait-Conn-Ack)
	StateWaitConnAck
	// StateWaitICEA - CER sent on initiator connection, waiting for CEA (RFC 6733: Wait-I-CEA)
	StateWaitICEA
	// StateWaitConnAckElect - Connection initiated, election pending (RFC 6733: Wait-Conn-Ack/Elect)
	StateWaitConnAckElect
	// StateWaitReturns - Waiting for election result (RFC 6733: Wait-Returns)
	StateWaitReturns
	// StateROpen - Responder connection is open (RFC 6733: R-Open)
	StateROpen
	// StateIOpen - Initiator connection is open (RFC 6733: I-Open)
	StateIOpen
	// StateClosing - Disconnect in progress, DPR sent or received (RFC 6733: Closing)
	StateClosing
	// StateSuspect - DWR sent, no answer yet (failure detection per RFC 3539)
	StateSuspect
	// StateZombie - PSM not running, peer needs restart or deletion
	StateZombie
)

// String returns the string representation of a state.
func (s State) String() string {
	names := []string{
		"Closed",
		"Wait-Conn-Ack",
		"Wait-I-CEA",
		"Wait-Conn-Ack/Elect",
		"Wait-Returns",
		"R-Open",
		"I-Open",
		"Closing",
		"Suspect",
		"Zombie",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("UNKNOWN(%d)", s)
}

// Event represents an event that can trigger state transitions per RFC 6733.
type Event int

const (
	// EventStart - Application signals connection should be initiated
	EventStart Event = iota
	// EventRConnCER - Incoming connection received with CER
	EventRConnCER
	// EventIRcvConnAck - Positive ack that initiator transport connection established
	EventIRcvConnAck
	// EventIRcvConnNack - Negative ack, initiator transport connection failed
	EventIRcvConnNack
	// EventTimeout - Application-defined timer expired
	EventTimeout
	// EventIRcvCER - CER received on initiator connection
	EventIRcvCER
	// EventIRcvCEA - CEA received on initiator connection
	EventIRcvCEA
	// EventRRcvCER - CER received on responder connection
	EventRRcvCER
	// EventRRcvCEA - CEA received on responder connection
	EventRRcvCEA
	// EventIRcvNonCEA - Non-CEA message received in Wait-I-CEA state
	EventIRcvNonCEA
	// EventIPeerDisc - Initiator connection disconnected by peer
	EventIPeerDisc
	// EventRPeerDisc - Responder connection disconnected by peer
	EventRPeerDisc
	// EventIRcvDWR - DWR received on initiator connection
	EventIRcvDWR
	// EventIRcvDWA - DWA received on initiator connection
	EventIRcvDWA
	// EventRRcvDWR - DWR received on responder connection
	EventRRcvDWR
	// EventRRcvDWA - DWA received on responder connection
	EventRRcvDWA
	// EventIRcvDPR - DPR received on initiator connection
	EventIRcvDPR
	// EventIRcvDPA - DPA received on initiator connection
	EventIRcvDPA
	// EventRRcvDPR - DPR received on responder connection
	EventRRcvDPR
	// EventRRcvDPA - DPA received on responder connection
	EventRRcvDPA
	// EventWinElection - Local node won the election
	EventWinElection
	// EventSendMessage - Application wants to send a message
	EventSendMessage
	// EventIRcvMessage - Application message received on initiator connection
	EventIRcvMessage
	// EventRRcvMessage - Application message received on responder connection
	EventRRcvMessage
	// EventStop - Application signals connection should be terminated
	EventStop
)

// String returns the string representation of an event.
func (e Event) String() string {
	names := []string{
		"Start",
		"R-Conn-CER",
		"I-Rcv-Conn-Ack",
		"I-Rcv-Conn-Nack",
		"Timeout",
		"I-Rcv-CER",
		"I-Rcv-CEA",
		"R-Rcv-CER",
		"R-Rcv-CEA",
		"I-Rcv-Non-CEA",
		"I-Peer-Disc",
		"R-Peer-Disc",
		"I-Rcv-DWR",
		"I-Rcv-DWA",
		"R-Rcv-DWR",
		"R-Rcv-DWA",
		"I-Rcv-DPR",
		"I-Rcv-DPA",
		"R-Rcv-DPR",
		"R-Rcv-DPA",
		"Win-Election",
		"Send-Message",
		"I-Rcv-Message",
		"R-Rcv-Message",
		"Stop",
	}
	if int(e) < len(names) {
		return names[e]
	}
	return fmt.Sprintf("UNKNOWN(%d)", e)
}

// Config holds the configuration for a peer.
type Config struct {
	// DiameterIdentity is the expected peer's Diameter Identity (FQDN)
	DiameterIdentity types.DiamID
	// Realm is the expected peer's realm
	Realm types.DiamID
	// Addresses are the peer's IP addresses/hostnames
	Addresses []string
	// Port is the Diameter port (default 3868)
	Port int
	// Network is the transport type (tcp, sctp)
	Network string
	// UseTLS enables TLS for the connection
	UseTLS bool
	// TLSConfig holds TLS configuration if UseTLS is true
	TLSConfig *TLSConfig
	// WatchdogInterval is the interval for DWR messages (default 30s)
	WatchdogInterval time.Duration
	// ConnectTimeout is the timeout for connection establishment
	ConnectTimeout time.Duration
	// ReconnectInterval is the time to wait before reconnecting
	ReconnectInterval time.Duration
	// Persistent keeps the peer entry even after disconnect
	Persistent bool
	// Zone is the edge zone this peer belongs to (empty when DEA is not configured)
	Zone string
	// Partner is the roaming partner this peer belongs to (empty if none)
	Partner string
}

// TLSConfig holds TLS configuration for a peer connection.
type TLSConfig struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	SkipVerify bool
	// MinVersion is the minimum TLS version ("1.2" or "1.3", default "1.2")
	MinVersion string
}

// Peer represents a Diameter peer per RFC 6733.
type Peer struct {
	mu sync.RWMutex

	// Configuration
	config Config
	// Local Configuration override (for testing or multi-node simulation)
	localConfig *LocalConfig

	// Identity received from peer
	diameterID types.DiamID
	realm      types.DiamID

	// Accept policy for inbound CER (server-side)
	acceptPolicy func(originHost, originRealm types.DiamID) (bool, types.ResultCode)

	// Current state per RFC 6733 Section 5.6
	state State

	// Dual connections per RFC 6733:
	// iConn - Initiator connection (we connected to peer)
	// rConn - Responder connection (peer connected to us)
	iConn net.Conn
	rConn net.Conn

	// Pending CER from R-Conn-CER event (used during election)
	pendingCER *message.Message

	// Dictionary for message parsing
	dict *dictionary.Dictionary

	// Identifiers
	hopByHopID uint32

	// Channels
	eventChan chan eventMessage
	sendChan  chan *message.Message
	recvChan  chan *message.Message

	// High-performance writer for batched, parallel writes
	writer *highPerfWriter

	// Callbacks
	onMessage     func(*Peer, *message.Message)
	onStateChange func(*Peer, State, State)

	// Context for permanent cancellation (Stop)
	ctx    context.Context
	cancel context.CancelFunc

	// Per-connection done signal (closed in cleanup, recreated on reconnect)
	connDone chan struct{}

	// Timers
	watchdogTimer *time.Timer
	timeoutTimer  *time.Timer

	// Statistics
	stats Stats

	// Pending requests keyed by Hop-by-Hop ID for answer correlation (RFC 6733 §6.2)
	pendingMu sync.Mutex
	pending   map[types.HopByHopID]pendingRequest
	nextE2E   atomic.Uint32

	// smWG tracks the runStateMachine goroutine so Stop() can block until
	// it has fully exited (and any pending state-change callbacks it fires
	// have returned) before returning to the caller.
	smWG sync.WaitGroup
}

type pendingRequest struct {
	EndToEndID types.EndToEndID
}

// Stats holds statistics for a peer.
type Stats struct {
	MessagesSent     uint64
	MessagesReceived uint64
	BytesSent        uint64
	BytesReceived    uint64
	LastActivity     time.Time
	ConnectedSince   time.Time
}

type eventMessage struct {
	event Event
	msg   *message.Message
	err   error
	conn  net.Conn // For R-Conn-CER events, the incoming connection
}

// New creates a new peer with the given configuration.
func New(config Config, dict *dictionary.Dictionary) *Peer {
	if config.Port == 0 {
		config.Port = 3868
	}
	if config.Network == "" {
		config.Network = "tcp"
	}
	if config.WatchdogInterval == 0 {
		config.WatchdogInterval = 30 * time.Second
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 10 * time.Second
	}
	if config.ReconnectInterval == 0 {
		config.ReconnectInterval = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Peer{
		config:    config,
		state:     StateClosed, // RFC 6733: Start in Closed state
		dict:      dict,
		eventChan: make(chan eventMessage, 256),      // Increased for high throughput
		sendChan:  make(chan *message.Message, 1024), // Fallback channel
		recvChan:  make(chan *message.Message, 1024), // Increased for high throughput
		ctx:       ctx,
		cancel:    cancel,
		connDone:  make(chan struct{}),
		pending:   make(map[types.HopByHopID]pendingRequest),
	}
	p.writer = newHighPerfWriter(p)
	return p
}

// Config returns the peer's configuration.
func (p *Peer) Config() Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// State returns the peer's current state.
func (p *Peer) State() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// DiameterID returns the peer's Diameter Identity.
func (p *Peer) DiameterID() types.DiamID {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.diameterID
}

// Zone returns the edge zone this peer belongs to.
func (p *Peer) Zone() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.Zone
}

// Partner returns the roaming partner this peer belongs to, if any.
func (p *Peer) Partner() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.Partner
}

// SetZone assigns the edge zone and partner of this peer. It is used by the
// server once the peer identity is known (after CER/CEA).
func (p *Peer) SetZone(zone, partner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.config.Zone = zone
	p.config.Partner = partner
}

// Realm returns the peer's realm.
func (p *Peer) Realm() types.DiamID {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.realm
}

// Stats returns the peer's statistics, including highPerfWriter counters.
func (p *Peer) Stats() Stats {
	p.mu.RLock()
	s := p.stats
	p.mu.RUnlock()
	if p.writer != nil {
		wSent, wBytes := p.writer.Stats()
		s.MessagesSent += wSent
		s.BytesSent += wBytes
	}
	return s
}

// IsOpen returns true if the peer is in an open state (I-Open or R-Open).
func (p *Peer) IsOpen() bool {
	state := p.State()
	return state == StateIOpen || state == StateROpen
}

// SetLocalOverride sets a local configuration override for this peer instance.
func (p *Peer) SetLocalOverride(cfg LocalConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.localConfig = &cfg
}

// SetAcceptPolicy sets the callback used to validate inbound CER (server-side).
// The callback returns (accepted, resultCode). If accepted is false,
// the CEA will be sent with the provided resultCode.
func (p *Peer) SetAcceptPolicy(fn func(originHost, originRealm types.DiamID) (bool, types.ResultCode)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acceptPolicy = fn
}

// LocalConfig returns the peer's local configuration.
func (p *Peer) LocalConfig() LocalConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.localConfig != nil {
		return *p.localConfig
	}
	return GetLocalConfig()
}

// SetOnMessage sets the callback for received messages.
func (p *Peer) SetOnMessage(cb func(*Peer, *message.Message)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onMessage = cb
}

// SetOnStateChange sets the callback for state changes.
func (p *Peer) SetOnStateChange(cb func(*Peer, State, State)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStateChange = cb
}

// NextHopByHopID returns the next hop-by-hop ID for this peer.
func (p *Peer) NextHopByHopID() types.HopByHopID {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hopByHopID++
	return types.HopByHopID(p.hopByHopID)
}

// nextEndToEndID returns a locally unique End-to-End ID.
func (p *Peer) nextEndToEndID() types.EndToEndID {
	return types.EndToEndID(p.nextE2E.Add(1))
}

// trackPending stores a pending request for answer correlation.
func (p *Peer) trackPending(msg *message.Message) {
	p.pendingMu.Lock()
	p.pending[msg.HopByHopID] = pendingRequest{EndToEndID: msg.EndToEndID}
	p.pendingMu.Unlock()
}

// popPending removes and returns a pending request for the given Hop-by-Hop ID.
func (p *Peer) popPending(hbh types.HopByHopID) (pendingRequest, bool) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	pr, ok := p.pending[hbh]
	if ok {
		delete(p.pending, hbh)
	}
	return pr, ok
}

// clearPending removes all pending requests and returns the count of cleared entries.
func (p *Peer) clearPending() int {
	p.pendingMu.Lock()
	n := len(p.pending)
	p.pending = make(map[types.HopByHopID]pendingRequest)
	p.pendingMu.Unlock()
	return n
}

// PendingCount returns the number of pending requests awaiting answers.
func (p *Peer) PendingCount() int {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return len(p.pending)
}

// TrackPending stores a pending request for answer correlation (exported for testing).
func (p *Peer) TrackPending(msg *message.Message) {
	p.trackPending(msg)
}

// setState changes the peer's state and invokes the callback.
func (p *Peer) setState(newState State) {
	p.mu.Lock()
	oldState := p.state
	p.state = newState
	callback := p.onStateChange
	p.mu.Unlock()

	if callback != nil && oldState != newState {
		callback(p, oldState, newState)
	}
}

// Send queues a message for sending to this peer.
func (p *Peer) Send(msg *message.Message) error {
	if !p.IsOpen() {
		return fmt.Errorf("peer not in open state: %s", p.State())
	}

	// Track outgoing requests for answer correlation.
	// This is needed for relay/proxy where the router sets the HopByHopID
	// before calling Send; the high-performance writer path does not call
	// sendMessageOnConn (which normally tracks pending), so we track here.
	// Requests with HopByHopID 0 are tracked later in sendMessageOnConn
	// where the ID is assigned.
	if msg.IsRequest() && msg.HopByHopID != 0 {
		p.trackPending(msg)
	}

	// Try lock-free high-performance writer first
	if p.writer != nil && p.writer.Send(msg) {
		return nil
	}

	// Fallback to channel-based sending
	select {
	case p.sendChan <- msg:
		return nil
	case <-p.ctx.Done():
		return fmt.Errorf("peer stopped")
	default:
		return fmt.Errorf("send queue full")
	}
}

// sendMessage sends a message on the active connection synchronously.
// This is used internally for sending protocol messages (CER, CEA, DWR, DWA, DPR, DPA).
func (p *Peer) sendMessage(msg *message.Message) error {
	return p.sendMessageOnActiveConn(msg)
}

// Receive returns a channel for receiving messages from this peer.
func (p *Peer) Receive() <-chan *message.Message {
	return p.recvChan
}

// Disconnect initiates a graceful disconnect from the peer.
func (p *Peer) Disconnect(_ types.DisconnectCause) {
	p.eventChan <- eventMessage{event: EventStop}
}

// Stop stops the peer state machine and blocks until the state machine
// goroutine (started by Start or StartWithConnection) has fully exited,
// including any in-flight OnStateChange callback it fires on the way out.
// This guarantees that once Stop returns, the peer will not touch any
// caller-provided callbacks or resources again.
//
// Do NOT call Stop from an OnStateChange callback: that callback runs on
// the state machine goroutine itself, so waiting for it would deadlock.
// Use `go peer.Stop()` from callbacks instead.
func (p *Peer) Stop() {
	p.cancel()
	p.smWG.Wait()
}
