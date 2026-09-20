package opnsense

import (
	"net/http"
	"testing"
)

func TestFetchNDPTable_Success(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Write([]byte(`[
			{
				"mac": "00:11:22:33:44:55",
				"ip": "fe80::1",
				"intf": "igb0",
				"intf_description": "LAN",
				"manufacturer": "Vendor Name",
				"expire": "23h59m50s",
				"type": "dynamic"
			},
			{
				"mac": "aa:bb:cc:dd:ee:ff",
				"ip": "2001:db8::1",
				"intf": "igb1",
				"intf_description": "WAN",
				"manufacturer": "Other Vendor",
				"expire": "permanent",
				"type": "static"
			}
		]`))
	})
	defer server.Close()

	data, err := client.FetchNDPTable()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.TotalEntries != 2 {
		t.Errorf("expected TotalEntries=2, got %d", data.TotalEntries)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("expected 2 NDP entries, got %d", len(data.Entries))
	}

	e1 := data.Entries[0]
	if e1.Mac != "00:11:22:33:44:55" {
		t.Errorf("expected mac '00:11:22:33:44:55', got %q", e1.Mac)
	}
	if e1.IP != "fe80::1" {
		t.Errorf("expected ip 'fe80::1', got %q", e1.IP)
	}
	if e1.IntfDescription != "LAN" {
		t.Errorf("expected IntfDescription 'LAN', got %q", e1.IntfDescription)
	}
	e2 := data.Entries[1]
	if e2.Mac != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("expected mac 'aa:bb:cc:dd:ee:ff', got %q", e2.Mac)
	}
	if e2.IP != "2001:db8::1" {
		t.Errorf("expected ip '2001:db8::1', got %q", e2.IP)
	}
	if e2.IntfDescription != "WAN" {
		t.Errorf("expected IntfDescription 'WAN', got %q", e2.IntfDescription)
	}
}

func TestFetchNDPTable_EmptyArray(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	defer server.Close()

	data, err := client.FetchNDPTable()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.TotalEntries != 0 {
		t.Errorf("expected TotalEntries=0, got %d", data.TotalEntries)
	}
	if len(data.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(data.Entries))
	}
}

func TestFetchNDPTable_ServerError(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	defer server.Close()

	_, err := client.FetchNDPTable()
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if err.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", err.StatusCode)
	}
}

// liveNDPFixture is trimmed verbatim from a live box
// (api/diagnostics/interface/get_ndp). The payload carries NO `type` and NO
// `expire` key — only mac, ip, intf, intf_description and manufacturer —
// re-confirmed 2026-09-20 against both testbed boxes by running the producer,
// /usr/local/opnsense/scripts/interfaces/list_ndp.py, directly. Neither key is
// modelled any more (OPN-0113), so this fixture is now the whole shape rather
// than a deliberate subset of it.
const liveNDPFixture = `[
 {"mac":"0e:40:69:ec:4d:9a","ip":"fe80::d6:761:6510:f3a6%ixl0","intf":"ixl0","manufacturer":"","intf_description":"LAN"},
 {"mac":"98:b7:85:21:af:f2","ip":"2001:db8::1","intf":"ixl0_vlan100","manufacturer":"Intel Corporate","intf_description":"MGMT"}
]`

func TestFetchNDPTable_CarriesManufacturerAndDevice(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(liveNDPFixture))
	})
	defer server.Close()

	table, err := client.FetchNDPTable()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(table.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(table.Entries))
	}

	byIP := make(map[string]NDPEntry, len(table.Entries))
	for _, e := range table.Entries {
		byIP[e.IP] = e
	}
	if got := byIP["2001:db8::1"].Manufacturer; got != "Intel Corporate" {
		t.Errorf("manufacturer = %q, want %q", got, "Intel Corporate")
	}
	if got := byIP["2001:db8::1"].Device; got != "ixl0_vlan100" {
		t.Errorf("device = %q, want %q", got, "ixl0_vlan100")
	}
	if got := byIP["fe80::d6:761:6510:f3a6%ixl0"].Manufacturer; got != "" {
		t.Errorf("unresolved manufacturer = %q, want empty", got)
	}
}
