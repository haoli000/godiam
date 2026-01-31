// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package rt_rewrite provides a routing extension that rewrites AVPs in Diameter messages.
package rt_rewrite

import (
	"log"
	"strings"
	"sync/atomic"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/core/routing"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

// rtRewrite is the AVP rewriting extension
type rtRewrite struct {
	rules        []rule
	dict         *dictionary.Dictionary
	mapsApplied  atomic.Uint64
	dropsApplied atomic.Uint64
}

type rule struct {
	sourceAVPPath []string
	destAVPPath   []string
	isDrop        bool
}

// Name returns the extension name.
func (e *rtRewrite) Name() string { return "rt_rewrite" }

// Init initializes the extension.
func (e *rtRewrite) Init(ctx extension.InitContext, config map[string]interface{}) error {
	log.Printf("Initializing extension: rt_rewrite")

	e.dict = ctx.GetDictionary()

	// Parse rules from config
	rulesConfig, ok := config["rules"].([]interface{})
	if !ok {
		log.Printf("rt_rewrite: No rules configured, extension will be inactive")
		return nil
	}

	for i, r := range rulesConfig {
		ruleMap, ok := r.(map[string]interface{})
		if !ok {
			log.Printf("rt_rewrite: Invalid rule at index %d, skipping", i)
			continue
		}

		rule := rule{}

		// Parse MAP rule
		if mapData, ok := ruleMap["map"].(map[string]interface{}); ok {
			source, ok := mapData["source"].(string)
			if !ok {
				log.Printf("rt_rewrite: Missing source in MAP rule at index %d", i)
				continue
			}
			dest, ok := mapData["destination"].(string)
			if !ok {
				log.Printf("rt_rewrite: Missing destination in MAP rule at index %d", i)
				continue
			}

			rule.sourceAVPPath = parseAVPPath(source)
			rule.destAVPPath = parseAVPPath(dest)

			// Validate AVPs exist in dictionary
			if !e.validateAVPPath(rule.sourceAVPPath) {
				log.Printf("rt_rewrite: Invalid source AVP path '%s' at index %d", source, i)
				continue
			}
			if !e.validateAVPPath(rule.destAVPPath) {
				log.Printf("rt_rewrite: Invalid destination AVP path '%s' at index %d", dest, i)
				continue
			}

			// Check type compatibility
			if !e.checkTypeCompatibility(rule.sourceAVPPath, rule.destAVPPath) {
				log.Printf("rt_rewrite: Type mismatch between '%s' and '%s' at index %d (continuing anyway)", source, dest, i)
			}

			e.rules = append(e.rules, rule)
			log.Printf("rt_rewrite: Added MAP rule: %s -> %s", source, dest)
		}

		// Parse DROP rule
		if avpName, ok := ruleMap["drop"].(string); ok {
			rule.sourceAVPPath = parseAVPPath(avpName)
			rule.isDrop = true

			if !e.validateAVPPath(rule.sourceAVPPath) {
				log.Printf("rt_rewrite: Invalid AVP path '%s' in DROP rule at index %d", avpName, i)
				continue
			}

			e.rules = append(e.rules, rule)
			log.Printf("rt_rewrite: Added DROP rule: %s", avpName)
		}
	}

	// Register an incoming routing handler to rewrite AVPs
	// Priority 40 (higher than app_redirect's 50)
	ctx.GetRouter().RegisterInHandler("rt_rewrite", 40, e.handleRouting)

	log.Printf("rt_rewrite: Loaded %d rewrite rules", len(e.rules))
	return nil
}

func (e *rtRewrite) handleRouting(_ *peer.Peer, msg *message.Message, candidates []routing.Route) ([]routing.Route, error) {
	// Apply all rewrite rules to the message
	for _, rule := range e.rules {
		if rule.isDrop {
			e.applyDropRule(msg, rule)
		} else {
			e.applyMapRule(msg, rule)
		}
	}

	return candidates, nil
}

// applyDropRule removes the AVP specified in the rule from the message
func (e *rtRewrite) applyDropRule(msg *message.Message, rule rule) {
	if len(rule.sourceAVPPath) == 0 {
		return
	}

	// Find AVP at the specified path
	avp, parent, index := e.findAVPByPath(msg, rule.sourceAVPPath)
	if avp == nil {
		return
	}

	if parent == nil {
		// Top-level AVP - remove from message
		e.removeAVPFromMessage(msg, index)
		e.dropsApplied.Add(1)
		log.Printf("rt_rewrite: Dropped top-level AVP %s", strings.Join(rule.sourceAVPPath, ":"))
	} else {
		// Child AVP - remove from parent
		parent.RemoveChild(index)
		e.dropsApplied.Add(1)
		log.Printf("rt_rewrite: Dropped child AVP %s", strings.Join(rule.sourceAVPPath, ":"))
	}
}

// applyMapRule copies AVP value from source to destination
func (e *rtRewrite) applyMapRule(msg *message.Message, rule rule) {
	if len(rule.sourceAVPPath) == 0 || len(rule.destAVPPath) == 0 {
		return
	}

	// Find source AVP
	sourceAVP, _, _ := e.findAVPByPath(msg, rule.sourceAVPPath)
	if sourceAVP == nil {
		return
	}

	// Ensure destination path exists (create intermediate grouped AVPs if needed)
	destAVP := e.ensureAVPPath(msg, rule.destAVPPath)
	if destAVP == nil {
		return
	}

	// Copy the data from source to destination
	// For grouped AVPs, we need to copy children
	if len(sourceAVP.Children()) > 0 {
		// Source is grouped - copy all children
		for _, child := range sourceAVP.Children() {
			newChild := e.copyAVP(child)
			destAVP.AddChild(newChild)
		}
		e.mapsApplied.Add(1)
		log.Printf("rt_rewrite: Mapped grouped AVP %s -> %s", strings.Join(rule.sourceAVPPath, ":"), strings.Join(rule.destAVPPath, ":"))
	} else {
		// Source is simple - copy data directly
		destAVP.Data = make([]byte, len(sourceAVP.Data))
		copy(destAVP.Data, sourceAVP.Data)
		e.mapsApplied.Add(1)
		log.Printf("rt_rewrite: Mapped AVP %s -> %s", strings.Join(rule.sourceAVPPath, ":"), strings.Join(rule.destAVPPath, ":"))
	}
}

// findAVPByPath finds an AVP at the specified path (e.g., ["Grouped-AVP", "Sub-AVP"])
func (e *rtRewrite) findAVPByPath(msg *message.Message, path []string) (*message.AVP, *message.AVP, int) {
	if len(path) == 0 {
		return nil, nil, 0
	}

	var current *message.AVP
	var parent *message.AVP
	var index int

	// First AVP is in the message
	for i, avp := range msg.AVPs {
		if e.avpMatchesName(avp, path[0]) {
			current = avp
			parent = nil
			index = i
			break
		}
	}

	if current == nil {
		return nil, nil, 0
	}

	// Navigate through child AVPs
	for i := 1; i < len(path); i++ {
		parent = current
		avpCode := e.getAVPCodeByName(path[i])
		if avpCode == 0 {
			return nil, nil, 0
		}
		current = current.FindChild(avpCode, 0)
		if current == nil {
			return nil, nil, 0
		}
		// For child AVPs, we always use index 0 (first match)
		index = 0
	}

	return current, parent, index
}

// ensureAVPPath ensures the AVP path exists, creating intermediate grouped AVPs if needed
func (e *rtRewrite) ensureAVPPath(msg *message.Message, path []string) *message.AVP {
	if len(path) == 0 {
		return nil
	}

	// Find or create the first AVP
	avp := msg.FindAVP(e.getAVPCodeByName(path[0]), 0)
	if avp == nil {
		// Create new AVP
		avpInfo := e.getAVPInfo(path[0])
		if avpInfo == nil {
			return nil
		}

		avp = &message.AVP{
			Code:     avpInfo.Code,
			Flags:    types.AVPFlagMandatory,
			VendorID: avpInfo.VendorID,
		}
		msg.AddAVP(avp)
	}

	// Navigate/create through child AVPs
	for i := 1; i < len(path); i++ {
		avpCode := e.getAVPCodeByName(path[i])
		if avpCode == 0 {
			return nil
		}
		child := avp.FindChild(avpCode, 0)
		if child == nil {
			// Create child AVP
			childInfo := e.getAVPInfo(path[i])
			if childInfo == nil {
				return nil
			}

			child = &message.AVP{
				Code:     childInfo.Code,
				Flags:    types.AVPFlagMandatory,
				VendorID: childInfo.VendorID,
			}
			avp.AddChild(child)
		}
		avp = child
	}

	return avp
}

// removeAVPFromMessage removes an AVP from the message by index
func (e *rtRewrite) removeAVPFromMessage(msg *message.Message, index int) {
	if index < 0 || index >= len(msg.AVPs) {
		return
	}
	// Remove by slicing
	msg.AVPs = append(msg.AVPs[:index], msg.AVPs[index+1:]...)
}

// validateAVPPath checks if all AVPs in the path exist in the dictionary
func (e *rtRewrite) validateAVPPath(path []string) bool {
	for _, avpName := range path {
		if e.getAVPInfo(avpName) == nil {
			return false
		}
	}
	return true
}

// checkTypeCompatibility checks if source and destination AVP types are compatible
func (e *rtRewrite) checkTypeCompatibility(sourcePath, destPath []string) bool {
	sourceInfo := e.getAVPInfo(sourcePath[len(sourcePath)-1])
	destInfo := e.getAVPInfo(destPath[len(destPath)-1])

	if sourceInfo == nil || destInfo == nil {
		return false
	}

	// OctetString can be mapped to any type
	// Other types must match exactly
	if sourceInfo.Type != destInfo.Type && sourceInfo.Type != "OctetString" {
		return false
	}

	return true
}

// getAVPInfo retrieves AVP information from the dictionary
func (e *rtRewrite) getAVPInfo(name string) *avpInfo {
	// Search for AVP in dictionary by name
	avpDef, ok := e.dict.GetAVPByName(name)
	if !ok {
		return nil
	}

	typeName := "OctetString"
	if avpDef.Type != nil {
		typeName = avpDef.Type.Name
	}

	return &avpInfo{
		Code:     avpDef.Code,
		VendorID: avpDef.VendorID,
		Type:     typeName,
	}
}

// getAVPCodeByName converts an AVP name to its code
func (e *rtRewrite) getAVPCodeByName(name string) types.AVPCode {
	info := e.getAVPInfo(name)
	if info == nil {
		return 0
	}
	return info.Code
}

// avpMatchesName checks if an AVP matches the given name
func (e *rtRewrite) avpMatchesName(avp *message.AVP, name string) bool {
	info := e.getAVPInfo(name)
	if info == nil {
		return false
	}
	return avp.Code == info.Code && avp.VendorID == info.VendorID
}

// copyAVP creates a copy of an AVP
func (e *rtRewrite) copyAVP(avp *message.AVP) *message.AVP {
	newAVP := &message.AVP{
		Code:     avp.Code,
		Flags:    avp.Flags,
		VendorID: avp.VendorID,
		Data:     make([]byte, len(avp.Data)),
	}
	copy(newAVP.Data, avp.Data)

	// Copy children if it's a grouped AVP
	for _, child := range avp.Children() {
		newAVP.AddChild(e.copyAVP(child))
	}

	return newAVP
}

// avpInfo stores AVP metadata
type avpInfo struct {
	Code     types.AVPCode
	VendorID types.VendorID
	Type     string
}

// parseAVPPath parses an AVP path string (e.g., "Grouped-AVP:Sub-AVP")
func parseAVPPath(path string) []string {
	return strings.Split(path, ":")
}

// Stop implements the Stoppable interface.
func (e *rtRewrite) Stop() error {
	return nil
}

// Metrics implements extension.MetricsProvider.
func (e *rtRewrite) Metrics() []extension.Metric {
	return []extension.Metric{
		{
			Name:  "maps_applied_total",
			Help:  "Total MAP rewrite rules successfully applied.",
			Type:  extension.MetricCounter,
			Value: float64(e.mapsApplied.Load()),
		},
		{
			Name:  "drops_applied_total",
			Help:  "Total DROP rewrite rules successfully applied.",
			Type:  extension.MetricCounter,
			Value: float64(e.dropsApplied.Load()),
		},
	}
}

func init() {
	extension.Register(&rtRewrite{})
}
