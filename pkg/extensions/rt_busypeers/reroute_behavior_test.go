// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package rt_busypeers

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type recordingSender struct {
	sent []*message.Message
	err  error
}

func (s *recordingSender) Send(msg *message.Message) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, msg)
	return nil
}

func TestTooBusyAnswerExcludesRespondingPeerAndRetriesAlternate(t *testing.T) {
	ext := newExt(t, map[string]interface{}{"retry_max_peers": 3})
	t.Cleanup(func() { _ = ext.Stop() })
	ext.router.AddRealmRoute("example.com", "busy.example.com", "backup.example.com")
	backup := &recordingSender{}
	ext.router.SetPeerLookup(func(identity types.DiamID) (interface{}, bool) {
		if identity != "backup.example.com" {
			return nil, false
		}
		return backup, true
	})

	req := routedRequest(0x1001)
	ext.TrackRequest(nil, req)
	busyPeer := peerWithIdentity(t, "busy.example.com", "example.com")
	t.Cleanup(busyPeer.Stop)

	got, err := ext.handleRouting(busyPeer, busyAnswer(req, types.ResultTooBusy), []routing.Route{{PeerIdentity: "busy.example.com", Score: 100}})
	if !errors.Is(err, routing.ErrConsumed) {
		t.Fatalf("error = %v, want ErrConsumed", err)
	}
	if got != nil {
		t.Fatalf("candidates = %#v, want consumed answer", got)
	}
	if len(backup.sent) != 1 || backup.sent[0] != req {
		t.Fatalf("backup sent %d messages, want original request once", len(backup.sent))
	}
	if ext.busyReceived.Load() != 1 || ext.retriesDone.Load() != 1 || ext.retryFailed.Load() != 0 {
		t.Fatalf("metrics busy/done/failed = %d/%d/%d, want 1/1/0",
			ext.busyReceived.Load(), ext.retriesDone.Load(), ext.retryFailed.Load())
	}
}

func TestOnlyTooBusyAnswersTriggerRetry(t *testing.T) {
	ext := newExt(t, nil)
	t.Cleanup(func() { _ = ext.Stop() })
	ext.router.AddRealmRoute("example.com", "backup.example.com")
	backup := &recordingSender{}
	ext.router.SetPeerLookup(func(types.DiamID) (interface{}, bool) { return backup, true })

	req := routedRequest(0x1002)
	ext.TrackRequest(nil, req)
	candidates := []routing.Route{{PeerIdentity: "original.example.com", Score: 100}}

	got, err := ext.handleRouting(nil, busyAnswer(req, types.ResultUnableToDeliver), candidates)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(got) != len(candidates) || len(backup.sent) != 0 {
		t.Fatalf("non-busy answer was retried: candidates=%#v sent=%d", got, len(backup.sent))
	}
	if ext.busyReceived.Load() != 0 {
		t.Fatalf("busyReceived = %d, want 0", ext.busyReceived.Load())
	}
}

func TestTooBusyGivesUpWhenEveryCandidateIsExcluded(t *testing.T) {
	ext := newExt(t, map[string]interface{}{"retry_max_peers": 10})
	t.Cleanup(func() { _ = ext.Stop() })
	ext.router.AddRealmRoute("example.com", "peer1.example.com", "peer2.example.com")
	ext.router.SetPeerLookup(func(types.DiamID) (interface{}, bool) {
		t.Fatal("LookupPeer must not run when routing finds no alternate")
		return nil, false
	})

	req := routedRequest(0x1003)
	ext.TrackRequest(nil, req)
	ext.mu.Lock()
	ext.retries[req.EndToEndID].excluded["peer1.example.com"] = struct{}{}
	ext.retries[req.EndToEndID].excluded["peer2.example.com"] = struct{}{}
	ext.mu.Unlock()

	candidates := []routing.Route{{PeerIdentity: "peer1.example.com", Score: 100}}
	got, err := ext.handleRouting(nil, busyAnswer(req, types.ResultTooBusy), candidates)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(got) != len(candidates) {
		t.Fatalf("candidates = %#v, want original pass-through", got)
	}
	if ext.retryFailed.Load() != 1 {
		t.Fatalf("retryFailed = %d, want 1", ext.retryFailed.Load())
	}
	ext.mu.Lock()
	_, exists := ext.retries[req.EndToEndID]
	ext.mu.Unlock()
	if exists {
		t.Fatal("retry record survived after every alternate was exhausted")
	}
}

