// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package peer provides Diameter peer management and state machine.
// This implements the Peer State Machine per RFC 6733 Section 5.6.
package peer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/haoli000/godiam/pkg/core/transport"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// Start initiates the peer state machine.
// This sends a Start event which triggers connection initiation from Closed state.
func (p *Peer) Start() error {
	p.smWG.Add(1)
	go p.runStateMachine()
	p.eventChan <- eventMessage{event: EventStart}
	return nil
}

// StartWithConnection starts the peer with an existing responder connection.
// This is used for server-side peers when a connection is accepted with a CER.
// Per RFC 6733 Section 5.6.1, we receive the connection and CER together as R-Conn-CER event.
func (p *Peer) StartWithConnection(conn net.Conn, cer *message.Message) error {
	p.smWG.Add(1)
	go p.runStateMachine()
	p.eventChan <- eventMessage{event: EventRConnCER, conn: conn, msg: cer}
	return nil
}

// InjectRConnCER injects an incoming connection with CER into the state machine.
// Used when a peer receives an incoming connection while already running.
func (p *Peer) InjectRConnCER(conn net.Conn, cer *message.Message) {
	p.eventChan <- eventMessage{event: EventRConnCER, conn: conn, msg: cer}
}

// runStateMachine is the main goroutine for the peer state machine.
func (p *Peer) runStateMachine() {
	defer p.smWG.Done()
	defer p.cleanup()

	for {
		select {
		case <-p.ctx.Done():
			p.setState(StateZombie)
			return

		case ev := <-p.eventChan:
			if err := p.handleEvent(ev); err != nil {
				// Log error - in production this should use proper logging
				fmt.Printf("peer %s: state machine error: %v\n", p.config.DiameterIdentity, err)
			}

			// Check if we should exit the state machine
			if p.state == StateZombie {
				return
			}
		}
	}
}

// handleEvent processes an event and transitions state according to RFC 6733 Section 5.6.
func (p *Peer) handleEvent(ev eventMessage) error {
	p.mu.RLock()
	currentState := p.state
	p.mu.RUnlock()

	switch currentState {
	case StateClosed:
		return p.handleStateClosed(ev)
	case StateWaitConnAck:
		return p.handleStateWaitConnAck(ev)
	case StateWaitICEA:
		return p.handleStateWaitICEA(ev)
	case StateWaitConnAckElect:
		return p.handleStateWaitConnAckElect(ev)
	case StateWaitReturns:
		return p.handleStateWaitReturns(ev)
	case StateROpen:
		return p.handleStateROpen(ev)
	case StateIOpen:
		return p.handleStateIOpen(ev)
	case StateClosing:
		return p.handleStateClosing(ev)
	case StateSuspect:
		return p.handleStateSuspect(ev)
	case StateZombie:
		return nil
	default:
		return fmt.Errorf("unhandled state: %s", currentState)
	}
}

// handleStateClosed handles events in the Closed state.
// RFC 6733 Section 5.6:
//
//	Closed + Start       -> I-Snd-Conn-Req  -> Wait-Conn-Ack
//	Closed + R-Conn-CER  -> R-Accept, Process-CER, R-Snd-CEA -> R-Open
func (p *Peer) handleStateClosed(ev eventMessage) error {
	switch ev.event {
	case EventStart:
		// I-Snd-Conn-Req: Initiate connection
		p.resetConnDone()
		p.setState(StateWaitConnAck)
		p.startTimeout()
		go p.initiateConnection()
		return nil

	case EventRConnCER:
		// R-Accept, Process-CER, R-Snd-CEA
		p.resetConnDone()
		if err := p.acceptResponderConnection(ev.conn); err != nil {
			p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
			return err
		}
		if err := p.processCER(ev.msg); err != nil {
			var rej cerRejectError
			if errors.As(err, &rej) {
				p.rejectConnection(ev.conn, ev.msg, rej.code)
			} else {
				p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
			}
			return err
		}
		if err := p.sendCEAOnConn(p.rConn, ev.msg, types.ResultSuccess); err != nil {
			p.closeRConn()
			return err
		}
		p.setState(StateROpen)
		p.startWatchdog()
		go p.readLoopR()
		go p.writeLoop()
		return nil

	default:
		return nil
	}
}

