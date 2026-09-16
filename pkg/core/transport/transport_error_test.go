// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package transport

import (
	"net"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDialReportsTCPErrorPaths(t *testing.T) {
	refusing := tcpAddressThatRefusesConnections(t)

	tests := []struct {
		name    string
		network string
		address string
		want    string
	}{
		{
			name:    "connection refused",
			network: "tcp",
			address: refusing,
			want:    "connect",
		},
		{
			name:    "unknown network",
			network: "diameter",
			address: "127.0.0.1:3868",
			want:    "unknown network",
		},
		{
			name:    "invalid address",
			network: "tcp",
			address: "not-a-socket-address",
			want:    "missing port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := Dial(tc.network, tc.address, 100*time.Millisecond)
			if conn != nil {
				t.Cleanup(func() { _ = conn.Close() })
			}
			if err == nil {
				t.Fatalf("dial unexpectedly succeeded for %s %q", tc.network, tc.address)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("dial error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestListenReportsTCPErrorPathsAndClose(t *testing.T) {
	listener, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on ephemeral TCP port failed: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	if _, ok := listener.Addr().(*net.TCPAddr); !ok {
		t.Fatalf("TCP listen returned %T", listener.Addr())
	}

	duplicate, err := Listen("tcp", listener.Addr().String())
	if err == nil {
		_ = duplicate.Close()
		t.Fatal("second listener unexpectedly bound the same TCP address")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("duplicate listen error %q does not describe a bound port", err)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("listener close failed: %v", err)
	}
	if err := listener.Close(); err == nil {
		t.Fatal("double close should be observable to callers cleaning up listeners")
	}
}

func TestListenRejectsUnknownAndInvalidAddresses(t *testing.T) {
	tests := []struct {
		name    string
		network string
		address string
		want    string
	}{
		{
			name:    "unknown network",
			network: "diameter",
			address: "127.0.0.1:0",
			want:    "unknown network",
		},
		{
			name:    "invalid address",
			network: "tcp",
			address: "not-a-socket-address",
			want:    "missing port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := Listen(tc.network, tc.address)
			if listener != nil {
				t.Cleanup(func() { _ = listener.Close() })
			}
			if err == nil {
				t.Fatalf("listen unexpectedly succeeded for %s %q", tc.network, tc.address)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("listen error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestListenSCTPSelectsPlatformImplementation(t *testing.T) {
	listener, err := Listen("sctp", "127.0.0.1:0")
	if listener != nil {
		t.Cleanup(func() { _ = listener.Close() })
	}

	if runtime.GOOS != "linux" {
		// Darwin does not expose SCTP sockets to Go here; this covers dispatch to
		// the platform stub without forcing kernel support into unit tests.
		if err == nil {
			t.Fatal("SCTP listen unexpectedly succeeded on a non-Linux platform")
		}
		if !strings.Contains(err.Error(), "SCTP not supported") {
			t.Fatalf("unexpected SCTP stub error: %v", err)
		}
		return
	}

	if err != nil {
		t.Logf("SCTP kernel support unavailable in this environment: %v", err)
	}
}

func tcpAddressThatRefusesConnections(t *testing.T) string {
	t.Helper()

	listener, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release TCP port: %v", err)
	}
	return addr
}
