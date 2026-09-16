// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package dbg_msg_dumps

import (
	"bytes"
	"log"
	"strings"
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

type dumpsContext struct {
	router *routing.Router
	cfg    *config.Config
	dict   *dictionary.Dictionary
	peers  []*peer.Peer
	start  time.Time
}

func (c *dumpsContext) GetDictionary() *dictionary.Dictionary   { return c.dict }
func (c *dumpsContext) GetRouter() *routing.Router              { return c.router }
func (c *dumpsContext) GetConfig() *config.Config               { return c.cfg }
func (c *dumpsContext) GetPeers() []*peer.Peer                  { return c.peers }
func (c *dumpsContext) GetStartTime() time.Time                 { return c.start }
func (c *dumpsContext) GetExtensionManager() *extension.Manager { return nil }
func (c *dumpsContext) GetEdgeRegistry() *edge.Registry         { return edge.NewRegistry(c.cfg) }

func newDumpsContext(peers ...*peer.Peer) *dumpsContext {
	return &dumpsContext{
		router: func() *routing.Router {
			r := routing.NewRouter()
			r.SetLocalIdentity("local.example.com", "example.com")
			return r
		}(),
		cfg:   &config.Config{Identity: "local.example.com", Realm: "example.com"},
		dict:  dictionary.New(),
		peers: peers,
		start: time.Now().Add(-time.Second),
	}
}

func dumpMessageFixture() *message.Message {
	msg := message.NewRequest(types.CmdCodeCreditControl, 16777238)
	msg.HopByHopID = 10
	msg.EndToEndID = 20
	msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, "session;1"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.net"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.net"))
	msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, "example.com"))
	msg.AddAVP(message.NewGroupedAVP(types.AVPCodeProxyInfo, types.AVPFlagMandatory, 0,
		message.NewDiameterIdentityAVP(types.AVPCodeProxyHost, types.AVPFlagMandatory, "proxy.example.net"),
		message.NewOctetStringAVP(types.AVPCodeProxyState, types.AVPFlagMandatory, []byte("state")),
	))
	return msg
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})
	return &buf
}

func encodedDumpMessage(t *testing.T, msg *message.Message) []byte {
	t.Helper()
	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("encoding message: %v", err)
	}
	return data
}