// handleStateWaitConnAck handles events in the Wait-Conn-Ack state.
// RFC 6733 Section 5.6:
//
//	Wait-Conn-Ack + I-Rcv-Conn-Ack  -> I-Snd-CER        -> Wait-I-CEA
//	Wait-Conn-Ack + I-Rcv-Conn-Nack -> Cleanup          -> Closed
//	Wait-Conn-Ack + R-Conn-CER      -> R-Accept, Process-CER -> Wait-Conn-Ack/Elect
//	Wait-Conn-Ack + Timeout         -> Error            -> Closed
func (p *Peer) handleStateWaitConnAck(ev eventMessage) error {
	switch ev.event {
	case EventIRcvConnAck:
		// I-Snd-CER
		p.stopTimeout()
		if err := p.sendCER(); err != nil {
			p.errorCleanup()
			p.setState(StateClosed)
			return err
		}
		p.setState(StateWaitICEA)
		p.startTimeout()
		go p.readLoopI()
		return nil

	case EventIRcvConnNack:
		// Cleanup
		p.stopTimeout()
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return ev.err

	case EventRConnCER:
		// R-Accept, Process-CER -> Wait-Conn-Ack/Elect
		if err := p.acceptResponderConnection(ev.conn); err != nil {
			p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
			return nil // Don't change state
		}
		if err := p.processCER(ev.msg); err != nil {
			var rej cerRejectError
			if errors.As(err, &rej) {
				p.rejectConnection(ev.conn, ev.msg, rej.code)
			} else {
				p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
			}
			p.closeRConn()
			return nil
		}
		p.mu.Lock()
		p.pendingCER = ev.msg
		p.mu.Unlock()
		p.setState(StateWaitConnAckElect)
		return nil

	case EventTimeout:
		// Error
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	default:
		return nil
	}
}

// handleStateWaitICEA handles events in the Wait-I-CEA state.
// RFC 6733 Section 5.6:
//
//	Wait-I-CEA + I-Rcv-CEA     -> Process-CEA      -> I-Open
//	Wait-I-CEA + R-Conn-CER    -> R-Accept, Process-CER, Elect -> Wait-Returns
//	Wait-I-CEA + I-Peer-Disc   -> I-Disc           -> Closed
//	Wait-I-CEA + I-Rcv-Non-CEA -> Error            -> Closed
//	Wait-I-CEA + Timeout       -> Error            -> Closed
func (p *Peer) handleStateWaitICEA(ev eventMessage) error {
	switch ev.event {
	case EventIRcvCEA:
		// Process-CEA
		p.stopTimeout()
		if err := p.processCEA(ev.msg); err != nil {
			p.errorCleanup()
			p.setState(StateClosed)
			return err
		}
		p.setState(StateIOpen)
		p.startWatchdog()
		go p.writeLoop()
		return nil

	case EventRConnCER:
		// R-Accept, Process-CER, Elect -> Wait-Returns
		if err := p.acceptResponderConnection(ev.conn); err != nil {
			p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
			return nil
		}
		if err := p.processCER(ev.msg); err != nil {
			var rej cerRejectError
			if errors.As(err, &rej) {
				p.rejectConnection(ev.conn, ev.msg, rej.code)
			} else {
				p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
			}
			p.closeRConn()
			return nil
		}
		p.mu.Lock()
		p.pendingCER = ev.msg
		p.mu.Unlock()
		p.setState(StateWaitReturns)
		// Perform election
		p.performElection()
		return nil

	case EventIPeerDisc:
		// I-Disc
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	case EventIRcvNonCEA:
		// Error: received something other than CEA
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return fmt.Errorf("received non-CEA message in Wait-I-CEA state")

	case EventTimeout:
		// Error
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	default:
		return nil
	}
}

