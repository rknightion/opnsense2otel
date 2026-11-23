package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The orphan sweep must keep every hand-maintained ledger and still remove a
// real orphan: losing one of these files silently loses reviewed history.
func TestWriteGoldensKeepsTheCommittedLedgers(t *testing.T) {
	dir := t.TempDir()
	ledgers := []string{"exemptions.json", "coverage.json", "known-blocked.json"}
	for _, name := range append(ledgers, "retiredEndpoint.json") {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	written, removed, err := writeGoldens(dir)
	if err != nil {
		t.Fatal(err)
	}
	if written == 0 || removed != 1 {
		t.Errorf("written = %d, removed = %d, want some written and exactly the one orphan removed", written, removed)
	}
	for _, name := range ledgers {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s was removed: %v", name, err)
		} else if string(raw) != "{}\n" {
			t.Errorf("%s was rewritten", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "retiredEndpoint.json")); !os.IsNotExist(err) {
		t.Errorf("the orphan was not removed: %v", err)
	}
}