func TestTooBusyRetryFailuresDoNotConsumeTheAnswer(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*rtBusypeers, *recordingSender)
		failures  uint64
	}{
		{
			name: "selected peer missing",
			configure: func(ext *rtBusypeers, _ *recordingSender) {
				ext.router.SetPeerLookup(func(types.DiamID) (interface{}, bool) { return nil, false })
			},
			failures: 1,
		},
		{
			name: "selected peer cannot send",
			configure: func(ext *rtBusypeers, _ *recordingSender) {
				ext.router.SetPeerLookup(func(types.DiamID) (interface{}, bool) { return struct{}{}, true })
			},
			failures: 0,
		},
		{
			name: "send fails",
			configure: func(ext *rtBusypeers, sender *recordingSender) {
				sender.err = errors.New("send failed")
				ext.router.SetPeerLookup(func(types.DiamID) (interface{}, bool) { return sender, true })
			},
			failures: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext := newExt(t, nil)
			t.Cleanup(func() { _ = ext.Stop() })
			ext.router.AddRealmRoute("example.com", "backup.example.com")
			sender := &recordingSender{}
			tc.configure(ext, sender)
			req := routedRequest(0x2000)
			ext.TrackRequest(nil, req)

			candidates := []routing.Route{{PeerIdentity: "busy.example.com", Score: 100}}
			got, err := ext.handleRouting(nil, busyAnswer(req, types.ResultTooBusy), candidates)
			if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if len(got) != len(candidates) {
				t.Fatalf("candidates = %#v, want original pass-through", got)
			}
			if ext.retryFailed.Load() != tc.failures {
				t.Fatalf("retryFailed = %d, want %d", ext.retryFailed.Load(), tc.failures)
			}
		})
	}
}

func routedRequest(e2e types.EndToEndID) *message.Message {
	req := message.NewRequest(types.CmdCodeCreditControl, types.AppIDCreditControl)
	req.EndToEndID = e2e
	req.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	return req
}

func busyAnswer(req *message.Message, code types.ResultCode) *message.Message {
	ans := message.NewAnswer(req)
	ans.EndToEndID = req.EndToEndID
	ans.SetResultCode(code)
	return ans
}

func peerWithIdentity(t *testing.T, identity, realm string) *peer.Peer {
	t.Helper()
	serverSide, remoteSide := net.Pipe()
	t.Cleanup(func() { _ = remoteSide.Close() })
	p := peer.New(peer.Config{DiameterIdentity: types.DiamID(identity), Realm: types.DiamID(realm)}, dictionary.New())
	p.SetLocalOverride(peer.LocalConfig{DiameterIdentity: "local.example.com", Realm: "example.com", ProductName: "godiam-test"})
	readCEA := make(chan error, 1)
	go func() {
		_, err := message.ReadMessage(remoteSide)
		readCEA <- err
	}()
	cer := message.NewRequest(types.CmdCodeCapabilitiesExchange, types.AppIDCommon)
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, types.DiamID(identity)))
	cer.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, types.DiamID(realm)))
	if err := p.StartWithConnection(serverSide, cer); err != nil {
		t.Fatalf("StartWithConnection: %v", err)
	}
	select {
	case err := <-readCEA:
		if err != nil {
			t.Fatalf("reading CEA: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for CEA")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.DiameterID() == types.DiamID(identity) {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("peer identity = %q, want %q", p.DiameterID(), identity)
	return nil
}
