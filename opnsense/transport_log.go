package opnsense

import (
	"bytes"
	"io"
	"log"
	"sync"
)

// unsolicitedResponseLog is the start of the one net/http transport log line
// that quotes received bytes at default settings. The transport writes it to
// the process-wide standard logger when a peer sends data on an idle
// connection, and offers no per-client logger or option to turn it off.
var unsolicitedResponseLog = []byte("Unsolicited response received on idle HTTP channel")

var transportLogGuardMu sync.Mutex

// transportLogGuard forwards standard-logger output, cutting the unsolicited
// response line down to the fact that it happened. An upstream reply can hold
// voucher codes, and the line's quoted bytes cannot be sanitized at the call
// site. Filtering the writer keeps every other log line and leaves connection
// reuse alone; disabling keep-alives would not close this path, because a peer
// can send before the first request is written.
type transportLogGuard struct {
	next io.Writer
}

// Write relies on the standard logger making one Write per log entry.
func (g *transportLogGuard) Write(p []byte) (int, error) {
	i := bytes.Index(p, unsolicitedResponseLog)
	if i < 0 {
		return g.next.Write(p)
	}
	line := make([]byte, 0, i+len(unsolicitedResponseLog)+len(" (content withheld)\n"))
	line = append(line, p[:i]...)
	line = append(line, unsolicitedResponseLog...)
	line = append(line, " (content withheld)\n"...)
	if _, err := g.next.Write(line); err != nil {
		return 0, err
	}
	return len(p), nil
}

// installTransportLogGuard wraps the standard logger's current output once.
func installTransportLogGuard() {
	transportLogGuardMu.Lock()
	defer transportLogGuardMu.Unlock()
	current := log.Writer()
	if _, ok := current.(*transportLogGuard); ok {
		return
	}
	log.SetOutput(&transportLogGuard{next: current})
}
