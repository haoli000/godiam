// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package test_app provides a test Diameter application extension for integration testing.
package test_app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/core/peer"
	"github.com/haoli000/godiam/pkg/proto/message"
	"github.com/haoli000/godiam/pkg/proto/types"
)

type appTest struct {
	appID       types.ApplicationID
	sendOnStart bool
	destRealm   string
	destHost    string
	count       int
	cancel      context.CancelFunc
}

// Name returns the extension name.
func (e *appTest) Name() string { return "test_app" }

// Init initializes the extension.
func (e *appTest) Init(ctx extension.InitContext, config map[string]interface{}) error {
	e.appID = types.ApplicationID(9999)
	if v, ok := config["app_id"].(float64); ok {
		e.appID = types.ApplicationID(uint32(v)) //nolint:gosec // G115: value range is protocol-constrained
	} else if v, ok := config["app_id"].(int); ok {
		e.appID = types.ApplicationID(uint32(v)) //nolint:gosec // G115: value range is protocol-constrained
	}
	if v, ok := config["send_on_start"].(bool); ok {
		e.sendOnStart = v
	}
	if v, ok := config["dest_realm"].(string); ok {
		e.destRealm = v
	}
	if v, ok := config["dest_host"].(string); ok {
		e.destHost = v
	}
	e.count = 1
	if v, ok := config["count"].(float64); ok {
		e.count = int(v)
	} else if v, ok := config["count"].(int); ok {
		e.count = v
	}

	infof("Initializing extension: test_app app_id=%d send_on_start=%t dest_realm=%s dest_host=%s count=%d", e.appID, e.sendOnStart, e.destRealm, e.destHost, e.count)

	ctx.GetRouter().RegisterDispatchHandler(e.appID, e.handleDispatch)

	if e.sendOnStart {
		var bgCtx context.Context
		bgCtx, e.cancel = context.WithCancel(context.Background())
		go e.sendInitial(bgCtx, ctx)
	}

	return nil
}

func (e *appTest) handleDispatch(p *peer.Peer, msg *message.Message) error {
	if !msg.IsRequest() {
		orh, _ := msg.GetOriginHost()
		orr, _ := msg.GetOriginRealm()
		res, ok := msg.GetResultCode()
		if ok {
			infof("test_app answer: code=%d app=%d hbh=%d e2e=%d from=%s/%s avps=%d rr=[%s]", msg.CommandCode, msg.ApplicationID, msg.HopByHopID, msg.EndToEndID, orh, orr, len(msg.AVPs), routeRecords(msg))
			infof("test_app result: %d", res)
		} else {
			infof("test_app answer: code=%d app=%d hbh=%d e2e=%d from=%s/%s avps=%d rr=[%s]", msg.CommandCode, msg.ApplicationID, msg.HopByHopID, msg.EndToEndID, orh, orr, len(msg.AVPs), routeRecords(msg))
		}
		return nil
	}

	orh, _ := msg.GetOriginHost()
	orr, _ := msg.GetOriginRealm()
	drh, _ := msg.GetDestinationHost()
	drr, _ := msg.GetDestinationRealm()
	infof("test_app request: code=%d app=%d hbh=%d e2e=%d from=%s/%s to=%s/%s avps=%d rr=[%s]", msg.CommandCode, msg.ApplicationID, msg.HopByHopID, msg.EndToEndID, orh, orr, drh, drr, len(msg.AVPs), routeRecords(msg))

	ans := message.NewAnswer(msg)
	ans.SetResultCode(types.ResultSuccess)

	if sessionID, ok := msg.GetSessionID(); ok {
		ans.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
	}

	localCfg := peer.GetLocalConfig()
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, localCfg.DiameterIdentity))
	ans.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, localCfg.Realm))
	ans.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(e.appID)))

	// Copy Proxy-Info AVPs from request to answer (RFC6733 requirement)
	for _, avp := range msg.AVPs {
		if avp.Code == types.AVPCodeProxyInfo {
			ans.AddAVP(avp)
		}
	}

	if err := p.Send(ans); err != nil {
		infof("test_app send answer failed: %v", err)
		return err
	}
	infof("test_app answer sent: code=%d app=%d hbh=%d e2e=%d to=%s avps=%d", ans.CommandCode, ans.ApplicationID, ans.HopByHopID, ans.EndToEndID, p.DiameterID(), len(ans.AVPs))

	return nil
}

func (e *appTest) sendInitial(bgCtx context.Context, ctx extension.InitContext) {
	select {
	case <-time.After(2 * time.Second):
	case <-bgCtx.Done():
		return
	}

	local := peer.GetLocalConfig()
	sessionID := fmt.Sprintf("testapp-%s-%d", local.DiameterIdentity, time.Now().UnixNano())
	for i := 0; i < e.count; i++ {
		select {
		case <-bgCtx.Done():
			return
		default:
		}
		msg := message.NewRequest(types.CmdCodeCreditControl, e.appID)
		msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionID, types.AVPFlagMandatory, sessionID))
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, local.DiameterIdentity))
		msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, local.Realm))
		if e.destRealm != "" {
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationRealm, types.AVPFlagMandatory, types.DiamID(e.destRealm)))
		}
		if e.destHost != "" {
			msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeDestinationHost, types.AVPFlagMandatory, types.DiamID(e.destHost)))
		}
		msg.AddAVP(message.NewUnsigned32AVP(types.AVPCodeAuthApplicationID, types.AVPFlagMandatory, uint32(e.appID)))

		nextHop, err := ctx.GetRouter().RouteOut(msg)
		if err != nil {
			infof("test_app route out failed: %v", err)
			continue
		}

		obj, ok := ctx.GetRouter().LookupPeer(nextHop)
		if !ok {
			infof("test_app next hop not found: %s", nextHop)
			continue
		}
		p, ok := obj.(*peer.Peer)
		if !ok {
			infof("test_app invalid peer object")
			continue
		}

		msg.HopByHopID = p.NextHopByHopID()
		msg.EndToEndID = types.EndToEndID(time.Now().UnixNano()) //nolint:gosec // G115: value range is protocol-constrained

		orh, _ := msg.GetOriginHost()
		orr, _ := msg.GetOriginRealm()
		drh, _ := msg.GetDestinationHost()
		drr, _ := msg.GetDestinationRealm()
		infof("test_app send request: code=%d app=%d hbh=%d e2e=%d from=%s/%s to=%s/%s next=%s avps=%d", msg.CommandCode, msg.ApplicationID, msg.HopByHopID, msg.EndToEndID, orh, orr, drh, drr, nextHop, len(msg.AVPs))

		if err := p.Send(msg); err != nil {
			infof("test_app send failed: %v", err)
			continue
		}
	}
}

func routeRecords(msg *message.Message) string {
	var rr []string
	for _, a := range msg.AVPs {
		if a.Code == types.AVPCodeRouteRecord {
			rr = append(rr, string(a.GetDiameterIdentity()))
		}
	}
	return strings.Join(rr, ",")
}

// Stop implements the Stoppable interface. It cancels any in-flight sendInitial goroutine.
func (e *appTest) Stop() error {
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}

func init() {
	extension.Register(&appTest{})
}

func infof(format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	_, _ = fmt.Fprintf(os.Stdout, "%s | %s\n", ts, fmt.Sprintf(format, args...))
}
