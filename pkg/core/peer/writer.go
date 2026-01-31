// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package peer provides Diameter peer management and state machine.
package peer

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/proto/message"
)

const (
	// writerBatchSize is the maximum number of messages to batch together
	writerBatchSize = 32
	// ringBufferSize must be power of 2 for efficient modulo
	ringBufferSize = 8192
	ringBufferMask = ringBufferSize - 1
)

// msgSlot is a slot in the lock-free ring buffer
type msgSlot struct {
	msg   *message.Message
	ready atomic.Bool
}

// lockFreeQueue is a multi-producer single-consumer lock-free queue
type lockFreeQueue struct {
	slots    [ringBufferSize]msgSlot
	head     atomic.Uint64 // next slot to write (producers)
	tail     atomic.Uint64 // next slot to read (consumer)
	notEmpty chan struct{} // signal when queue becomes non-empty
}

func newLockFreeQueue() *lockFreeQueue {
	return &lockFreeQueue{
		notEmpty: make(chan struct{}, 1),
	}
}

// Push adds a message to the queue (lock-free, multi-producer safe)
func (q *lockFreeQueue) Push(msg *message.Message) bool {
	for {
		head := q.head.Load()
		tail := q.tail.Load()

		// Check if queue is full
		if head-tail >= ringBufferSize {
			return false // Queue full
		}

		// Try to claim this slot
		if q.head.CompareAndSwap(head, head+1) {
			slot := &q.slots[head&ringBufferMask]
			slot.msg = msg
			slot.ready.Store(true)

			// Signal consumer if queue was empty
			select {
			case q.notEmpty <- struct{}{}:
			default:
			}
			return true
		}
		// CAS failed, another producer won - retry
	}
}

// Pop removes and returns a message from the queue (single consumer only)
func (q *lockFreeQueue) Pop() *message.Message {
	tail := q.tail.Load()
	head := q.head.Load()

	if tail >= head {
		return nil // Empty
	}

	slot := &q.slots[tail&ringBufferMask]

	// Wait for producer to finish writing
	for !slot.ready.Load() {
		runtime.Gosched()
	}

	msg := slot.msg
	slot.msg = nil
	slot.ready.Store(false)
	q.tail.Store(tail + 1)

	return msg
}

// PopBatch removes up to maxCount messages from the queue
func (q *lockFreeQueue) PopBatch(batch []*message.Message) int {
	count := 0
	maxCount := len(batch)

	for count < maxCount {
		tail := q.tail.Load()
		head := q.head.Load()

		if tail >= head {
			break // Empty
		}

		slot := &q.slots[tail&ringBufferMask]

		// Wait for producer to finish writing
		for !slot.ready.Load() {
			runtime.Gosched()
		}

		batch[count] = slot.msg
		slot.msg = nil
		slot.ready.Store(false)
		q.tail.Store(tail + 1)
		count++
	}

	return count
}

// Wait returns a channel that signals when the queue has messages
func (q *lockFreeQueue) Wait() <-chan struct{} {
	return q.notEmpty
}

// IsEmpty returns true if the queue is empty
func (q *lockFreeQueue) IsEmpty() bool {
	return q.tail.Load() >= q.head.Load()
}

// highPerfWriter handles batched writes to connections
type highPerfWriter struct {
	peer  *Peer
	queue *lockFreeQueue
	conn  atomic.Pointer[net.Conn] // Primary connection
	wg    sync.WaitGroup
	done  chan struct{}

	mu      sync.Mutex
	started atomic.Bool

	// Stats
	messagesSent atomic.Uint64
	bytesSent    atomic.Uint64
}

func newHighPerfWriter(p *Peer) *highPerfWriter {
	return &highPerfWriter{
		peer:  p,
		queue: newLockFreeQueue(),
		done:  make(chan struct{}),
	}
}

// Send queues a message for sending (lock-free)
// Returns false if writer is not started or queue is full
func (w *highPerfWriter) Send(msg *message.Message) bool {
	if !w.started.Load() {
		return false
	}
	return w.queue.Push(msg)
}

// SetConnection sets the connection to use for writing
func (w *highPerfWriter) SetConnection(conn net.Conn) {
	w.conn.Store(&conn)
}

// AddConnection adds a connection (alias for SetConnection for compatibility)
func (w *highPerfWriter) AddConnection(conn net.Conn) {
	w.SetConnection(conn)
}

// GetConnection returns the connection for writing
func (w *highPerfWriter) GetConnection() net.Conn {
	ptr := w.conn.Load()
	if ptr == nil {
		return nil
	}
	return *ptr
}

// Start starts the writer goroutine. Safe to call after Stop for restart.
func (w *highPerfWriter) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started.Load() {
		return
	}
	w.done = make(chan struct{})
	w.started.Store(true)
	w.wg.Add(1)
	go w.writerLoop()
}

// Stop stops the writer goroutine. May be followed by Start to restart.
func (w *highPerfWriter) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started.Load() {
		return
	}
	w.started.Store(false)
	close(w.done)
	w.wg.Wait()
}

// writerLoop is the single writer goroutine that batches and writes messages
func (w *highPerfWriter) writerLoop() {
	defer w.wg.Done()

	batch := make([]*message.Message, writerBatchSize)
	encodeBuf := make([]byte, 0, 32*1024) // 32KB buffer for batched encoding

	for {
		// Wait for first message or done signal
		select {
		case <-w.done:
			return
		case <-w.queue.Wait():
		}

		// Collect available messages without waiting
		count := w.queue.PopBatch(batch)
		if count == 0 {
			continue
		}

		// Flush immediately - no waiting for more messages
		w.flushBatch(batch[:count], encodeBuf)
	}
}

// flushBatch encodes and writes a batch of messages
func (w *highPerfWriter) flushBatch(batch []*message.Message, buf []byte) {
	if len(batch) == 0 {
		return
	}

	conn := w.GetConnection()
	if conn == nil {
		// Fallback to peer's active connection
		conn = w.peer.getActiveConn()
	}
	if conn == nil {
		return
	}

	// Encode all messages into single buffer
	buf = buf[:0]
	for _, msg := range batch {
		data, err := msg.Encode()
		if err != nil {
			continue
		}
		buf = append(buf, data...)
	}

	if len(buf) == 0 {
		return
	}

	// Single write for entire batch
	n, err := conn.Write(buf)
	if err != nil {
		// Report error to peer
		w.peer.mu.RLock()
		hasIConn := w.peer.iConn != nil
		w.peer.mu.RUnlock()

		if hasIConn {
			select {
			case w.peer.eventChan <- eventMessage{event: EventIPeerDisc, err: err}:
			default:
			}
		} else {
			select {
			case w.peer.eventChan <- eventMessage{event: EventRPeerDisc, err: err}:
			default:
			}
		}
		return
	}

	// Update stats
	w.messagesSent.Add(uint64(len(batch)))
	w.bytesSent.Add(uint64(n)) //nolint:gosec // G115: value range is protocol-constrained
}

// Stats returns the writer statistics
func (w *highPerfWriter) Stats() (messagesSent, bytesSent uint64) {
	return w.messagesSent.Load(), w.bytesSent.Load()
}