func TestInitRegistersIncomingOutgoingAndPeerHooks(t *testing.T) {
	buf := captureLog(t)
	p := peer.New(peer.Config{DiameterIdentity: "peer.example.net", Realm: "example.net"}, dictionary.New())
	defer p.Stop()
	ext := &dbgMsgDumps{}
	ctx := newDumpsContext(p)

	ctx.router.AddRealmRoute("example.com", "peer.example.net")
	if err := ext.Init(ctx, map[string]interface{}{"dump_level": "0x8020"}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	msg := dumpMessageFixture()
	if next, err := ctx.router.RouteOut(msg); err != nil || next != "peer.example.net" {
		t.Fatalf("RouteOut = %q, %v; want peer.example.net", next, err)
	}
	if err := ctx.router.RouteIn(p, msg); err == nil || !strings.Contains(err.Error(), "no handler") {
		t.Fatalf("RouteIn error = %v, want dispatch failure after dump handler", err)
	}
	p.SetOnStateChange(nil)

	logs := buf.String()
	if !strings.Contains(logs, "DEBUG: SND") || !strings.Contains(logs, "DEBUG: RCV") {
		t.Fatalf("registered handlers did not dump both directions:\n%s", logs)
	}
}

func TestInitRejectsInvalidDumpLevel(t *testing.T) {
	ext := &dbgMsgDumps{}
	if err := ext.Init(newDumpsContext(), map[string]interface{}{"dump_level": "not-a-number"}); err == nil {
		t.Fatal("invalid dump_level was accepted")
	}
}

func TestRoutingDumpsDoNotMutateMessagesOrCandidates(t *testing.T) {
	buf := captureLog(t)
	ext := &dbgMsgDumps{dumpLevel: hkSndrcvCompact}
	msg := dumpMessageFixture()
	before := encodedDumpMessage(t, msg)
	candidates := []routing.Route{{PeerIdentity: "peer-a", Score: 10, Reason: "first"}}

	got, err := ext.handleIncoming(nil, msg, candidates)
	if err != nil {
		t.Fatalf("incoming dump failed: %v", err)
	}
	if len(got) != 1 || got[0] != candidates[0] {
		t.Fatalf("incoming dump changed route candidates: %+v", got)
	}
	if got, err = ext.handleOutgoing(msg, candidates); err != nil {
		t.Fatalf("outgoing dump failed: %v", err)
	}
	if len(got) != 1 || got[0] != candidates[0] {
		t.Fatalf("outgoing dump changed route candidates: %+v", got)
	}
	if after := encodedDumpMessage(t, msg); !bytes.Equal(after, before) {
		t.Fatal("debug dumping mutated the Diameter message")
	}
	if logs := buf.String(); !strings.Contains(logs, "RCV") || !strings.Contains(logs, "SND") || !strings.Contains(logs, "<unknown peer>") {
		t.Fatalf("unexpected dump log:\n%s", logs)
	}
}

func TestQuietFlagsLeaveTrafficUntouched(t *testing.T) {
	buf := captureLog(t)
	ext := &dbgMsgDumps{dumpLevel: hkSndrcvQuiet | hkPeersQuiet}
	msg := dumpMessageFixture()
	candidates := []routing.Route{{PeerIdentity: "peer-a", Score: 10}}

	if got, err := ext.handleIncoming(nil, msg, candidates); err != nil || len(got) != 1 {
		t.Fatalf("incoming quiet result = %+v, %v", got, err)
	}
	if got, err := ext.handleOutgoing(msg, candidates); err != nil || len(got) != 1 {
		t.Fatalf("outgoing quiet result = %+v, %v", got, err)
	}
	ext.handlePeerStateChange(nil, peer.StateClosed, peer.StateIOpen)

	if buf.Len() != 0 {
		t.Fatalf("quiet dump level wrote logs: %q", buf.String())
	}
}

// The dump level is read from configuration and passed through the send/receive
// handlers, so this drives the handler rather than the selector directly. An
// earlier version of this test called the selector with the hkErrors* bits,
// which is how a mismatch between the bits the callers pass and the bits the
// selector tested survived: isolated, the selector looked correct, while every
// real dump silently fell through to tree format.
func TestConfiguredDumpLevelReachesTheDump(t *testing.T) {
	tests := []struct {
		name  string
		level uint32
		want  message.DumpFormat
	}{
		{name: "compact", level: hkSndrcvCompact, want: message.DumpSummary},
		{name: "full", level: hkSndrcvFull, want: message.DumpFull},
		{name: "tree", level: hkSndrcvTree, want: message.DumpTree},
		{name: "unset defaults to tree", level: 0, want: message.DumpTree},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ext := &dbgMsgDumps{dumpLevel: tc.level}
			if got := ext.sndrcvDumpMode(tc.level & (hkSndrcvCompact | hkSndrcvFull | hkSndrcvTree)); got != tc.want {
				t.Fatalf("dump mode = %v, want %v", got, tc.want)
			}

			msg := message.NewRequest(types.CmdCodeCreditControl, 16777238)
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))

			buf := captureLog(t)
			if _, err := ext.handleIncoming(nil, msg, nil); err != nil {
				t.Fatalf("handleIncoming: %v", err)
			}

			want := map[message.DumpFormat]string{
				message.DumpSummary: msg.DumpSummary(),
				message.DumpFull:    msg.DumpFull(),
				message.DumpTree:    msg.DumpTree(),
			}[tc.want]
			if !strings.Contains(buf.String(), strings.TrimSpace(want)) {
				t.Fatalf("dump did not use %v format:\n%s", tc.want, buf.String())
			}
		})
	}
}

func TestMalformedMessagesStillDump(t *testing.T) {
	buf := captureLog(t)
	ext := &dbgMsgDumps{dumpLevel: hkSndrcvTree | hkPeersTree}
	msg := message.NewRequest(types.CmdCodeCreditControl, 16777238)
	msg.AddAVP(message.NewAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, 0, nil))
	msg.AddAVP(message.NewAVP(types.AVPCodeResultCode, types.AVPFlagMandatory, 0, []byte{1}))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("dumping malformed message panicked: %v", r)
		}
	}()
	_, _ = ext.handleIncoming(nil, msg, nil)
	ext.handlePeerStateChange(nil, peer.StateROpen, peer.StateClosed)

	if logs := buf.String(); !strings.Contains(logs, "Request") || !strings.Contains(logs, "DISCONNECTED") {
		t.Fatalf("malformed message was not dumped:\n%s", logs)
	}
}

func TestStopIsImmediate(t *testing.T) {
	ext := &dbgMsgDumps{}
	start := time.Now()
	if err := ext.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("Stop blocked despite owning no background resources")
	}
}