// handleStateWaitConnAckElect handles events in the Wait-Conn-Ack/Elect state.
// RFC 6733 Section 5.6:
//
//	Wait-Conn-Ack/Elect + I-Rcv-Conn-Ack  -> I-Snd-CER, Elect -> Wait-Returns
//	Wait-Conn-Ack/Elect + I-Rcv-Conn-Nack -> R-Snd-CEA        -> R-Open
//	Wait-Conn-Ack/Elect + R-Peer-Disc     -> R-Disc           -> Wait-Conn-Ack
//	Wait-Conn-Ack/Elect + R-Conn-CER      -> R-Reject         -> Wait-Conn-Ack/Elect
//	Wait-Conn-Ack/Elect + Timeout         -> Error            -> Closed
func (p *Peer) handleStateWaitConnAckElect(ev eventMessage) error {
	switch ev.event {
	case EventIRcvConnAck:
		// I-Snd-CER, Elect
		p.stopTimeout()
		if err := p.sendCER(); err != nil {
			p.errorCleanup()
			p.setState(StateClosed)
			return err
		}
		p.setState(StateWaitReturns)
		p.startTimeout()
		go p.readLoopI()
		// Perform election
		p.performElection()
		return nil

	case EventIRcvConnNack:
		// R-Snd-CEA -> R-Open (we lost initiator, but have responder)
		p.stopTimeout()
		p.mu.RLock()
		pendingCER := p.pendingCER
		p.mu.RUnlock()
		if err := p.sendCEAOnConn(p.rConn, pendingCER, types.ResultSuccess); err != nil {
			p.closeRConn()
			p.setState(StateClosed)
			return err
		}
		p.mu.Lock()
		p.pendingCER = nil
		p.mu.Unlock()
		p.setState(StateROpen)
		p.startWatchdog()
		go p.readLoopR()
		go p.writeLoop()
		return nil

	case EventRPeerDisc:
		// R-Disc -> back to Wait-Conn-Ack
		p.closeRConn()
		p.mu.Lock()
		p.pendingCER = nil
		p.mu.Unlock()
		p.setState(StateWaitConnAck)
		return nil

	case EventRConnCER:
		// R-Reject: already have an R connection in election
		p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
		return nil

	case EventTimeout:
		// Error
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	default:
		return nil
	}
}

// handleStateWaitReturns handles events in the Wait-Returns state.
// RFC 6733 Section 5.6:
//
//	Wait-Returns + Win-Election -> I-Disc, R-Snd-CEA -> R-Open
//	Wait-Returns + I-Peer-Disc  -> I-Disc, R-Snd-CEA -> R-Open
//	Wait-Returns + I-Rcv-CEA    -> R-Disc            -> I-Open
//	Wait-Returns + R-Peer-Disc  -> R-Disc            -> Wait-I-CEA
//	Wait-Returns + R-Conn-CER   -> R-Reject          -> Wait-Returns
//	Wait-Returns + Timeout      -> Error             -> Closed
func (p *Peer) handleStateWaitReturns(ev eventMessage) error {
	switch ev.event {
	case EventWinElection:
		// I-Disc, R-Snd-CEA -> R-Open (we won, keep R connection)
		p.stopTimeout()
		p.closeIConn()
		p.mu.RLock()
		pendingCER := p.pendingCER
		p.mu.RUnlock()
		if err := p.sendCEAOnConn(p.rConn, pendingCER, types.ResultSuccess); err != nil {
			p.closeRConn()
			p.setState(StateClosed)
			return err
		}
		p.mu.Lock()
		p.pendingCER = nil
		p.mu.Unlock()
		p.setState(StateROpen)
		p.startWatchdog()
		go p.readLoopR()
		go p.writeLoop()
		return nil

	case EventIPeerDisc:
		// I-Disc, R-Snd-CEA -> R-Open
		p.stopTimeout()
		p.closeIConn()
		p.mu.RLock()
		pendingCER := p.pendingCER
		p.mu.RUnlock()
		if err := p.sendCEAOnConn(p.rConn, pendingCER, types.ResultSuccess); err != nil {
			p.closeRConn()
			p.setState(StateClosed)
			return err
		}
		p.mu.Lock()
		p.pendingCER = nil
		p.mu.Unlock()
		p.setState(StateROpen)
		p.startWatchdog()
		go p.readLoopR()
		go p.writeLoop()
		return nil

	case EventIRcvCEA:
		// R-Disc -> I-Open (peer lost election, sent us CEA)
		p.stopTimeout()
		p.closeRConn()
		p.mu.Lock()
		p.pendingCER = nil
		p.mu.Unlock()
		if err := p.processCEA(ev.msg); err != nil {
			p.closeIConn()
			p.setState(StateClosed)
			return err
		}
		p.setState(StateIOpen)
		p.startWatchdog()
		go p.writeLoop()
		return nil

	case EventRPeerDisc:
		// R-Disc -> Wait-I-CEA
		p.closeRConn()
		p.mu.Lock()
		p.pendingCER = nil
		p.mu.Unlock()
		p.setState(StateWaitICEA)
		return nil

	case EventRConnCER:
		// R-Reject: already have an R connection
		p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
		return nil

	case EventTimeout:
		// Error
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	default:
		return nil
	}
}

