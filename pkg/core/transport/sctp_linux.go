// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package transport

import (
	"fmt"
	"net"
	"time"

	"github.com/ishidawataru/sctp"
)

func dialSCTP(network, address string, timeout time.Duration) (net.Conn, error) {
	addr, err := sctp.ResolveSCTPAddr(network, address)
	if err != nil {
		return nil, err
	}

	// sctp.DialSCTP does not accept a timeout, so we enforce one
	// with a goroutine + channel, matching net.DialTimeout for TCP.
	type dialResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := sctp.DialSCTP(network, nil, addr)
		ch <- dialResult{conn, err}
	}()

	select {
	case res := <-ch:
		return res.conn, res.err
	case <-time.After(timeout):
		// Drain the goroutine asynchronously and close any late connection.
		go func() {
			if res := <-ch; res.conn != nil {
				res.conn.Close() //nolint:errcheck,gosec // best-effort cleanup of late connection
			}
		}()
		return nil, fmt.Errorf("sctp dial to %s timed out after %v", address, timeout)
	}
}

func listenSCTP(network, address string) (net.Listener, error) {
	addr, err := sctp.ResolveSCTPAddr(network, address)
	if err != nil {
		return nil, err
	}
	return sctp.ListenSCTP(network, addr)
}

func listenSCTPAddresses(ips []net.IP, port int) (net.Listener, error) {
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses provided for SCTP multihoming")
	}
	ipAddrs := make([]net.IPAddr, len(ips))
	for i, ip := range ips {
		ipAddrs[i] = net.IPAddr{IP: ip}
	}
	addr := &sctp.SCTPAddr{
		IPAddrs: ipAddrs,
		Port:    port,
	}
	return sctp.ListenSCTP("sctp", addr)
}
