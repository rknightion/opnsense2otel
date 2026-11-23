package opnsense

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/rknightion/opnsense2otel/v5/internal/redact"
)

// ConfigSnapshotEntity is a current, upstream-produced configuration row
// projected from the firewall search endpoints. Config never includes a field
// whose name marks it as a credential or private key.
type ConfigSnapshotEntity struct {
	Kind   string
	ID     string
	Config map[string]any
}

type configSnapshotSearchResponse struct {
	Rows []json.RawMessage `json:"rows"`
}

// FilterController::searchRuleAction merges MVC rows with the non-MVC rows
// from scripts/filter/list_non_mvc_rules.php. On stable/26.7 (6fc5e865) and
// stable/26.1 (8cc69b21), that producer maps a generated PF label to uuid but
// emits every rule with its own sort_order. A repeated generated-rule UUID is
// therefore valid, and this delimiter keeps its producer discriminator in the
// internal entity identity without changing the source row.
const configSnapshotDuplicateIDDelimiter = ":"

// FetchFirewallConfigSnapshots returns the current filter and four NAT rule
// sets. It intentionally reuses the already-registered search endpoints: no
// endpoint registry or schema-contract expansion is required. The row bodies
// are preserved as JSON objects so this config snapshot follows fields OPNsense
// actually produced rather than a guessed local struct.
func (c *Client) FetchFirewallConfigSnapshots() ([]ConfigSnapshotEntity, *APICallError) {
	type endpoint struct {
		kind   string
		name   EndpointName
		method string
		body   string
	}
	endpoints := []endpoint{
		{kind: "filter_rule", name: "firewallRules", method: "POST", body: fetchFirewallRulesPayload},
		{kind: "source_nat", name: "natSourceNATRules", method: "GET"},
		{kind: "d_nat", name: "natDNATRules", method: "GET"},
		{kind: "one_to_one", name: "natOneToOneRules", method: "GET"},
		{kind: "npt", name: "natNPTRules", method: "GET"},
	}

	var entities []ConfigSnapshotEntity
	for _, endpoint := range endpoints {
		path, ok := c.endpoints[endpoint.name]
		if !ok {
			return nil, &APICallError{Endpoint: string(endpoint.name), Message: "endpoint not found in client endpoints"}
		}
		var response configSnapshotSearchResponse
		var body io.Reader
		if endpoint.body != "" {
			body = strings.NewReader(endpoint.body)
		}
		if err := c.do(endpoint.method, path, body, &response); err != nil {
			return nil, err
		}
		rows, err := configSnapshotRows(endpoint.kind, response.Rows)
		if err != nil {
			return nil, &APICallError{Endpoint: string(endpoint.name), Message: "failed to decode " + endpoint.kind + " configuration rows: " + decodeFailureReason(err)}
		}
		entities = append(entities, rows...)
	}
	return entities, nil
}

func configSnapshotRows(kind string, rows []json.RawMessage) ([]ConfigSnapshotEntity, error) {
	type decodedRow struct {
		id     string
		config map[string]any
	}
	decoded := make([]decodedRow, 0, len(rows))
	uuidCounts := make(map[string]int, len(rows))
	for _, row := range rows {
		decoder := json.NewDecoder(bytes.NewReader(row))
		decoder.UseNumber()
		var config map[string]any
		if err := decoder.Decode(&config); err != nil {
			return nil, fmt.Errorf("decode %s configuration row: %w", kind, err)
		}
		id, ok := config["uuid"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("%s configuration row has no uuid", kind)
		}
		redactConfigSnapshotFields(config)
		decoded = append(decoded, decodedRow{id: id, config: config})
		uuidCounts[id]++
	}

	entities := make([]ConfigSnapshotEntity, 0, len(decoded))
	for _, row := range decoded {
		id := row.id
		if kind == "filter_rule" && uuidCounts[row.id] > 1 {
			sortOrder, ok := row.config["sort_order"].(string)
			if !ok || strings.TrimSpace(sortOrder) == "" {
				return nil, fmt.Errorf("%s configuration row with repeated uuid %q has no nonempty string sort_order", kind, row.id)
			}
			id += configSnapshotDuplicateIDDelimiter + sortOrder
		}
		entities = append(entities, ConfigSnapshotEntity{Kind: kind, ID: id, Config: row.config})
	}
	return entities, nil
}

func redactConfigSnapshotFields(value any) {
	switch value := value.(type) {
	case map[string]any:
		redact.ConfigPropertyValue(value)
		for key, child := range value {
			if SensitiveConfigKey(key) {
				delete(value, key)
				continue
			}
			redactConfigSnapshotFields(child)
		}
	case []any:
		for _, child := range value {
			redactConfigSnapshotFields(child)
		}
	}
}

// SensitiveConfigKey reports whether a configuration key or XML element name
// names a credential that must never leave the firewall in a log record.
//
// The comparison is made on a separator-free lowercase form so snake_case,
// kebab-case, dotted and camelCase wire keys all resolve to the same
// vocabulary. Over-matching here is cheap; under-matching ships a credential.
func SensitiveConfigKey(key string) bool {
	return redact.SensitiveKey(key)
}