// handleStateROpen handles events in the R-Open state.
// RFC 6733 Section 5.6:
//
//	R-Open + Send-Message -> R-Snd-Message    -> R-Open
//	R-Open + R-Rcv-Message -> Process         -> R-Open
//	R-Open + R-Rcv-DWR    -> Process-DWR, R-Snd-DWA -> R-Open
//	R-Open + R-Rcv-DWA    -> Process-DWA      -> R-Open
//	R-Open + R-Conn-CER   -> R-Reject         -> R-Open
//	R-Open + Stop         -> R-Snd-DPR        -> Closing
//	R-Open + R-Rcv-DPR    -> R-Snd-DPA        -> Closing
//	R-Open + R-Peer-Disc  -> R-Disc           -> Closed
func (p *Peer) handleStateROpen(ev eventMessage) error {
	switch ev.event {
	case EventSendMessage:
		// R-Snd-Message (handled by writeLoop)
		return nil

	case EventRRcvMessage:
		// Process request/answer
		p.resetWatchdog()
		if p.processInboundMessage(ev.msg) {
			return nil
		}
		return nil

	case EventRRcvDWR:
		// Process-DWR, R-Snd-DWA
		p.resetWatchdog()
		return p.sendDWAOnConn(p.rConn, ev.msg)

	case EventRRcvDWA:
		// Process-DWA
		p.resetWatchdog()
		if p.state == StateSuspect {
			p.setState(StateROpen)
		}
		return nil

	case EventRConnCER:
		// R-Reject: already open
		p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
		return nil

	case EventStop:
		// R-Snd-DPR
		p.stopWatchdog()
		_ = p.sendDPROnConn(p.rConn, types.DisconnectRebooting)
		p.setState(StateClosing)
		p.startTimeout()
		return nil

	case EventRRcvDPR:
		// R-Snd-DPA
		p.stopWatchdog()
		_ = p.sendDPAOnConn(p.rConn, ev.msg)
		p.setState(StateClosing)
		return nil

	case EventRPeerDisc:
		// R-Disc
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	case EventTimeout:
		// Watchdog timeout -> Suspect
		p.setState(StateSuspect)
		return p.sendDWROnConn(p.rConn)

	default:
		return nil
	}
}

// handleStateIOpen handles events in the I-Open state.
// RFC 6733 Section 5.6:
//
//	I-Open + Send-Message -> I-Snd-Message    -> I-Open
//	I-Open + I-Rcv-Message -> Process         -> I-Open
//	I-Open + I-Rcv-DWR    -> Process-DWR, I-Snd-DWA -> I-Open
//	I-Open + I-Rcv-DWA    -> Process-DWA      -> I-Open
//	I-Open + R-Conn-CER   -> R-Reject         -> I-Open
//	I-Open + Stop         -> I-Snd-DPR        -> Closing
//	I-Open + I-Rcv-DPR    -> I-Snd-DPA        -> Closing
//	I-Open + I-Peer-Disc  -> I-Disc           -> Closed
func (p *Peer) handleStateIOpen(ev eventMessage) error {
	switch ev.event {
	case EventSendMessage:
		// I-Snd-Message (handled by writeLoop)
		return nil

	case EventIRcvMessage:
		// Process request/answer
		p.resetWatchdog()
		if p.processInboundMessage(ev.msg) {
			return nil
		}
		return nil

	case EventIRcvDWR:
		// Process-DWR, I-Snd-DWA
		p.resetWatchdog()
		return p.sendDWAOnConn(p.iConn, ev.msg)

	case EventIRcvDWA:
		// Process-DWA
		p.resetWatchdog()
		if p.state == StateSuspect {
			p.setState(StateIOpen)
		}
		return nil

	case EventRConnCER:
		// R-Reject: already open
		p.rejectConnection(ev.conn, ev.msg, types.ResultUnableToComply)
		return nil

	case EventStop:
		// I-Snd-DPR
		p.stopWatchdog()
		_ = p.sendDPROnConn(p.iConn, types.DisconnectRebooting)
		p.setState(StateClosing)
		p.startTimeout()
		return nil

	case EventIRcvDPR:
		// I-Snd-DPA
		p.stopWatchdog()
		_ = p.sendDPAOnConn(p.iConn, ev.msg)
		p.setState(StateClosing)
		return nil

	case EventIPeerDisc:
		// I-Disc
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	case EventTimeout:
		// Watchdog timeout -> Suspect
		p.setState(StateSuspect)
		return p.sendDWROnConn(p.iConn)

	default:
		return nil
	}
}

