// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package transport

import (
	"fmt"
	"net"
	"time"
)

func dialSCTP(_, _ string, _ time.Duration) (net.Conn, error) {
	return nil, fmt.Errorf("SCTP not supported on this platform")
}

func listenSCTP(_, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("SCTP not supported on this platform")
}

func listenSCTPAddresses(_ []net.IP, _ int) (net.Listener, error) {
	return nil, fmt.Errorf("SCTP not supported on this platform")
}
