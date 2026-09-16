// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package peer

import (
	"testing"
	"time"

	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

func queueMessage(id uint32) *message.Message {
	msg := message.NewRequest(types.CmdCodeDeviceWatchdog, types.AppIDCommon)
	msg.HopByHopID = types.HopByHopID(id)
	msg.EndToEndID = types.EndToEndID(id + 1000)
	return msg
}

func TestLockFreeQueuePopsMessagesInFIFOOrder(t *testing.T) {
	q := newLockFreeQueue()
	if !q.IsEmpty() {
		t.Fatal("new queue is not empty")
	}
	if got := q.Pop(); got != nil {
		t.Fatalf("empty Pop returned %#v", got)
	}

	for i := uint32(1); i <= 3; i++ {
		if !q.Push(queueMessage(i)) {
			t.Fatalf("Push(%d) reported full queue", i)
		}
	}
	if q.IsEmpty() {
		t.Fatal("queue reports empty after pushes")
	}
	for i := uint32(1); i <= 3; i++ {
		got := q.Pop()
		if got == nil {
			t.Fatalf("Pop(%d) returned nil", i)
		}
		if got.HopByHopID != types.HopByHopID(i) {
			t.Fatalf("Pop(%d) hbh = %d, want %d", i, got.HopByHopID, i)
		}
	}
	if !q.IsEmpty() {
		t.Fatal("queue is not empty after popping all messages")
	}
}

func TestLockFreeQueueRejectsPushWhenFullAndAcceptsAfterPop(t *testing.T) {
	q := newLockFreeQueue()
	for i := 0; i < ringBufferSize; i++ {
		if !q.Push(queueMessage(uint32(i + 1))) {
			t.Fatalf("queue became full after %d pushes, want capacity %d", i, ringBufferSize)
		}
	}
	if q.Push(queueMessage(999999)) {
		t.Fatal("Push succeeded even though the ring buffer was full")
	}
	if got := q.Pop(); got == nil || got.HopByHopID != 1 {
		t.Fatalf("first pop after full queue = %#v, want first message", got)
	}
	if !q.Push(queueMessage(999999)) {
		t.Fatal("Push did not succeed after a pop freed one slot")
	}
}

func TestHighPerfWriterFlushesQueuedMessagesInOrder(t *testing.T) {
	p := New(testConfig("writer-order.example.com"), testDict())
	t.Cleanup(p.Stop)
	conn := newMockConn()
	p.writer.SetConnection(conn)
	p.writer.Start()
	t.Cleanup(p.writer.Stop)

	for i := uint32(1); i <= 4; i++ {
		if !p.writer.Send(queueMessage(i)) {
			t.Fatalf("Send(%d) failed", i)
		}
	}
	waitForData(t, conn, 2*time.Second)

	data := conn.GetWrittenData()
	for i := uint32(1); i <= 4; i++ {
		if len(data) == 0 {
			t.Fatalf("only decoded %d messages", i-1)
		}
		msg, err := message.DecodeMessage(data)
		if err != nil {
			t.Fatalf("decoding message %d: %v", i, err)
		}
		if msg.HopByHopID != types.HopByHopID(i) {
			t.Fatalf("message %d hbh = %d, want %d", i, msg.HopByHopID, i)
		}
		data = data[msg.EncodedLen():]
	}
	if len(data) != 0 {
		t.Fatalf("%d trailing bytes after four messages", len(data))
	}
}
