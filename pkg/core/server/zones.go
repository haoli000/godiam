// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/transport"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// startZoneListeners starts every listener belonging to a single edge zone.
func (s *Server) startZoneListeners(zone *config.ZoneConfig) error {
	binds := zone.Listen.Addresses
	// A configuration with no zones keeps the historical behaviour of binding
	// TCP to every interface; listen.addresses only ever steered SCTP there.
	if len(binds) == 0 || zone.IsImplicit() {
		binds = []string{""}
	}

	var tlsConfig *tls.Config
	if zone.TLS.Enabled {
		var err error
		tlsConfig, err = buildServerTLSConfig(&zone.TLS)
		if err != nil {
			return fmt.Errorf("loading TLS config: %w", err)
		}
	}

	if zone.Listen.EnableTCP {
		// The main port speaks TLS when the zone enables it, which is how a
		// single-port configuration has always behaved.
		if err := s.listenTCP(zone, binds, zone.Listen.Port, tlsConfig); err != nil {
			return err
		}
	}

	if zone.Listen.EnableTLS {
		if tlsConfig == nil {
			return fmt.Errorf("zone %q: listen.enable_tls requires tls.enabled", zone.Name)
		}
		if err := s.listenTCP(zone, binds, zone.Listen.SecurePort, tlsConfig); err != nil {
			return err
		}
	}

	if zone.Listen.EnableSCTP {
		var (
			l   net.Listener
			err error
		)
		if len(zone.Listen.Addresses) > 0 {
			var ips []net.IP
			for _, a := range zone.Listen.Addresses {
				if ip := net.ParseIP(a); ip != nil {
					ips = append(ips, ip)
				}
			}
			l, err = transport.ListenSCTPAddresses(ips, zone.Listen.Port)
		} else {
			l, err = transport.Listen("sctp", fmt.Sprintf(":%d", zone.Listen.Port))
		}
		if err != nil {
			return fmt.Errorf("SCTP listen: %w", err)
		}
		s.addListener(l, zone)
		log.Printf("Zone %q listening on %s (sctp)", zone.Name, l.Addr())
	}

	return nil
}

// listenTCP binds one TCP listener per configured address on the given port,
// wrapping it in TLS when a configuration is supplied.
func (s *Server) listenTCP(zone *config.ZoneConfig, binds []string, port int, tlsConfig *tls.Config) error {
	for _, bind := range binds {
		addr := net.JoinHostPort(bind, fmt.Sprintf("%d", port))
		var (
			l   net.Listener
			err error
		)
		if tlsConfig != nil {
			l, err = tls.Listen("tcp", addr, tlsConfig)
		} else {
			l, err = transport.Listen("tcp", addr)
		}
		if err != nil {
			return fmt.Errorf("TCP listen on %s: %w", addr, err)
		}
		s.addListener(l, zone)
		log.Printf("Zone %q listening on %s (tcp, tls=%v)", zone.Name, l.Addr(), tlsConfig != nil)
	}
	return nil
}

// addListener registers a listener and starts its accept loop.
func (s *Server) addListener(l net.Listener, zone *config.ZoneConfig) {
	s.mu.Lock()
	s.listeners = append(s.listeners, l)
	s.mu.Unlock()
	go s.acceptLoop(l, zone)
}

// buildServerTLSConfig builds the server-side TLS configuration for a zone.
func buildServerTLSConfig(t *config.ZoneTLSConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if err != nil {
		return nil, err
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tlsMinVersion(t.MinVersion),
	}

	if t.CAFile != "" {
		pool, err := loadCertPool(t.CAFile)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
	}

	if t.VerifyPeer {
		if cfg.ClientCAs == nil {
			// Explicit zones are rejected by configuration validation before
			// reaching this point. A configuration predating the edge model can
			// still land here, and it must keep starting: leaving ClientCAs nil
			// verifies against the host's root store, which is weaker than
			// pinned anchors but is what the operator asked for.
			log.Printf("WARNING: zone TLS sets verify_peer without ca_file; " +
				"client certificates will be checked against the system root store")
		}
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return cfg, nil
}

// loadCertPool reads a PEM bundle into an x509 pool.
func loadCertPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path) //nolint:gosec // G304: path is admin-supplied config
	if err != nil {
		return nil, fmt.Errorf("reading ca_file %q: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ca_file %q contains no usable certificates", path)
	}
	return pool, nil
}

// tlsMinVersion maps a configured minimum TLS version to its constant.
func tlsMinVersion(v string) uint16 {
	if v == "1.3" {
		return tls.VersionTLS13
	}
	return tls.VersionTLS12
}

// zoneForConnection returns the zone a listener belongs to, falling back to the
// implicit default zone when the edge model is not configured.
func (s *Server) zoneOrDefault(zone *config.ZoneConfig) *config.ZoneConfig {
	if zone != nil {
		return zone
	}
	zones := s.config.EffectiveZones()
	return &zones[0]
}

