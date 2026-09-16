// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package admin_api

import (
	"net/http"

	"github.com/haoli000/godiam/pkg/core/config"
)

// zoneView is the read-only representation of a zone.
type zoneView struct {
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Addresses []string `json:"addresses,omitempty"`
	Port      int      `json:"port,omitempty"`
	TLSPort   int      `json:"tls_port,omitempty"`
	TLS       bool     `json:"tls"`
	MutualTLS bool     `json:"mutual_tls"`
	Partners  int      `json:"partners"`
}

// partnerView is the read-only representation of a partner. It deliberately
// omits key material paths and only reports which policies are active.
type partnerView struct {
	Name           string   `json:"name"`
	Zone           string   `json:"zone"`
	Realms         []string `json:"realms,omitempty"`
	Peers          []string `json:"peers,omitempty"`
	Applications   []uint32 `json:"applications,omitempty"`
	Commands       []uint32 `json:"commands,omitempty"`
	AllowTransit   bool     `json:"allow_transit"`
	Screening      bool     `json:"screening"`
	TopologyHiding string   `json:"topology_hiding"`
	RateLimit      bool     `json:"rate_limit"`
	TLSOverride    bool     `json:"tls_override"`
}

// handleEdge handles GET /api/v1/edge, reporting the live zone and partner model.
func (a *adminAPI) handleEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if a.reg == nil {
		writeError(w, http.StatusServiceUnavailable, "edge registry unavailable")
		return
	}

	zones := make([]zoneView, 0)
	for _, z := range a.reg.Zones() {
		zones = append(zones, zoneView{
			Name:      z.Name,
			Role:      z.Role,
			Addresses: z.Listen.Addresses,
			Port:      z.Listen.Port,
			TLSPort:   z.Listen.SecurePort,
			TLS:       z.TLS.Enabled,
			MutualTLS: z.TLS.VerifyPeer,
			Partners:  len(a.reg.PartnersForZone(z.Name)),
		})
	}

	partners := make([]partnerView, 0)
	for _, p := range a.reg.Partners() {
		peers := make([]string, 0, len(p.Peers))
		for _, pp := range p.Peers {
			peers = append(peers, pp.Identity)
		}
		partners = append(partners, partnerView{
			Name:           p.Name,
			Zone:           p.Zone,
			Realms:         p.Realms,
			Peers:          peers,
			Applications:   p.Applications,
			Commands:       p.Commands,
			AllowTransit:   p.AllowTransit,
			Screening:      p.ScreeningEnabled(a.reg.IsExternalZone(p.Zone)),
			TopologyHiding: topologyMode(p),
			RateLimit:      p.RateLimit.Enabled,
			TLSOverride:    p.TLS != nil,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"configured":   a.reg.Configured(),
		"identity":     a.reg.Identity(),
		"realm":        a.reg.Realm(),
		"default_zone": a.reg.DefaultZone(),
		"zones":        zones,
		"partners":     partners,
	})
}

func topologyMode(p *config.PartnerConfig) string {
	if p.TopologyHiding.Mode == "" {
		return config.TopoHideNone
	}
	return p.TopologyHiding.Mode
}
