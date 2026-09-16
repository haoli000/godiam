// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_ereg provides a routing extension that adjusts peer scores based
// on regex patterns matching AVP values.
// This corresponds to rt_ereg from the original freeDiameter.
package rt_ereg

import (
	"fmt"
	"log"
	"regexp"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type eregRule struct {
	avpCode  types.AVPCode
	vendorID types.VendorID
	pattern  *regexp.Regexp
	server   string // peer identity to boost
	score    int    // score to add
}

type rtEreg struct {
	dict    *dictionary.Dictionary
	rules   []eregRule
	matched atomic.Uint64
}

// Name returns the extension name.
func (e *rtEreg) Name() string { return "rt_ereg" }

// Init initializes the extension.
func (e *rtEreg) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.dict = ctx.GetDictionary()

	rulesRaw, ok := config["rules"]
	if !ok {
		return fmt.Errorf("rt_ereg: 'rules' configuration required")
	}

	rulesList, ok := rulesRaw.([]interface{})
	if !ok {
		return fmt.Errorf("rt_ereg: 'rules' must be a list")
	}

	for _, ruleRaw := range rulesList {
		ruleMap, ok := ruleRaw.(map[string]interface{})
		if !ok {
			continue
		}

		avpName, _ := ruleMap["avp"].(string)
		patternStr, _ := ruleMap["pattern"].(string)
		server, _ := ruleMap["server"].(string)

		if avpName == "" || patternStr == "" || server == "" {
			return fmt.Errorf("rt_ereg: each rule must have 'avp', 'pattern', and 'server'")
		}

		re, err := regexp.Compile(patternStr)
		if err != nil {
			return fmt.Errorf("rt_ereg: invalid pattern %q: %w", patternStr, err)
		}

		score := 100 // default
		if s, ok := ruleMap["score"]; ok {
			switch v := s.(type) {
			case int:
				score = v
			case float64:
				score = int(v)
			}
		}

		// Look up AVP code from dictionary
		avpCode, vendorID := e.lookupAVP(avpName)

		e.rules = append(e.rules, eregRule{
			avpCode:  avpCode,
			vendorID: vendorID,
			pattern:  re,
			server:   server,
			score:    score,
		})
	}

	log.Printf("Initializing extension: rt_ereg (%d rules)", len(e.rules))

	// Priority 45: runs after ignore_dh (35) and rewrite (40), before dispatch.
	// Score adjustments must run on the outgoing path: RouteIn hands incoming
	// handlers an empty candidate list and discards what they return, so a boost
	// registered there never reaches the routing decision.
	ctx.GetRouter().RegisterOutHandler("rt_ereg", 45, e.handleRouting)
	return nil
}

// lookupAVP resolves an AVP name to its code and vendor ID using the dictionary.
func (e *rtEreg) lookupAVP(name string) (types.AVPCode, types.VendorID) {
	// Try well-known AVPs first
	wellKnown := map[string]types.AVPCode{
		"User-Name":         types.AVPCodeUserName,
		"Session-Id":        types.AVPCodeSessionID,
		"Origin-Host":       types.AVPCodeOriginHost,
		"Origin-Realm":      types.AVPCodeOriginRealm,
		"Destination-Host":  types.AVPCodeDestinationHost,
		"Destination-Realm": types.AVPCodeDestinationRealm,
	}
	if code, ok := wellKnown[name]; ok {
		return code, 0
	}

	// Fall back to dictionary lookup
	if e.dict != nil {
		if avpDef, ok := e.dict.GetAVPByName(name); ok {
			return avpDef.Code, avpDef.VendorID
		}
	}

	log.Printf("rt_ereg: warning: AVP %q not found in dictionary", name)
	return 0, 0
}

// handleRouting checks AVP values against regex rules and adjusts candidate scores.
func (e *rtEreg) handleRouting(msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	if !msg.IsRequest() {
		return candidates, nil
	}

	for _, rule := range e.rules {
		if rule.avpCode == 0 {
			continue
		}

		avp := msg.FindAVP(rule.avpCode, rule.vendorID)
		if avp == nil {
			continue
		}

		// Get AVP value as string
		value := avp.GetUTF8String()
		if value == "" {
			value = string(avp.GetDiameterIdentity())
		}
		if value == "" {
			continue
		}

		if rule.pattern.MatchString(value) {
			// Boost the score of the matching server
			for i := range candidates {
				if string(candidates[i].PeerIdentity) == rule.server {
					candidates[i].Score += rule.score
					candidates[i].Reason = fmt.Sprintf("rt_ereg: pattern %s matched", rule.pattern.String())
					e.matched.Add(1)
				}
			}
		}
	}

	return candidates, nil
}

// Stop implements the Stoppable interface.
func (e *rtEreg) Stop() error { return nil }

// Metrics implements extension.MetricsProvider.
func (e *rtEreg) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "matched_total",
			Help:  "Total regex matches that adjusted routing scores.",
			Type:  extension.MetricCounter,
			Value: float64(e.matched.Load()),
		},
	}
}

func init() {
	extension.Register(&rtEreg{})
}