// handleStateSuspect handles events in the Suspect state (watchdog failure detection).
func (p *Peer) handleStateSuspect(ev eventMessage) error {
	switch ev.event {
	case EventIRcvDWA, EventRRcvDWA:
		// Peer responded, back to open
		p.mu.RLock()
		hasIConn := p.iConn != nil
		p.mu.RUnlock()
		if hasIConn {
			p.setState(StateIOpen)
		} else {
			p.setState(StateROpen)
		}
		p.resetWatchdog()
		return nil

	case EventIRcvMessage, EventRRcvMessage:
		// Any message counts as being alive
		p.mu.RLock()
		hasIConn := p.iConn != nil
		p.mu.RUnlock()
		if hasIConn {
			p.setState(StateIOpen)
		} else {
			p.setState(StateROpen)
		}
		p.resetWatchdog()
		if p.processInboundMessage(ev.msg) {
			return nil
		}
		return nil

	case EventTimeout:
		// Still no response, connection dead
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	case EventIPeerDisc:
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	case EventRPeerDisc:
		p.errorCleanup()
		p.setState(StateClosed)
		p.scheduleReconnect()
		return nil

	default:
		return nil
	}
}

// handleStateClosing handles events in the Closing state.
// RFC 6733 Section 5.6:
//
//	Closing + I-Rcv-DPA   -> I-Disc           -> Closed
//	Closing + R-Rcv-DPA   -> R-Disc           -> Closed
//	Closing + Timeout     -> Error            -> Closed
//	Closing + I-Peer-Disc -> I-Disc           -> Closed
//	Closing + R-Peer-Disc -> R-Disc           -> Closed
func (p *Peer) handleStateClosing(ev eventMessage) error {
	switch ev.event {
	case EventIRcvDPA:
		p.errorCleanup()
		p.setState(StateClosed)
		return nil

	case EventRRcvDPA:
		p.errorCleanup()
		p.setState(StateClosed)
		return nil

	case EventTimeout:
		p.errorCleanup()
		p.setState(StateClosed)
		return nil

	case EventIPeerDisc:
		p.errorCleanup()
		p.setState(StateClosed)
		return nil

	case EventRPeerDisc:
		p.errorCleanup()
		p.setState(StateClosed)
		return nil

	default:
		return nil
	}
}

// ============================================================================
// Election Process (RFC 6733 Section 5.6.4)
// ============================================================================

// performElection performs the election process per RFC 6733 Section 5.6.4.
// The election is performed on the responder. The responder compares the
// Origin-Host received in the CER with its own Origin-Host.
// If the local Origin-Host lexicographically succeeds the received Origin-Host,
// a Win-Election event is issued locally.
func (p *Peer) performElection() {
	localHost := string(p.getEffectiveLocalConfig().DiameterIdentity)
	peerHost := string(p.diameterID)

	// RFC 6733: ASCII comparison, case-insensitive
	localHostLower := strings.ToLower(localHost)
	peerHostLower := strings.ToLower(peerHost)

	if localHostLower > peerHostLower {
		// We win - issue Win-Election event
		p.eventChan <- eventMessage{event: EventWinElection}
	}
	// If we lose, we wait for CEA from peer (they will close their R connection)
}

// ============================================================================
// Connection Management
// ============================================================================

// initiateConnection attempts to establish an initiator connection to the peer.
func (p *Peer) initiateConnection() {
	for _, addr := range p.config.Addresses {
		address := fmt.Sprintf("%s:%d", addr, p.config.Port)

		var conn net.Conn
		var err error

		// Dial using transport abstraction (supporting TCP and SCTP)
		conn, err = transport.Dial(p.config.Network, address, p.config.ConnectTimeout)
		if err != nil {
			continue
		}

		if p.config.UseTLS {
			if p.config.Network == "tcp" {
				// Load TLS config
				tlsConfig, err := p.loadTLSConfig()
				if err != nil {
					_ = conn.Close()
					p.eventChan <- eventMessage{
						event: EventIRcvConnNack,
						err:   fmt.Errorf("failed to load TLS config: %w", err),
					}
					return
				}
				// Wrap with TLS
				tlsConn := tls.Client(conn, tlsConfig)
				_ = conn.SetDeadline(time.Now().Add(p.config.ConnectTimeout))
				if err := tlsConn.HandshakeContext(context.Background()); err != nil {
					_ = conn.Close()
					continue
				}
				_ = conn.SetDeadline(time.Time{})
				conn = tlsConn
			}
		}

		p.mu.Lock()
		p.iConn = conn
		p.stats.ConnectedSince = time.Now()
		p.mu.Unlock()

		p.eventChan <- eventMessage{event: EventIRcvConnAck}
		return
	}

	p.eventChan <- eventMessage{
		event: EventIRcvConnNack,
		err:   fmt.Errorf("failed to connect to peer"),
	}
}

// acceptResponderConnection accepts an incoming connection as the responder connection.
func (p *Peer) acceptResponderConnection(conn net.Conn) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.rConn != nil {
		return fmt.Errorf("responder connection already exists")
	}
	p.rConn = conn
	return nil
}

