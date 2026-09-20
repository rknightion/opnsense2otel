package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// unknownGeneration is what the heading carries when the box could not be asked
// what it is running. It is deliberately loud: the failure this stamp exists to
// prevent is a clean report from a stale box reading exactly like a clean report
// from a current one, and an unlabelled heading is precisely that (OPN-0107).
const unknownGeneration = "VERSION UNKNOWN (firmware status unreadable)"

// firmwareChannels maps the package identity OPNsense reports to the channel
// word used in the report heading. Anything absent is printed verbatim rather
// than guessed at - a report must never name a channel nobody read off a box.
var firmwareChannels = map[string]string{
	"opnsense":       "release",
	"opnsense-devel": "devel",
}

// deriveGeneration builds the heading label from the box's own product identity,
// e.g. "devel 27.1.a_40" or "release 26.7.1_1".
//
// The version is carried through untouched, snapshot suffix and all. Parsing it
// down to a release number would throw away the only thing that distinguishes
// one devel snapshot from the next, which is the difference the nightly profile
// exists to watch.
func deriveGeneration(productID, productVersion string) string {
	productVersion = strings.TrimSpace(productVersion)
	if productVersion == "" {
		return unknownGeneration
	}
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return productVersion
	}
	if channel, ok := firmwareChannels[productID]; ok {
		return channel + " " + productVersion
	}
	return productID + " " + productVersion
}

// parseFirmwareIdentity reads the product id and version out of a
// core/firmware/status body.
//
// Resolution is top-level-wins-else-nested, mirroring
// opnsense/firmware.go's productID()/productVersion(). The nested copy is the
// one that matters in practice: on a box that has never run a firmware check the
// response carries only product/status/status_msg, with no top-level identity at
// all (#640), and that is exactly the state a freshly booted lab box is in.
func parseFirmwareIdentity(body []byte) (productID, productVersion string, err error) {
	var status struct {
		ProductID      string `json:"product_id"`
		ProductVersion string `json:"product_version"`
		Product        struct {
			ProductID      string `json:"product_id"`
			ProductVersion string `json:"product_version"`
		} `json:"product"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return "", "", fmt.Errorf("decoding core/firmware/status: %w", err)
	}
	productID = status.ProductID
	if productID == "" {
		productID = status.Product.ProductID
	}
	productVersion = status.ProductVersion
	if productVersion == "" {
		productVersion = status.Product.ProductVersion
	}
	return productID, productVersion, nil
}

// fetchGeneration asks the box what it is running. It never fails the run: a
// canary that refused to probe because it could not read a version would turn a
// cosmetic gap into an outage, so an unreadable status degrades to a loud label
// and the reason goes to stderr for the workflow log.
func (p *prober) fetchGeneration() string {
	body, code, err := p.fetchRaw("GET", "api/core/firmware/status", "", "")
	if err != nil {
		p.note("apidrift: could not read core/firmware/status: %v", err)
		return unknownGeneration
	}
	if code != 200 {
		p.note("apidrift: core/firmware/status returned HTTP %d", code)
		return unknownGeneration
	}
	productID, productVersion, err := parseFirmwareIdentity(body)
	if err != nil {
		p.note("apidrift: %v", err)
		return unknownGeneration
	}
	return deriveGeneration(productID, productVersion)
}
