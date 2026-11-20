package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/flow"
	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

const pfStateRefreshMaxInterval = 15 * time.Minute

func nextPFStateRefreshInterval(last time.Duration) time.Duration {
	// Compare before multiplication to avoid overflowing time.Duration.
	if last >= pfStateRefreshMaxInterval/10 {
		return pfStateRefreshMaxInterval
	}
	if last <= pfStateRefreshInterval/10 {
		return pfStateRefreshInterval
	}
	return 10 * last
}

type pfStateRefresher struct {
	fetch       func() (opnsense.FirewallStates, *opnsense.APICallError)
	repairer    *flow.Repairer
	logger      *slog.Logger
	previous    *flow.RouteTable
	previousNAT *flow.NATTable
}

func (r *pfStateRefresher) refreshOnce() {
	states, ferr := r.fetch()
	if ferr != nil {
		r.logger.Warn("pf state fetch failed; policy-route repair is resolving against the previous snapshot", "err", ferr.Error())
		return
	}
	rows := make([]flow.StateRow, 0, len(states.States))
	for _, st := range states.States {
		rows = append(rows, flow.StateRow{
			Proto: st.Proto, Direction: st.Direction,
			SrcAddr: st.SrcAddr, SrcPort: st.SrcPort,
			DstAddr: st.DstAddr, DstPort: st.DstPort,
			RouteToDevice: st.RouteToDevice, NatAddr: st.NatAddr, NatPort: st.NatPort,
		})
	}
	built := time.Now()
	r.previous = flow.BuildRouteTable(flow.RouteTableInput{Rows: rows, Built: built, Previous: r.previous, Retain: pfStateRetention})
	r.repairer.SetRouteTable(r.previous)
	r.previousNAT = flow.BuildNATTable(flow.NATTableInput{Rows: rows, Built: built, Previous: r.previousNAT, Retain: pfStateRetention})
	r.repairer.SetNATTable(r.previousNAT)
}

func (r *pfStateRefresher) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		r.refreshOnce()
		timer := time.NewTimer(nextPFStateRefreshInterval(time.Since(start)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
