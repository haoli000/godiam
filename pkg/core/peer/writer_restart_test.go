// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"sync"
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// waitForData polls until the mockConn has data written to it, or times out.
func waitForData(t *testing.T, conn *mockConn, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(conn.GetWrittenData()) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for data on connection")
}

// TestWriterStartStopRestart verifies that highPerfWriter supports
// multiple Start/Stop cycles — the core fix for the writer restart bug
// where sync.Once prevented restart after a peer disconnect/reconnect.
func TestWriterStartStopRestart(t *testing.T) {
	dict := testDict()
	cfg := testConfig("writer-restart.test")
	p := New(cfg, dict)
	defer p.Stop()

	w := p.writer

	// Cycle 1: Start, send, stop
	conn1 := newMockConn()
	w.SetConnection(conn1)
	w.Start()

	msg := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	if !w.Send(msg) {
		t.Fatal("Send should succeed after first Start")
	}
	w.Stop()

	// After Stop, Send must fail
	if w.Send(msg) {
		t.Fatal("Send should fail after Stop")
	}

	// Cycle 2: Restart with a new connection (simulates reconnect)
	conn2 := newMockConn()
	w.SetConnection(conn2)
	w.Start()

	if !w.Send(msg) {
		t.Fatal("Send should succeed after restart (second Start)")
	}
	waitForData(t, conn2, 2*time.Second)
	w.Stop()

	// Cycle 3: third restart still works
	conn3 := newMockConn()
	w.SetConnection(conn3)
	w.Start()

	if !w.Send(msg) {
		t.Fatal("Send should succeed on third Start")
	}
	waitForData(t, conn3, 2*time.Second)
	w.Stop()
}

// TestWriterStopIsIdempotent verifies double-Stop doesn't panic.
func TestWriterStopIsIdempotent(t *testing.T) {
	dict := testDict()
	cfg := testConfig("writer-idempotent.test")
	p := New(cfg, dict)
	defer p.Stop()

	w := p.writer
	conn := newMockConn()
	w.SetConnection(conn)
	w.Start()
	w.Stop()
	w.Stop() // must not panic or deadlock
}

// TestWriterStartIsIdempotent verifies double-Start doesn't spawn extra goroutines.
func TestWriterStartIsIdempotent(t *testing.T) {
	dict := testDict()
	cfg := testConfig("writer-start-idem.test")
	p := New(cfg, dict)
	defer p.Stop()

	w := p.writer
	conn := newMockConn()
	w.SetConnection(conn)
	w.Start()
	w.Start() // second call must be a no-op
	w.Stop()  // single Stop must cleanly shut down
}

// TestWriterSendBeforeStart verifies Send returns false before Start.
func TestWriterSendBeforeStart(t *testing.T) {
	dict := testDict()
	cfg := testConfig("writer-no-start.test")
	p := New(cfg, dict)
	defer p.Stop()

	msg := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	if p.writer.Send(msg) {
		t.Fatal("Send should return false when writer is not started")
	}
}

// TestWriterConcurrentSendDuringRestart verifies no races when sends
// happen concurrently with Stop/Start cycles.
func TestWriterConcurrentSendDuringRestart(t *testing.T) {
	dict := testDict()
	cfg := testConfig("writer-concurrent.test")
	p := New(cfg, dict)
	defer p.Stop()

	w := p.writer
	msg := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)

	for cycle := 0; cycle < 5; cycle++ {
		conn := newMockConn()
		w.SetConnection(conn)
		w.Start()

		var wg sync.WaitGroup
		// Launch concurrent senders
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					w.Send(msg) // may succeed or fail, must not panic
				}
			}()
		}
		wg.Wait()
		w.Stop()
	}
}
