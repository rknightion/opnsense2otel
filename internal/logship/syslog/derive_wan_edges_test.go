package syslog

import (
	"github.com/rknightion/opnsense2otel/v5/internal/logship"
	"github.com/rknightion/opnsense2otel/v5/internal/options"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func (f *fakeSink) ObservePPPLink(iface, event string, timestamp float64) bool {
	f.calls = append(f.calls, fakeCall{"ppp", []string{iface, event, strconv.FormatFloat(timestamp, 'f', -1, 64)}})
	return true
}
func (f *fakeSink) ObserveGatewayStatesKilled(gateway string) bool {
	f.calls = append(f.calls, fakeCall{"gateway_states_killed", []string{gateway}})
	return true
}

func TestDerivePPPCapturedFlap(t *testing.T) {
	// Sanitized operational shapes from the parser's captured mpd5 flap.
	lines := []string{
		"[opt7_link0] Link: DOWN event", "[opt7_link0] LCP: state change Opened --> Stopping",
		"[opt7] IPCP: state change Opened --> Stopping", "[opt7] IPV6CP: state change Opened --> Stopping",
		"[opt7] IFACE: Down event", "ppp-linkdown: executing on pppoe0 for inet", "ppp-linkdown: executing on pppoe0 for inet6",
		"[opt7_link0] PPPoE: connection closed", "[opt7_link0] Link: UP event",
		"[opt7_link0] LCP: state change Ack-Sent --> Opened", "[opt7] IPCP: state change Ack-Sent --> Opened",
		"[opt7] IFACE: Up event", "ppp-linkup: executing on pppoe0 for inet", "ppp-linkup: executing on pppoe0 for inet6",
	}
	sink := &fakeSink{}
	source := newSource(&options.SyslogConfig{}, logship.Deps{MetricSink: sink})
	source.emit = func(logship.Record) {}
	for _, line := range lines {
		env := pppEnv(t, line)
		rec, ok := parsePPP(env, nil, nil)
		if !ok {
			t.Fatalf("captured line did not parse: %s", line)
		}
		_ = rec
		source.handle([]byte("<134>1 2026-07-27T09:12:03Z test-firewall ppp 39598 - - "+line), netip.MustParseAddr("192.0.2.1"))
	}
	if len(sink.calls) != 2 {
		t.Fatalf("one flap produced %d observations: %v, want exactly down and up", len(sink.calls), sink.calls)
	}
	for i, event := range []string{"link_down", "link_up"} {
		got := sink.calls[i].args
		if got[0] != "opt7" || got[1] != event || got[2] != "1785143523" {
			t.Errorf("edge %d = %v", i, got)
		}
	}
}

func TestDeriveGatewayStateKillCapturedOnly(t *testing.T) {
	sink := &fakeSink{}
	source := newSource(&options.SyslogConfig{}, logship.Deps{MetricSink: sink})
	source.emit = func(logship.Record) {}
	for _, msg := range []string{
		"/usr/local/etc/rc.filter_configure: ROUTING: killing states for unreachable gateway WAN_TEST [12345678-1234-1234-1234-123456789abc]",
		"/usr/local/etc/rc.filter_configure: ROUTING: configuring monitor for WAN_TEST",
	} {
		env := Envelope{Program: "opnsense", Message: msg, Timestamp: time.Unix(1785143523, 0)}
		rec, _ := parseCapturedRecord(env, nil)
		_ = rec
		source.handle([]byte("<134>1 2026-07-27T09:12:03Z test-firewall opnsense 123 - - "+msg), netip.MustParseAddr("192.0.2.1"))
	}
	if len(sink.calls) != 1 || sink.calls[0].method != "gateway_states_killed" || sink.calls[0].args[0] != "WAN_TEST" {
		t.Fatalf("state-kill observations = %v", sink.calls)
	}
}