// certificateIdentities returns the CN and DNS SANs of a certificate.
func certificateIdentities(cert *x509.Certificate) []string {
	ids := make([]string, 0, len(cert.DNSNames)+1)
	if cert.Subject.CommonName != "" {
		ids = append(ids, cert.Subject.CommonName)
	}
	ids = append(ids, cert.DNSNames...)
	return ids
}

// certIdentityMatches reports whether the peer certificate presented on conn
// carries an identity matching the Diameter identity claimed in the CER.
// A wildcard CN/SAN ("*.example.com") matches any strict subdomain.
func certIdentityMatches(conn net.Conn, identity string) (bool, []string) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return false, nil
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return false, nil
	}
	want := strings.ToLower(strings.TrimSuffix(identity, "."))
	ids := certificateIdentities(state.PeerCertificates[0])
	for _, id := range ids {
		got := strings.ToLower(strings.TrimSuffix(id, "."))
		if got == want {
			return true, ids
		}
		if strings.HasPrefix(got, "*.") && strings.HasSuffix(want, got[1:]) {
			return true, ids
		}
	}
	return false, ids
}

// admissionResult carries the outcome of an inbound CER admission decision.
type admissionResult struct {
	accepted bool
	code     types.ResultCode
	partner  string
	reason   string
}

// admitPeer decides whether an inbound peer may connect on the given zone and,
// when accepted, reports the roaming partner it belongs to.
//
//nolint:gocyclo // admission is a flat decision table; splitting it hurts readability
func (s *Server) admitPeer(zone *config.ZoneConfig, conn net.Conn, originHost, originRealm types.DiamID) admissionResult {
	oh, or := string(originHost), string(originRealm)

	// TLS client-certificate identity binding.
	if zone.TLS.RequireClientCertIdentity {
		ok, ids := certIdentityMatches(conn, oh)
		if !ok {
			return admissionResult{
				code:   types.ResultUnknownPeer,
				reason: fmt.Sprintf("client certificate identities %v do not match Origin-Host %q", ids, oh),
			}
		}
	}

	reg := s.GetEdgeRegistry()

	// Partner-aware admission: a peer claimed by a partner must arrive on that
	// partner's zone, and an external zone only accepts known partner traffic.
	if partner, ok := reg.PartnerForPeer(oh); ok {
		if partner.Zone != zone.Name {
			return admissionResult{
				code:   types.ResultUnknownPeer,
				reason: fmt.Sprintf("peer %q belongs to partner %q on zone %q, not %q", oh, partner.Name, partner.Zone, zone.Name),
			}
		}
		if len(partner.Realms) > 0 && !edge.RealmMatches(or, partner.Realms) {
			return admissionResult{
				code:   types.ResultUnknownPeer,
				reason: fmt.Sprintf("peer %q presented realm %q outside partner %q realms", oh, or, partner.Name),
			}
		}
		return admissionResult{accepted: true, code: types.ResultSuccess, partner: partner.Name}
	}

	if zone.IsExternal() {
		// Unlisted peer on an external zone: accept only if its realm belongs to
		// a partner of this very zone, or if the zone policy explicitly allows it.
		if partner, ok := reg.PartnerForRealm(or); ok && partner.Zone == zone.Name {
			return admissionResult{accepted: true, code: types.ResultSuccess, partner: partner.Name}
		}
		if allowedByZonePolicy(zone, oh) {
			return admissionResult{accepted: true, code: types.ResultSuccess}
		}
		return admissionResult{
			code:   types.ResultUnknownPeer,
			reason: fmt.Sprintf("peer %q (realm %q) is not a known partner of external zone %q", oh, or, zone.Name),
		}
	}

	// Internal zone: legacy node-level policy.
	if allowedByZonePolicy(zone, oh) {
		return admissionResult{accepted: true, code: types.ResultSuccess}
	}
	for _, pc := range s.config.Peers {
		if pc.Identity != oh {
			continue
		}
		if pc.Zone != "" && pc.Zone != zone.Name {
			return admissionResult{
				code:   types.ResultUnknownPeer,
				reason: fmt.Sprintf("peer %q is configured on zone %q, not %q", oh, pc.Zone, zone.Name),
			}
		}
		if pc.Realm == "" || pc.Realm == or {
			return admissionResult{accepted: true, code: types.ResultSuccess}
		}
		// Realm mismatch: reject as unknown peer (aligning with freeDiameter behavior)
		return admissionResult{
			code:   types.ResultUnknownPeer,
			reason: fmt.Sprintf("peer %q presented realm %q, expected %q", oh, or, pc.Realm),
		}
	}

	return admissionResult{
		code:   types.ResultUnknownPeer,
		reason: fmt.Sprintf("peer %q is not configured and zone %q does not allow unknown peers", oh, zone.Name),
	}
}

// allowedByZonePolicy applies a zone's peers_policy to an identity.
func allowedByZonePolicy(zone *config.ZoneConfig, identity string) bool {
	if zone.PeersPolicy.AllowUnknown {
		return true
	}
	for _, id := range zone.PeersPolicy.AllowIdentities {
		if id == identity {
			return true
		}
	}
	return false
}