// rejectConnection sends a CEA with error result code and closes the connection.
func (p *Peer) rejectConnection(conn net.Conn, cer *message.Message, resultCode types.ResultCode) {
	_ = p.sendCEAOnConn(conn, cer, resultCode)
	_ = conn.Close()
}

// closeIConn closes the initiator connection.
func (p *Peer) closeIConn() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.iConn != nil {
		_ = p.iConn.Close()
		p.iConn = nil
	}
}

// closeRConn closes the responder connection.
func (p *Peer) closeRConn() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.rConn != nil {
		_ = p.rConn.Close()
		p.rConn = nil
	}
}

// errorCleanup closes all connections and cleans up resources.
func (p *Peer) errorCleanup() {
	p.stopTimeout()
	p.stopWatchdog()
	p.closeIConn()
	p.closeRConn()
	p.clearPending()
	p.mu.Lock()
	p.pendingCER = nil
	p.mu.Unlock()

	// Signal writeLoop to exit for this connection cycle
	p.mu.Lock()
	select {
	case <-p.connDone:
	default:
		close(p.connDone)
	}
	p.mu.Unlock()
}

// cleanup closes all connections and releases resources.
func (p *Peer) cleanup() {
	p.errorCleanup()
}

// resetConnDone prepares connDone for a new connection cycle.
func (p *Peer) resetConnDone() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.connDone:
		p.connDone = make(chan struct{})
	default:
	}
}

// scheduleReconnect schedules a reconnection attempt if the peer is persistent.
func (p *Peer) scheduleReconnect() {
	if p.config.Persistent {
		time.AfterFunc(p.config.ReconnectInterval, func() {
			select {
			case p.eventChan <- eventMessage{event: EventStart}:
			case <-p.ctx.Done():
			}
		})
	} else {
		p.setState(StateZombie)
	}
}

// ============================================================================
// Timer Management
// ============================================================================

// startTimeout starts the timeout timer for state transitions.
func (p *Peer) startTimeout() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timeoutTimer != nil {
		p.timeoutTimer.Stop()
	}
	p.timeoutTimer = time.AfterFunc(p.config.ConnectTimeout, func() {
		select {
		case p.eventChan <- eventMessage{event: EventTimeout}:
		case <-p.ctx.Done():
		}
	})
}

// stopTimeout stops the timeout timer.
func (p *Peer) stopTimeout() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timeoutTimer != nil {
		p.timeoutTimer.Stop()
		p.timeoutTimer = nil
	}
}

// ============================================================================
// Read Loops
// ============================================================================

// readLoopI continuously reads messages from the initiator connection.
func (p *Peer) readLoopI() {
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		p.mu.RLock()
		conn := p.iConn
		p.mu.RUnlock()

		if conn == nil {
			return
		}

		msg, err := message.ReadMessage(conn)
		if err != nil {
			p.eventChan <- eventMessage{event: EventIPeerDisc, err: err}
			return
		}

		p.mu.Lock()
		p.stats.MessagesReceived++
		p.stats.BytesReceived += uint64(msg.EncodedLen()) //nolint:gosec // G115: value range is protocol-constrained
		p.stats.LastActivity = time.Now()
		p.mu.Unlock()

		// Classify message and send appropriate event
		event := p.classifyMessageI(msg)
		p.eventChan <- eventMessage{event: event, msg: msg}
	}
}

// readLoopR continuously reads messages from the responder connection.
func (p *Peer) readLoopR() {
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		p.mu.RLock()
		conn := p.rConn
		p.mu.RUnlock()

		if conn == nil {
			return
		}

		msg, err := message.ReadMessage(conn)
		if err != nil {
			p.eventChan <- eventMessage{event: EventRPeerDisc, err: err}
			return
		}

		p.mu.Lock()
		p.stats.MessagesReceived++
		p.stats.BytesReceived += uint64(msg.EncodedLen()) //nolint:gosec // G115: value range is protocol-constrained
		p.stats.LastActivity = time.Now()
		p.mu.Unlock()

		// Classify message and send appropriate event
		event := p.classifyMessageR(msg)
		p.eventChan <- eventMessage{event: event, msg: msg}
	}
}

