// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package prom_metrics provides Prometheus metrics exposition for Diameter statistics.
package prom_metrics

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/haoli000/godiam/pkg/core/edge"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
)

const (
	defaultPort        = 9090
	defaultBindAddress = "127.0.0.1"
)

type promMetrics struct {
	ctx      extension.InitContext
	server   *http.Server
	registry *prometheus.Registry
}

// Name returns the extension name.
func (e *promMetrics) Name() string { return "prom_metrics" }

// Init initializes the extension.
func (e *promMetrics) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.ctx = ctx
	log.Printf("Initializing extension: prom_metrics")

	port := defaultPort
	if p, ok := config["port"].(int); ok {
		port = p
	} else if p, ok := config["port"].(float64); ok {
		port = int(p)
	}

	bindAddr := defaultBindAddress
	if b, ok := config["bind_address"].(string); ok {
		bindAddr = b
	}

	e.registry = prometheus.NewRegistry()
	e.registry.MustRegister(newDiameterCollector(ctx))
	e.registry.MustRegister(&extensionMetricsCollector{ctx: ctx})
	e.registry.MustRegister(collectors.NewGoCollector())
	e.registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	addr := fmt.Sprintf("%s:%d", bindAddr, port)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{}))

	e.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("prom_metrics listening on http://%s/metrics", addr)
		if err := e.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("prom_metrics HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop implements the Stoppable interface.
func (e *promMetrics) Stop() error {
	if e.server != nil {
		log.Printf("prom_metrics: shutting down HTTP server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return e.server.Shutdown(ctx)
	}
	return nil
}

func init() {
	extension.Register(&promMetrics{})
}

// diameterCollector implements prometheus.Collector for dynamic Diameter metrics.
type diameterCollector struct {
	ctx extension.InitContext

	// Server metrics
	serverInfo   *prometheus.Desc
	serverUptime *prometheus.Desc

	// Peer metrics
	peersTotal             *prometheus.Desc
	peerUp                 *prometheus.Desc
	peerMessagesSent       *prometheus.Desc
	peerMessagesReceived   *prometheus.Desc
	peerBytesSent          *prometheus.Desc
	peerBytesReceived      *prometheus.Desc
	peerConnectionDuration *prometheus.Desc

	// Router metrics
	routingRequestsTotal       *prometheus.Desc
	routingRelayedTotal        *prometheus.Desc
	routingDispatchedTotal     *prometheus.Desc
	routingErrorsTotal         *prometheus.Desc
	routingLoopsDetectedTotal  *prometheus.Desc
	routingAnswersRelayedTotal *prometheus.Desc

	// Dictionary metrics
	dictVendors      *prometheus.Desc
	dictApplications *prometheus.Desc
	dictAVPs         *prometheus.Desc
	dictCommands     *prometheus.Desc

	// Edge metrics
	edgeZones        *prometheus.Desc
	edgePartners     *prometheus.Desc
	edgePartnerPeers *prometheus.Desc

	// Extension metrics
	extensionsTotal *prometheus.Desc
}

func newDiameterCollector(ctx extension.InitContext) *diameterCollector {
	return &diameterCollector{
		ctx: ctx,

		serverInfo: prometheus.NewDesc(
			"diameter_server_info",
			"Diameter server information.",
			[]string{"identity", "realm", "version"}, nil,
		),
		serverUptime: prometheus.NewDesc(
			"diameter_server_uptime_seconds",
			"Seconds since the Diameter server started.",
			nil, nil,
		),

		peersTotal: prometheus.NewDesc(
			"diameter_peers_total",
			"Number of connected Diameter peers.",
			nil, nil,
		),
		peerUp: prometheus.NewDesc(
			"diameter_peer_up",
			"Whether a Diameter peer is in an open state (1) or not (0).",
			[]string{"peer", "realm", "zone", "partner"}, nil,
		),
		peerMessagesSent: prometheus.NewDesc(
			"diameter_peer_messages_sent_total",
			"Total messages sent to a peer.",
			[]string{"peer", "realm", "zone", "partner"}, nil,
		),
		peerMessagesReceived: prometheus.NewDesc(
			"diameter_peer_messages_received_total",
			"Total messages received from a peer.",
			[]string{"peer", "realm", "zone", "partner"}, nil,
		),
		peerBytesSent: prometheus.NewDesc(
			"diameter_peer_bytes_sent_total",
			"Total bytes sent to a peer.",
			[]string{"peer", "realm", "zone", "partner"}, nil,
		),
		peerBytesReceived: prometheus.NewDesc(
			"diameter_peer_bytes_received_total",
			"Total bytes received from a peer.",
			[]string{"peer", "realm", "zone", "partner"}, nil,
		),
		peerConnectionDuration: prometheus.NewDesc(
			"diameter_peer_connection_duration_seconds",
			"Duration of the current peer connection in seconds.",
			[]string{"peer", "realm", "zone", "partner"}, nil,
		),

		edgeZones: prometheus.NewDesc(
			"diameter_edge_zones",
			"Number of configured edge zones, by role.",
			[]string{"role"}, nil,
		),
		edgePartners: prometheus.NewDesc(
			"diameter_edge_partners",
			"Number of configured roaming partners, by zone.",
			[]string{"zone"}, nil,
		),
		edgePartnerPeers: prometheus.NewDesc(
			"diameter_edge_partner_peers_up",
			"Number of connected peers of a partner.",
			[]string{"partner", "zone"}, nil,
		),

		routingRequestsTotal: prometheus.NewDesc(
			"diameter_routing_requests_total",
			"Total requests entering the router.",
			nil, nil,
		),
		routingRelayedTotal: prometheus.NewDesc(
			"diameter_routing_relayed_total",
			"Total requests relayed to another peer.",
			nil, nil,
		),
		routingDispatchedTotal: prometheus.NewDesc(
			"diameter_routing_dispatched_total",
			"Total requests dispatched to a local handler.",
			nil, nil,
		),
		routingErrorsTotal: prometheus.NewDesc(
			"diameter_routing_errors_total",
			"Total routing errors (error answers sent).",
			nil, nil,
		),
		routingLoopsDetectedTotal: prometheus.NewDesc(
			"diameter_routing_loops_detected_total",
			"Total routing loop detections.",
			nil, nil,
		),
		routingAnswersRelayedTotal: prometheus.NewDesc(
			"diameter_routing_answers_relayed_total",
			"Total answers relayed back to the originating peer.",
			nil, nil,
		),

		dictVendors: prometheus.NewDesc(
			"diameter_dictionary_vendors",
			"Number of vendors in the Diameter dictionary.",
			nil, nil,
		),
		dictApplications: prometheus.NewDesc(
			"diameter_dictionary_applications",
			"Number of applications in the Diameter dictionary.",
			nil, nil,
		),
		dictAVPs: prometheus.NewDesc(
			"diameter_dictionary_avps",
			"Number of AVPs in the Diameter dictionary.",
			nil, nil,
		),
		dictCommands: prometheus.NewDesc(
			"diameter_dictionary_commands",
			"Number of commands in the Diameter dictionary.",
			nil, nil,
		),

		extensionsTotal: prometheus.NewDesc(
			"diameter_extensions_total",
			"Number of extensions by state.",
			[]string{"state"}, nil,
		),
	}
}

// Describe sends the descriptors of each metric to the channel.
func (c *diameterCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.serverInfo
	ch <- c.serverUptime
	ch <- c.peersTotal
	ch <- c.peerUp
	ch <- c.peerMessagesSent
	ch <- c.peerMessagesReceived
	ch <- c.peerBytesSent
	ch <- c.peerBytesReceived
	ch <- c.peerConnectionDuration
	ch <- c.edgeZones
	ch <- c.edgePartners
	ch <- c.edgePartnerPeers
	ch <- c.routingRequestsTotal
	ch <- c.routingRelayedTotal
	ch <- c.routingDispatchedTotal
	ch <- c.routingErrorsTotal
	ch <- c.routingLoopsDetectedTotal
	ch <- c.routingAnswersRelayedTotal
	ch <- c.dictVendors
	ch <- c.dictApplications
	ch <- c.dictAVPs
	ch <- c.dictCommands
	ch <- c.extensionsTotal
}

// Collect gathers current metrics from the Diameter server.
func (c *diameterCollector) Collect(ch chan<- prometheus.Metric) {
	c.collectServer(ch)
	c.collectPeers(ch)
	c.collectEdge(ch)
	c.collectRouting(ch)
	c.collectDictionary(ch)
	c.collectExtensions(ch)
}

func (c *diameterCollector) collectServer(ch chan<- prometheus.Metric) {
	cfg := c.ctx.GetConfig()
	ch <- prometheus.MustNewConstMetric(
		c.serverInfo, prometheus.GaugeValue, 1,
		cfg.Identity, cfg.Realm, "godiam",
	)
	ch <- prometheus.MustNewConstMetric(
		c.serverUptime, prometheus.GaugeValue,
		time.Since(c.ctx.GetStartTime()).Seconds(),
	)
}

func (c *diameterCollector) collectPeers(ch chan<- prometheus.Metric) {
	peers := c.ctx.GetPeers()
	ch <- prometheus.MustNewConstMetric(
		c.peersTotal, prometheus.GaugeValue, float64(len(peers)),
	)

	reg := c.ctx.GetEdgeRegistry()
	for _, p := range peers {
		peerID := string(p.DiameterID())
		realm := string(p.Realm())
		zone, partner := edgeLabels(reg, p)
		stats := p.Stats()

		up := float64(0)
		if p.IsOpen() {
			up = 1
		}
		ch <- prometheus.MustNewConstMetric(c.peerUp, prometheus.GaugeValue, up, peerID, realm, zone, partner)
		ch <- prometheus.MustNewConstMetric(c.peerMessagesSent, prometheus.GaugeValue, float64(stats.MessagesSent), peerID, realm, zone, partner)
		ch <- prometheus.MustNewConstMetric(c.peerMessagesReceived, prometheus.GaugeValue, float64(stats.MessagesReceived), peerID, realm, zone, partner)
		ch <- prometheus.MustNewConstMetric(c.peerBytesSent, prometheus.GaugeValue, float64(stats.BytesSent), peerID, realm, zone, partner)
		ch <- prometheus.MustNewConstMetric(c.peerBytesReceived, prometheus.GaugeValue, float64(stats.BytesReceived), peerID, realm, zone, partner)

		if !stats.ConnectedSince.IsZero() {
			ch <- prometheus.MustNewConstMetric(
				c.peerConnectionDuration, prometheus.GaugeValue,
				time.Since(stats.ConnectedSince).Seconds(),
				peerID, realm, zone, partner,
			)
		}
	}
}

// edgeLabels resolves the zone and partner a peer belongs to. Both labels are
// empty when the node is not configured as an edge agent.
func edgeLabels(reg *edge.Registry, p *peer.Peer) (zone, partner string) {
	zone, partner = p.Zone(), p.Partner()
	if reg == nil || !reg.Configured() {
		return zone, partner
	}
	identity := string(p.DiameterID())
	if zone == "" {
		zone = reg.ZoneNameForPeer(identity)
	}
	if partner == "" {
		if pc, ok := reg.PartnerForPeer(identity); ok {
			partner = pc.Name
		}
	}
	return zone, partner
}

// collectEdge reports the zone and partner model plus partner connectivity.
func (c *diameterCollector) collectEdge(ch chan<- prometheus.Metric) {
	reg := c.ctx.GetEdgeRegistry()
	if reg == nil || !reg.Configured() {
		return
	}

	byRole := map[string]float64{}
	for _, z := range reg.Zones() {
		byRole[z.Role]++
	}
	for role, count := range byRole {
		ch <- prometheus.MustNewConstMetric(c.edgeZones, prometheus.GaugeValue, count, role)
	}

	byZone := map[string]float64{}
	connected := map[string]float64{}
	for _, p := range reg.Partners() {
		byZone[p.Zone]++
		connected[p.Name] = 0
	}
	for zone, count := range byZone {
		ch <- prometheus.MustNewConstMetric(c.edgePartners, prometheus.GaugeValue, count, zone)
	}

	for _, p := range c.ctx.GetPeers() {
		if !p.IsOpen() {
			continue
		}
		if pc, ok := reg.PartnerForPeer(string(p.DiameterID())); ok {
			connected[pc.Name]++
		}
	}
	for _, p := range reg.Partners() {
		ch <- prometheus.MustNewConstMetric(c.edgePartnerPeers, prometheus.GaugeValue, connected[p.Name], p.Name, p.Zone)
	}
}

func (c *diameterCollector) collectRouting(ch chan<- prometheus.Metric) {
	routed, relayed, dispatched, errors, loops, answersRelayed := c.ctx.GetRouter().Stats()
	ch <- prometheus.MustNewConstMetric(c.routingRequestsTotal, prometheus.GaugeValue, float64(routed))
	ch <- prometheus.MustNewConstMetric(c.routingRelayedTotal, prometheus.GaugeValue, float64(relayed))
	ch <- prometheus.MustNewConstMetric(c.routingDispatchedTotal, prometheus.GaugeValue, float64(dispatched))
	ch <- prometheus.MustNewConstMetric(c.routingErrorsTotal, prometheus.GaugeValue, float64(errors))
	ch <- prometheus.MustNewConstMetric(c.routingLoopsDetectedTotal, prometheus.GaugeValue, float64(loops))
	ch <- prometheus.MustNewConstMetric(c.routingAnswersRelayedTotal, prometheus.GaugeValue, float64(answersRelayed))
}

func (c *diameterCollector) collectDictionary(ch chan<- prometheus.Metric) {
	stats := c.ctx.GetDictionary().Stats()
	ch <- prometheus.MustNewConstMetric(c.dictVendors, prometheus.GaugeValue, float64(stats.Vendors))
	ch <- prometheus.MustNewConstMetric(c.dictApplications, prometheus.GaugeValue, float64(stats.Applications))
	ch <- prometheus.MustNewConstMetric(c.dictAVPs, prometheus.GaugeValue, float64(stats.AVPs))
	ch <- prometheus.MustNewConstMetric(c.dictCommands, prometheus.GaugeValue, float64(stats.Commands))
}

func (c *diameterCollector) collectExtensions(ch chan<- prometheus.Metric) {
	mgr := c.ctx.GetExtensionManager()
	if mgr == nil {
		return
	}

	stateCounts := make(map[string]int)
	for _, ext := range mgr.List() {
		stateCounts[ext.State]++
	}
	for state, count := range stateCounts {
		ch <- prometheus.MustNewConstMetric(
			c.extensionsTotal, prometheus.GaugeValue,
			float64(count), state,
		)
	}
}

// extensionMetricsCollector dynamically collects metrics from extensions
// that implement MetricsProvider. Registered as an unchecked collector
// (empty Describe) because the set of extension metrics is dynamic.
type extensionMetricsCollector struct {
	ctx extension.InitContext
}

// Describe sends no descriptors (unchecked collector).
func (c *extensionMetricsCollector) Describe(_ chan<- *prometheus.Desc) {}

// Collect iterates all active MetricsProvider extensions and converts
// their Metric values to prometheus format.
func (c *extensionMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	mgr := c.ctx.GetExtensionManager()
	if mgr == nil {
		return
	}

	allMetrics := mgr.CollectExtensionMetrics()
	for extName, metrics := range allMetrics {
		for _, m := range metrics {
			fqName := fmt.Sprintf("diameter_ext_%s_%s", extName, m.Name)

			labelNames := make([]string, 0, len(m.Labels))
			for k := range m.Labels {
				labelNames = append(labelNames, k)
			}
			sort.Strings(labelNames)

			labelValues := make([]string, 0, len(m.Labels))
			for _, k := range labelNames {
				labelValues = append(labelValues, m.Labels[k])
			}

			desc := prometheus.NewDesc(fqName, m.Help, labelNames, nil)

			valueType := prometheus.GaugeValue
			if m.Type == extension.MetricCounter {
				valueType = prometheus.CounterValue
			}

			ch <- prometheus.MustNewConstMetric(desc, valueType, m.Value, labelValues...)
		}
	}
}
