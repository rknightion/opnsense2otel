package main

import "testing"

func TestDeriveGeneration(t *testing.T) {
	cases := []struct {
		name      string
		productID string
		version   string
		want      string
	}{
		{
			name:      "the release box names the release channel",
			productID: "opnsense",
			version:   "26.7.1_1",
			want:      "release 26.7.1_1",
		},
		{
			// The whole point of the stamp: a devel snapshot must be
			// distinguishable from a release at a glance, and the snapshot
			// suffix must survive verbatim rather than being parsed down to a
			// release number.
			name:      "the devel box names the devel channel and keeps the snapshot suffix",
			productID: "opnsense-devel",
			version:   "27.1.a_40",
			want:      "devel 27.1.a_40",
		},
		{
			// Never invent a channel we did not read. A product id we do not
			// recognise is printed as-is, so the report says what the box said.
			name:      "an unrecognised product id is carried verbatim",
			productID: "opnsense-business",
			version:   "26.7",
			want:      "opnsense-business 26.7",
		},
		{
			name:      "a missing product id degrades to the bare version",
			productID: "",
			version:   "26.7.1_1",
			want:      "26.7.1_1",
		},
		{
			// A version we could not read teaches us nothing, so it must not
			// render as a normal heading - that is the failure this whole
			// stamp exists to prevent.
			name:      "no version at all is unknown, whatever the product id says",
			productID: "opnsense",
			version:   "",
			want:      unknownGeneration,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveGeneration(tc.productID, tc.version); got != tc.want {
				t.Errorf("deriveGeneration(%q, %q) = %q, want %q", tc.productID, tc.version, got, tc.want)
			}
		})
	}
}

// The identity fields live under product.* unconditionally, while the top-level
// copies are absent on a box that has never run a firmware check (#640). Mirror
// opnsense/firmware.go's top-level-wins-else-nested resolution, or the two
// disagree on the same payload.
func TestFirmwareStatusResolution(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantID      string
		wantVersion string
	}{
		{
			name:        "nested identity is used when the top level is absent",
			body:        `{"product":{"product_id":"opnsense-devel","product_version":"27.1.a_40"},"status":"ok"}`,
			wantID:      "opnsense-devel",
			wantVersion: "27.1.a_40",
		},
		{
			name:        "the top level wins when both are present",
			body:        `{"product_id":"opnsense","product_version":"26.7.1_1","product":{"product_id":"stale","product_version":"0.0"}}`,
			wantID:      "opnsense",
			wantVersion: "26.7.1_1",
		},
		{
			name:        "an empty top-level field falls through to the nested copy",
			body:        `{"product_id":"","product_version":"","product":{"product_id":"opnsense","product_version":"26.7.1_1"}}`,
			wantID:      "opnsense",
			wantVersion: "26.7.1_1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, version, err := parseFirmwareIdentity([]byte(tc.body))
			if err != nil {
				t.Fatalf("parseFirmwareIdentity: %v", err)
			}
			if id != tc.wantID || version != tc.wantVersion {
				t.Errorf("parseFirmwareIdentity = (%q, %q), want (%q, %q)", id, version, tc.wantID, tc.wantVersion)
			}
		})
	}
}

func TestParseFirmwareIdentityRejectsGarbage(t *testing.T) {
	if _, _, err := parseFirmwareIdentity([]byte("this is not json")); err == nil {
		t.Error("parseFirmwareIdentity accepted a non-JSON body; an unreadable status must not render as a version")
	}
}
