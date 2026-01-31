// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package transport provides network transport abstractions for Diameter connections.
package transport

import (
	"net"
	"time"
)

// Dial connects to the address on the named network.
// Supported networks: "tcp", "tcp4", "tcp6", "sctp", "sctp4", "sctp6".
func Dial(network, address string, timeout time.Duration) (net.Conn, error) {
	switch network {
	case "sctp", "sctp4", "sctp6":
		return dialSCTP(network, address, timeout)
	default:
		return net.DialTimeout(network, address, timeout) //nolint:noctx // transport layer dial does not propagate context
	}
}

// Listen announces on the local network address.
func Listen(network, address string) (net.Listener, error) {
	switch network {
	case "sctp", "sctp4", "sctp6":
		return listenSCTP(network, address)
	default:
		return net.Listen(network, address) //nolint:noctx // transport layer does not propagate context
	}
}

// ListenSCTPAddresses creates an SCTP listener bound to specific IP addresses
// for multihoming. All addresses share a single SCTP socket.
func ListenSCTPAddresses(ips []net.IP, port int) (net.Listener, error) {
	return listenSCTPAddresses(ips, port)
}
