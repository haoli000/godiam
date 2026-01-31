// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package transport

import (
	"net"
	"runtime"
	"testing"
	"time"
)

func TestDialSCTP_Platform(t *testing.T) {
	_, err := Dial("sctp", "127.0.0.1:3868", time.Second)

	if runtime.GOOS == "linux" {
		// On Linux, it might succeed or fail with connection refused or specialized error
		// We can't easily assert exactly without running sctp server
		if err != nil && err.Error() == "SCTP not supported on this platform" {
			t.Error("SCTP should be supported on Linux (build tag issue?)")
		}
	} else {
		// On non-Linux (Mac), it MUST fail with stub error
		if err == nil {
			t.Error("Expected error on non-Linux platform")
		} else if err.Error() != "SCTP not supported on this platform" {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestListenSCTPAddresses_Platform(t *testing.T) {
	ips := []net.IP{net.ParseIP("127.0.0.1")}
	_, err := ListenSCTPAddresses(ips, 0)

	if runtime.GOOS == "linux" {
		if err != nil && err.Error() == "SCTP not supported on this platform" {
			t.Error("SCTP should be supported on Linux (build tag issue?)")
		}
	} else {
		if err == nil {
			t.Error("Expected error on non-Linux platform")
		} else if err.Error() != "SCTP not supported on this platform" {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestListenSCTPAddresses_EmptyIPs(t *testing.T) {
	_, err := ListenSCTPAddresses(nil, 3868)
	if runtime.GOOS != "linux" {
		// Non-Linux returns platform error before checking IPs
		t.Skip("SCTP not supported on this platform")
	}
	if err == nil {
		t.Error("Expected error for empty IP list")
	}
}