// classifyMessageI determines the event type for a message received on initiator connection.
func (p *Peer) classifyMessageI(msg *message.Message) Event {
	switch msg.CommandCode {
	case types.CmdCodeCapabilitiesExchange:
		if msg.IsRequest() {
			return EventIRcvCER
		}
		return EventIRcvCEA
	case types.CmdCodeDeviceWatchdog:
		if msg.IsRequest() {
			return EventIRcvDWR
		}
		return EventIRcvDWA
	case types.CmdCodeDisconnectPeer:
		if msg.IsRequest() {
			return EventIRcvDPR
		}
		return EventIRcvDPA
	default:
		return EventIRcvMessage
	}
}

// classifyMessageR determines the event type for a message received on responder connection.
func (p *Peer) classifyMessageR(msg *message.Message) Event {
	switch msg.CommandCode {
	case types.CmdCodeCapabilitiesExchange:
		if msg.IsRequest() {
			return EventRRcvCER
		}
		return EventRRcvCEA
	case types.CmdCodeDeviceWatchdog:
		if msg.IsRequest() {
			return EventRRcvDWR
		}
		return EventRRcvDWA
	case types.CmdCodeDisconnectPeer:
		if msg.IsRequest() {
			return EventRRcvDPR
		}
		return EventRRcvDPA
	default:
		return EventRRcvMessage
	}
}

// ============================================================================
// Write Loop
// ============================================================================

// writeLoop handles sending messages from the send queue (fallback path).
// The high-performance writer handles the primary path for application messages.
func (p *Peer) writeLoop() {
	// Start the high-performance writer and add current connection
	if p.writer != nil {
		conn := p.getActiveConn()
		if conn != nil {
			p.writer.AddConnection(conn)
		}
		p.writer.Start()
		defer p.writer.Stop()
	}

	p.mu.RLock()
	connDone := p.connDone
	p.mu.RUnlock()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-connDone:
			return
		case msg := <-p.sendChan:
			if err := p.sendMessageOnActiveConn(msg); err != nil {
				// Determine which connection failed
				p.mu.RLock()
				hasIConn := p.iConn != nil
				p.mu.RUnlock()
				if hasIConn {
					p.eventChan <- eventMessage{event: EventIPeerDisc, err: err}
				} else {
					p.eventChan <- eventMessage{event: EventRPeerDisc, err: err}
				}
				return
			}
		}
	}
}

// ============================================================================
// Message Sending Helpers
// ============================================================================

// getActiveConn returns the currently active connection (I or R).
func (p *Peer) getActiveConn() net.Conn {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Prefer I connection if in I-Open, R connection if in R-Open
	if p.state == StateIOpen && p.iConn != nil {
		return p.iConn
	}
	if p.state == StateROpen && p.rConn != nil {
		return p.rConn
	}
	// Fallback
	if p.iConn != nil {
		return p.iConn
	}
	return p.rConn
}

// sendMessageOnActiveConn sends a message on the active connection.
func (p *Peer) sendMessageOnActiveConn(msg *message.Message) error {
	conn := p.getActiveConn()
	if conn == nil {
		return fmt.Errorf("no active connection")
	}
	return p.sendMessageOnConn(conn, msg)
}

// sendMessageOnConn sends a message on the specified connection.
func (p *Peer) sendMessageOnConn(conn net.Conn, msg *message.Message) error {
	if conn == nil {
		return fmt.Errorf("no connection")
	}

	if msg.IsRequest() {
		// Ensure identifiers are set and track pending for answer correlation (RFC 6733 §6.2)
		if msg.HopByHopID == 0 {
			msg.HopByHopID = p.NextHopByHopID()
		}
		if msg.EndToEndID == 0 {
			msg.EndToEndID = p.nextEndToEndID()
		}
		p.trackPending(msg)
	}

	data, err := msg.Encode()
	if err != nil {
		return err
	}

	_, err = conn.Write(data)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.stats.MessagesSent++
	p.stats.BytesSent += uint64(len(data))
	p.stats.LastActivity = time.Now()
	p.mu.Unlock()

	return nil
}

// sendCEAOnConn sends a CEA on the specified connection.
func (p *Peer) sendCEAOnConn(conn net.Conn, req *message.Message, resultCode types.ResultCode) error {
	cfg := p.getEffectiveLocalConfig()
	msg := message.NewAnswer(req)

	msg.SetResultCode(resultCode)

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))

	for _, ip := range cfg.HostIPAddresses {
		addr := types.NewAddressFromIP(ip)
		msg.AddAVP(message.NewAddressAVP(types.AVPCodeHostIPAddress, types.AVPFlagMandatory, addr))
	}

	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeVendorID, types.AVPFlagMandatory, uint32(cfg.VendorID)))

	if cfg.ProductName != "" {
		msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeProductName, 0, cfg.ProductName))
	}

	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, cfg.OriginStateID))

	for _, appID := range cfg.AuthAppIDs {
		msg.AddAVP(message.NewUnsigned32AVP(
			types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(appID)))
	}
	for _, appID := range cfg.AcctAppIDs {
		msg.AddAVP(message.NewUnsigned32AVP(
			types.AVPCodeAcctApplicationID, types.AVPFlagMandatory, uint32(appID)))
	}

	return p.sendMessageOnConn(conn, msg)
}

