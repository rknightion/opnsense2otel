package syslog

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/logship"
	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

func TestFactoryUsesValidatedSyslogConfig(t *testing.T) {
	loc := time.FixedZone("sender", 2*60*60)
	cfg := &options.SyslogConfig{UDPAddr: "127.0.0.1:0", MaxConns: 1, LegacyLocation: loc}
	push, err := buildSource(logship.Deps{SyslogConfigResolved: true, SyslogEnabled: true, SyslogConfig: cfg})
	if err != nil || push == nil {
		t.Fatalf("factory with validated config: source=%v err=%v", push, err)
	}
	s := push.(*source)
	t.Cleanup(func() { _ = s.l.Close() })
	var records []logship.Record
	s.emit = func(r logship.Record) { records = append(records, r) }
	now := time.Now().In(loc).Truncate(time.Second)
	line := fmt.Sprintf("<134>%s firewall app: message", now.Format("Jan _2 15:04:05"))
	s.handle([]byte(line), netip.MustParseAddr("192.0.2.1"))
	if len(records) != 1 || !records[0].Timestamp.Equal(now) {
		t.Fatalf("records = %+v; want timestamp %v", records, now)
	}
	if disabled, err := buildSource(logship.Deps{SyslogConfigResolved: true}); err != nil || disabled != nil {
		t.Fatalf("validated disabled source=%v err=%v", disabled, err)
	}
}
