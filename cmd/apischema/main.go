// Command apischema writes the committed golden schema files derived from the
// exporter's response structs (opnsense/schema.go + schema_registry.go) into
// opnsense/testdata/schemas/. The schemas are structure-only (key paths + JSON
// types, no values) and are what the live canary (cmd/apidrift) validates a
// real box's payloads against.
//
// Run via `just schemas` after changing any response struct;
// opnsense.TestSchemasUpToDate fails CI when the committed files are stale.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

func main() {
	out := flag.String("out", "opnsense/testdata/schemas", "directory to write golden schema files into")
	flag.Parse()

	written, removed, err := writeGoldens(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d golden schemas to %s (%d orphans removed)\n", written, *out, removed)
}

// writeGoldens writes every endpoint's golden schema into out and removes
// orphaned JSON files, keeping the committed hand-maintained ledgers.
func writeGoldens(out string) (written, removed int, err error) {
	schemas, err := opnsense.AllEndpointSchemas()
	if err != nil {
		return 0, 0, fmt.Errorf("deriving schemas: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return 0, 0, fmt.Errorf("create %s: %w", out, err)
	}

	// Committed non-golden files in the same directory: the compat ledger, the
	// live-coverage ledger (#377) and the known-blocked ledger. They are
	// hand-maintained, so the orphan sweep below must never delete them.
	valid := map[string]bool{"exemptions.json": true, "coverage.json": true, "known-blocked.json": true}
	for _, s := range schemas {
		name := s.Endpoint + ".json"
		valid[name] = true
		raw, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return 0, 0, fmt.Errorf("marshal %s: %w", s.Endpoint, err)
		}
		raw = append(raw, '\n')
		if err := os.WriteFile(filepath.Join(out, name), raw, 0o644); err != nil {
			return 0, 0, fmt.Errorf("write %s: %w", name, err)
		}
	}

	// Remove orphans from endpoints that no longer exist.
	entries, err := os.ReadDir(out)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", out, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || valid[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(out, e.Name())); err != nil {
			return 0, 0, fmt.Errorf("remove orphan %s: %w", e.Name(), err)
		}
		removed++
	}
	return len(schemas), removed, nil
}