// sendDWROnConn sends a DWR on the specified connection.
func (p *Peer) sendDWROnConn(conn net.Conn) error {
	cfg := p.getEffectiveLocalConfig()
	msg := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = p.nextEndToEndID()

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, cfg.OriginStateID))

	return p.sendMessageOnConn(conn, msg)
}

// sendDWAOnConn sends a DWA on the specified connection.
func (p *Peer) sendDWAOnConn(conn net.Conn, req *message.Message) error {
	cfg := p.getEffectiveLocalConfig()
	msg := message.NewAnswer(req)

	msg.SetResultCode(types.ResultSuccess)

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeOriginStateID, types.AVPFlagMandatory, cfg.OriginStateID))

	return p.sendMessageOnConn(conn, msg)
}

// sendDPROnConn sends a DPR on the specified connection.
func (p *Peer) sendDPROnConn(conn net.Conn, cause types.DisconnectCause) error {
	cfg := p.getEffectiveLocalConfig()
	msg := message.NewRequest(types.CmdCodeDisconnectPeer, types.AppIDCommon)
	msg.HopByHopID = p.NextHopByHopID()
	msg.EndToEndID = p.nextEndToEndID()

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))
	msg.AddAVP(message.NewUnsigned32AVP(
		types.AVPCodeDisconnectCause, types.AVPFlagMandatory, uint32(cause)))

	return p.sendMessageOnConn(conn, msg)
}

// sendDPAOnConn sends a DPA on the specified connection.
func (p *Peer) sendDPAOnConn(conn net.Conn, req *message.Message) error {
	cfg := p.getEffectiveLocalConfig()
	msg := message.NewAnswer(req)

	msg.SetResultCode(types.ResultSuccess)

	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginHost, types.AVPFlagMandatory, cfg.DiameterIdentity))
	msg.AddAVP(message.NewDiameterIdentityAVP(
		types.AVPCodeOriginRealm, types.AVPFlagMandatory, cfg.Realm))

	return p.sendMessageOnConn(conn, msg)
}

// ============================================================================
// TLS Support
// ============================================================================

// loadTLSConfig loads the TLS configuration for this peer.
func (p *Peer) loadTLSConfig() (*tls.Config, error) {
	if p.config.TLSConfig == nil {
		return &tls.Config{InsecureSkipVerify: true}, nil //nolint:gosec // G402: TLS verification configured by user
	}

	cfg := &tls.Config{
		InsecureSkipVerify: p.config.TLSConfig.SkipVerify, //nolint:gosec // G402: TLS verification configured by user
		ServerName:         string(p.config.DiameterIdentity),
	}

	// Load certificate if provided
	if p.config.TLSConfig.CertFile != "" && p.config.TLSConfig.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(p.config.TLSConfig.CertFile, p.config.TLSConfig.KeyFile)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	// Load CA if provided
	if p.config.TLSConfig.CAFile != "" {
		// TODO: In a real implementation, load the CA pool here.
		_ = p.config.TLSConfig.CAFile
	}

	return cfg, nil
}

// ============================================================================
// Message Delivery
// ============================================================================

// deliverMessage delivers a message to the application.
func (p *Peer) deliverMessage(msg *message.Message) {
	p.mu.RLock()
	callback := p.onMessage
	p.mu.RUnlock()

	if callback != nil {
		callback(p, msg)
	}

	// Also put on the receive channel
	select {
	case p.recvChan <- msg:
	default:
		// Channel full, drop message
	}
}

// processInboundMessage enforces RFC 6733 §6.2 (Diameter Answer Processing).
// Returns true if the message should be delivered.
func (p *Peer) processInboundMessage(msg *message.Message) bool {
	if msg.IsRequest() {
		p.deliverMessage(msg)
		return true
	}

	// Answers must match a pending request by Hop-by-Hop ID and End-to-End ID.
	if msg.IsRetransmit() {
		return false
	}

	pending, ok := p.popPending(msg.HopByHopID)
	if !ok {
		return false
	}
	if pending.EndToEndID != 0 && pending.EndToEndID != msg.EndToEndID {
		return false
	}

	p.deliverMessage(msg)
	return true
}
