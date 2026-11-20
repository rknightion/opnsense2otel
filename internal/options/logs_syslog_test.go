package options

import (
	"strings"
	"testing"
)

func TestLogsSyslogLegacyTimezone(t *testing.T) {
	oldEnabled, oldZone := *logsSyslogEnabled, *logsSyslogLegacyTimezone
	oldUDP, oldMax := *logsSyslogListenUDP, *logsSyslogMaxConns
	t.Cleanup(func() {
		*logsSyslogEnabled, *logsSyslogLegacyTimezone, *logsSyslogListenUDP, *logsSyslogMaxConns = oldEnabled, oldZone, oldUDP, oldMax
	})
	*logsSyslogEnabled = true
	*logsSyslogListenUDP = ":5514"
	*logsSyslogMaxConns = 64
	*logsSyslogLegacyTimezone = ""
	cfg, enabled, err := LogsSyslog()
	if err != nil || !enabled || cfg.LegacyLocation != nil {
		t.Fatalf("empty timezone: cfg=%v enabled=%v err=%v", cfg, enabled, err)
	}
	*logsSyslogLegacyTimezone = "Invalid/NoSuchZone"
	_, _, err = LogsSyslog()
	if err == nil || !strings.Contains(err.Error(), "legacy-timezone") {
		t.Fatalf("invalid timezone error = %v", err)
	}
}
