package opnsense

import (
	"io"
	"net/http"
	"testing"
)

func TestFetchFirewallStates(t *testing.T) {
	server, mux, client := newTestClientWithMux(t)
	defer server.Close()

	mux.HandleFunc("/api/diagnostics/firewall/query_states", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		const wantBody = `{"current":1,"rowCount":-1,"searchPhrase":""}`
		if string(body) != fetchFirewallStatesPayload {
			t.Errorf("body = %q, want payload %q", body, fetchFirewallStatesPayload)
		}
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", body, wantBody)
		}
		_, _ = io.WriteString(w, `{"total":5,"rowCount":2,"current":1,"rows":[
			{"proto":"tcp","direction":"out","src_addr":"192.0.2.10","src_port":"1234","dst_addr":"198.51.100.20","dst_port":"443","route-to":"203.0.113.1@example0","nat_addr":"192.0.2.11","nat_port":"4321"},
			{"proto":"udp","direction":"in","src_addr":"198.51.100.30","src_port":"53","dst_addr":"192.0.2.40","dst_port":"5678","nat_addr":null,"nat_port":null}
		]}`)
	})

	got, err := client.FetchFirewallStates()
	if err != nil {
		t.Fatalf("FetchFirewallStates: %v", err)
	}
	if got.Total != 5 {
		t.Errorf("Total = %d, want 5", got.Total)
	}
	want := []FirewallState{
		{Proto: "tcp", Direction: "out", SrcAddr: "192.0.2.10", SrcPort: "1234", DstAddr: "198.51.100.20", DstPort: "443", RouteToGateway: "203.0.113.1", RouteToDevice: "example0", NatAddr: "192.0.2.11", NatPort: "4321"},
		{Proto: "udp", Direction: "in", SrcAddr: "198.51.100.30", SrcPort: "53", DstAddr: "192.0.2.40", DstPort: "5678", RouteToGateway: "", RouteToDevice: "", NatAddr: "", NatPort: ""},
	}
	if len(got.States) != len(want) {
		t.Fatalf("got %d states, want %d", len(got.States), len(want))
	}
	for i := range want {
		if got.States[i] != want[i] {
			t.Errorf("state %d = %+v, want %+v", i, got.States[i], want[i])
		}
	}
}

func TestFetchFirewallStatesSplitRouteTo(t *testing.T) {
	tests := []struct {
		name, input, gateway, device string
	}{
		{"gateway and device", "203.0.113.1@example0", "203.0.113.1", "example0"},
		{"no at sign", "203.0.113.1", "", ""},
		{"empty device", "203.0.113.1@", "", ""},
		{"empty gateway", "@example0", "", "example0"},
		{"empty", "", "", ""},
		{"last at sign", "part@203.0.113.1@example0", "part@203.0.113.1", "example0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, device := splitRouteTo(tt.input)
			if gateway != tt.gateway || device != tt.device {
				t.Errorf("splitRouteTo(%q) = %q, %q; want %q, %q", tt.input, gateway, device, tt.gateway, tt.device)
			}
		})
	}
}
