package opnsense

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// captivePortalZoneMap decodes the api/captiveportal/session/zones response.
// zonesAction builds $response[(string)$zone->zoneid] = $zone->description, and
// PHP's json_encode re-integerizes numeric string keys, so a zone set keyed
// 0,1,2,… (the default for the first configured zone(s)) is serialized as a JSON
// *array* (e.g. ["Guest WiFi"]) rather than an object — while a genuinely empty
// map is []. A plain flexStringMap treats any array as empty and would silently
// drop real zones (#73), so this type reconstructs {index: element} for a
// non-empty array and preserves the empty-array→empty-map quirk.
type captivePortalZoneMap map[string]string

// UnmarshalJSON implements json.Unmarshaler.
func (z *captivePortalZoneMap) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*z = make(captivePortalZoneMap)
		return nil
	}
	if trimmed[0] == '[' {
		var arr []string
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return fmt.Errorf("captivePortalZoneMap array: %w", err)
		}
		m := make(captivePortalZoneMap, len(arr))
		for i, desc := range arr {
			m[strconv.Itoa(i)] = desc
		}
		*z = m
		return nil
	}
	m := make(map[string]string)
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return fmt.Errorf("captivePortalZoneMap object: %w", err)
	}
	*z = m
	return nil
}

// captivePortalSessionRow mirrors one row from api/captiveportal/session/search.
// Only zoneid is decoded; PII fields (userName, macAddress, ipAddress) are
// deliberately not modeled per D10 (no per-session detail).
//
// VERIFICATION: core endpoint — validate shapes against the live OPNsense box
// via ~/repos/chat/opnsense/CLAUDE.md (read-only GET). The unconfigured shape
// (zones returns [] and sessions returns {total:0,rows:[]}) was verified live.
// The populated fixture is derived from CaptivePortal/Api/SessionController.php.
type captivePortalSessionRow struct {
	ZoneID flexString `json:"zoneid"`
}

// captivePortalSessionSearch is the bootgrid response from session/search.
type captivePortalSessionSearch struct {
	Total    int                       `json:"total"`
	RowCount int                       `json:"rowCount"`
	Current  int                       `json:"current"`
	Rows     []captivePortalSessionRow `json:"rows"`
}

// CaptivePortalZone is the normalised per-zone session count, joined with the
// configured zone description.
type CaptivePortalZone struct {
	ZoneID      string
	Description string
	Sessions    float64
}

// CaptivePortalSessions holds the aggregated result of FetchCaptivePortalSessions.
type CaptivePortalSessions struct {
	Present       bool
	Zones         []CaptivePortalZone // one entry per configured zone (0 sessions included)
	SessionsTotal float64
}

// FetchCaptivePortalSessions fetches per-zone session counts from the captive
// portal endpoints. Two requests are issued per scrape:
//  1. GET api/captiveportal/session/zones — flat map of zone ID → description
//     (returns [] when no zones are configured).
//  2. POST api/captiveportal/session/search (rowCount=-1) — all active sessions.
//     Only the zoneid field is decoded; PII fields are not modeled (D10).
//
// Sessions whose zoneid has no corresponding configured zone are counted in a
// synthetic zone with ZoneID "unknown" (only emitted when non-zero).
//
// A HTTP 404 on the zones endpoint is treated as "feature absent" for defensive
// compatibility with older/stripped builds that lack the captive portal module.
func (c *Client) FetchCaptivePortalSessions() (CaptivePortalSessions, *APICallError) {
	var data CaptivePortalSessions

	zonesURL, ok := c.endpoints["captivePortalZones"]
	if !ok {
		return data, &APICallError{
			Endpoint:   "captivePortalZones",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}
	sessionsURL, ok := c.endpoints["captivePortalSessions"]
	if !ok {
		return data, &APICallError{
			Endpoint:   "captivePortalSessions",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}

	// Fetch zones: an unconfigured portal returns PHP [] → empty map; zones keyed
	// by sequential-from-0 zoneids arrive as a JSON array (see captivePortalZoneMap).
	var zones captivePortalZoneMap
	if err := c.do("GET", zonesURL, nil, &zones); err != nil {
		if err.StatusCode == http.StatusNotFound {
			return data, nil // feature absent
		}
		return data, err
	}
	data.Present = true

	// No zones configured: there is nothing to attribute sessions to, and the
	// search is not merely wasted work (#524). searchAction takes no zoneid — it
	// always runs `configd captiveportal list_clients`, and on a box whose portal
	// has never started that reads a SQLite database with no schema (DB.open()
	// creates the file, only DB.create() builds the tables), so the script exits
	// non-zero and the firewall logs a traceback once per poll. Stop before it.
	if len(zones) == 0 {
		return data, nil
	}

	// Build a session count map keyed by zone ID.
	sessionCounts := make(map[string]float64)

	form := url.Values{
		"current":  {"1"},
		"rowCount": {"-1"},
	}
	var sessResp captivePortalSessionSearch
	if err := c.doForm(sessionsURL, form, &sessResp); err != nil {
		return data, err
	}

	for _, row := range sessResp.Rows {
		sessionCounts[row.ZoneID.String()]++
	}
	data.SessionsTotal = float64(sessResp.Total)

	// Build one CaptivePortalZone per configured zone (0 sessions included).
	for zoneID, description := range zones {
		data.Zones = append(data.Zones, CaptivePortalZone{
			ZoneID:      zoneID,
			Description: description,
			Sessions:    sessionCounts[zoneID],
		})
		delete(sessionCounts, zoneID)
	}

	// Any remaining zone IDs in sessionCounts are unknown — emit a synthetic entry.
	unknownCount := 0.0
	for _, count := range sessionCounts {
		unknownCount += count
	}
	if unknownCount > 0 {
		data.Zones = append(data.Zones, CaptivePortalZone{
			ZoneID:      "unknown",
			Description: "",
			Sessions:    unknownCount,
		})
	}

	// Sort by ZoneID for deterministic output and tests.
	sort.Slice(data.Zones, func(i, j int) bool {
		return data.Zones[i].ZoneID < data.Zones[j].ZoneID
	})

	return data, nil
}

// captivePortalVoucherRow mirrors one row from
// api/captiveportal/voucher/list_vouchers/{provider}/{group}, as computed by
// OPNsense's core Voucher auth connector (listVouchers(), a direct SQLite
// read — verified against opnsense/core src/opnsense/mvc/app/library/OPNsense/Auth/Voucher.php
// and a live 26.7-devel box, #207).
//
// SENSITIVITY (tracker #227): the row's own "username" field IS the voucher
// code — a bearer credential a guest redeems for network access, not an
// identifier — and is deliberately NOT modeled here, NOT decoded, and must
// never be added. This struct only carries the aggregate-safe fields: how
// long a voucher is valid for, its hard expiry cutoff (0 = none), and the
// state OPNsense itself derives (valid/unused/expired).
type captivePortalVoucherRow struct {
	Validity   flexInt    `json:"validity"`
	Expirytime flexInt    `json:"expirytime"`
	State      flexString `json:"state"`
}

type voucherDecodedRow struct {
	row         captivePortalVoucherRow
	validState  bool
	validExpiry bool
}

// voucherRowFields limits decoding to fields needed for aggregation. In particular,
// voucher usernames and all other per-voucher fields are ignored by the decoder.
type voucherRowFields struct {
	State  json.RawMessage `json:"state"`
	Expiry json.RawMessage `json:"expirytime"`
}

// UnmarshalJSON keeps the row decoder's historical tolerance: a null row and
// any state shape still count, and a non-object row still fails its group. A
// shape OPNsense does not produce only clears validState or validExpiry, which
// keeps that inventory out of the cache.
func (row *voucherDecodedRow) UnmarshalJSON(data []byte) error {
	var fields voucherRowFields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields.State) > 0 {
		if err := json.Unmarshal(fields.State, &row.row.State); err != nil {
			return err
		}
		row.validState = fields.State[0] == '"' && row.row.State != ""
	}
	expiry, ok := parseVoucherExpiry(fields.Expiry)
	row.row.Expirytime = flexInt(expiry)
	row.validExpiry = ok
	return nil
}

// parseVoucherExpiry is the single decision on a voucher's expiry: its value is
// what the group aggregates and its ok is what admits the inventory to the
// cache. The forms OPNsense variants emit for "no expiry" (null, "", []) are
// valid zero values.
func parseVoucherExpiry(raw json.RawMessage) (int, bool) {
	value := string(bytes.TrimSpace(raw))
	switch {
	case value == "":
		return 0, true
	case value[0] == '"':
		// The historical decoder read the raw text, so an escaped digit was never
		// a number: it stays no expiry, and the odd form leaves the inventory
		// incomplete.
		if bytes.IndexByte(raw, '\\') >= 0 || json.Unmarshal(raw, &value) != nil {
			return 0, false
		}
	case value[0] == '[':
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || len(items) != 0 {
			return 0, false
		}
		return 0, true
	}
	if value == "" || value == "null" || value == "[]" {
		return 0, true
	}
	return parseExactDecimalInt(value)
}

// parseExactDecimalInt truncates a decimal literal (optional sign, fraction and
// exponent) toward zero and reports whether the result fits an int. It works on
// the digits because a float64 rounds neighbouring integers together, which
// would carry a value just outside the int range back inside it.
func parseExactDecimalInt(s string) (int, bool) {
	negative := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	mantissa, exponent := s, 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		var ok bool
		if exponent, ok = parseDecimalExponent(s[i+1:]); !ok {
			return 0, false
		}
		mantissa = s[:i]
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	if whole+fraction == "" || !isDecimalDigits(whole) || !isDecimalDigits(fraction) {
		return 0, false
	}

	// point is where the decimal point falls in digits once leading zeros go.
	digits := strings.TrimLeft(whole+fraction, "0")
	point := len(whole) + exponent - (len(whole) + len(fraction) - len(digits))
	if digits == "" || point <= 0 {
		return 0, true // zero, or a magnitude below one
	}
	if point > maxDecimalIntDigits {
		return 0, false
	}
	var integer string
	if point <= len(digits) {
		integer = digits[:point]
	} else {
		integer = digits + strings.Repeat("0", point-len(digits))
	}
	if negative {
		integer = "-" + integer
	}
	n, err := strconv.ParseInt(integer, 10, 0)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// maxDecimalIntDigits exceeds the digit count of any int, so a longer integer
// part is out of range without building it.
const maxDecimalIntDigits = 20

// decimalExponentLimit saturates an exponent. It is far beyond any integer part
// that can fit, and small enough that adding a body length cannot overflow.
const decimalExponentLimit = 1 << 30

func parseDecimalExponent(s string) (int, bool) {
	negative := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	if !isDecimalDigits(s) || s == "" {
		return 0, false
	}
	exponent := 0
	for _, digit := range []byte(s) {
		exponent = min(exponent*10+int(digit-'0'), decimalExponentLimit)
	}
	if negative {
		exponent = -exponent
	}
	return exponent, true
}

func isDecimalDigits(s string) bool {
	for _, char := range []byte(s) {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// CaptivePortalVoucherGroup is the aggregated, per-(provider,group) voucher
// inventory: a count per state plus the group's earliest hard expiry deadline
// (if any voucher in the group has one).
type CaptivePortalVoucherGroup struct {
	Provider string
	Group    string

	// Counts is keyed by voucher state. OPNsense's Voucher connector only ever
	// emits "valid", "unused", or "expired" (see the source reference above),
	// but this is a map — not a fixed struct — so an unrecognised future state
	// is still counted and exported rather than silently dropped.
	Counts map[string]float64

	// NextExpiryTimestamp is the earliest positive `expirytime` (an absolute
	// Unix timestamp — OPNsense's own hard per-voucher cutoff, independent of
	// use) among this group's "unused"/"valid" vouchers. Most groups never set
	// this (expirytime 0 = "no hard cap", the common case; a voucher's
	// ordinary lifetime is governed by `validity` counted from first use, not
	// this field) so HasNextExpiryTimestamp is false far more often than true.
	NextExpiryTimestamp    float64
	HasNextExpiryTimestamp bool
}

// CaptivePortalVouchers holds the aggregated result of FetchCaptivePortalVouchers.
type CaptivePortalVouchers struct {
	// Present is false when the list_providers call itself fails with 404
	// (defensive: this is a core action, not a plugin, so a live box should
	// never actually 404 it — but a stripped/custom build might lack the
	// module entirely, mirroring FetchCaptivePortalSessions's own zones gate).
	Present bool

	// Complete reports whether every provider and group was read successfully.
	Complete bool

	// Groups is empty on any box with no voucher-type auth server configured,
	// or one with providers but no generated voucher groups yet — both are
	// normal, common states, not errors (issue #207).
	Groups []CaptivePortalVoucherGroup
}

// FetchCaptivePortalVouchers enumerates every voucher-type authentication
// provider, every voucher group each one has generated, and aggregates each
// group's vouchers by state. Three GETs per group (list_providers once,
// list_voucher_groups per provider, list_vouchers per group); a group can
// hold up to 10,000 vouchers, so rows are aggregated immediately and never
// held or exported individually.
//
// A provider or group whose own list call fails (for a reason other than the
// top-level 404 gate) is skipped, logged at debug level, and does not abort
// the rest of the fetch — mirroring FetchSMARTDevices's per-device tolerance.
func (c *Client) FetchCaptivePortalVouchers() (CaptivePortalVouchers, *APICallError) {
	return c.fetchCaptivePortalVouchers(false)
}

// FetchCaptivePortalVouchersFresh reads inventory without the response body cache.
// It is intended for the collector's separately bounded aggregate cache.
func (c *Client) FetchCaptivePortalVouchersFresh() (CaptivePortalVouchers, *APICallError) {
	return c.fetchCaptivePortalVouchers(true)
}

// CaptivePortalVoucherCacheTTL returns the configured inventory cache limit.
func (c *Client) CaptivePortalVoucherCacheTTL() time.Duration {
	if c.cache == nil {
		return 0
	}
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	return c.cache.ttls[c.endpoints["captivePortalVoucherProviders"]]
}

// voucherNames flattens a decoded provider or group list as a plain []string
// decode would (a null element becomes ""), and reports whether every name was
// a real one. A null list or element, or an empty name, is an incomplete list.
func voucherNames(values []*string) ([]string, bool) {
	complete := values != nil
	names := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil || *value == "" {
			complete = false
			names = append(names, "")
			continue
		}
		names = append(names, *value)
	}
	return names, complete
}

// withoutVoucherResponseBody drops the message from an API error, keeping its
// endpoint and status. A voucher row's username is a bearer credential (#227),
// and the message can hold bytes the upstream sent: a failed response carries a
// bounded slice of its body, and with status 0 Go's malformed-response transport
// error quotes what it received. So no voucher error keeps its message.
func withoutVoucherResponseBody(err *APICallError) *APICallError {
	if err == nil {
		return nil
	}
	safe := *err
	safe.Message = "upstream response detail withheld"
	return &safe
}

func (c *Client) fetchCaptivePortalVouchers(fresh bool) (CaptivePortalVouchers, *APICallError) {
	var data CaptivePortalVouchers

	providersURL, ok := c.endpoints["captivePortalVoucherProviders"]
	if !ok {
		return data, &APICallError{
			Endpoint:   "captivePortalVoucherProviders",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}
	groupsBase, ok := c.endpoints["captivePortalVoucherGroups"]
	if !ok {
		return data, &APICallError{
			Endpoint:   "captivePortalVoucherGroups",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}
	vouchersBase, ok := c.endpoints["captivePortalVouchers"]
	if !ok {
		return data, &APICallError{
			Endpoint:   "captivePortalVouchers",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}

	var providers []*string
	providerClient := c
	if fresh {
		providerClient = c.WithoutCache()
	}
	if err := providerClient.do("GET", providersURL, nil, &providers); err != nil {
		if err.StatusCode == http.StatusNotFound {
			return data, nil // feature absent
		}
		return data, withoutVoucherResponseBody(err)
	}
	providerNames, providersComplete := voucherNames(providers)
	data.Present = true
	data.Complete = providersComplete

	groupsClient, vouchersClient := *c, *c
	if c.observer != nil {
		groupsClient.observer = requestObserverAtPath{observer: c.observer, path: string(groupsBase)}
		vouchersClient.observer = requestObserverAtPath{observer: c.observer, path: string(vouchersBase)}
	}

	for _, provider := range providerNames {
		groupsPath := EndpointPath(string(groupsBase) + "/" + url.PathEscape(provider))
		var groups []*string
		if err := groupsClient.do("GET", groupsPath, nil, &groups); err != nil {
			data.Complete = false
			c.log.Debug("captiveportal voucher groups call failed for provider; skipping",
				"component", "opnsense-client",
				"provider", provider,
				"err", withoutVoucherResponseBody(err).Error())
			continue
		}

		groupNames, groupsComplete := voucherNames(groups)
		if !groupsComplete {
			data.Complete = false
		}
		for _, group := range groupNames {
			vouchersPath := EndpointPath(string(vouchersBase) + "/" + url.PathEscape(provider) + "/" + url.PathEscape(group))
			var rows []voucherDecodedRow
			if err := vouchersClient.do("GET", vouchersPath, nil, &rows); err != nil {
				data.Complete = false
				c.log.Debug("captiveportal list_vouchers call failed for group; skipping",
					"component", "opnsense-client",
					"provider", provider,
					"group", group,
					"err", withoutVoucherResponseBody(err).Error())
				continue
			}

			if rows == nil {
				data.Complete = false
			}

			vg := CaptivePortalVoucherGroup{
				Provider: provider,
				Group:    group,
				Counts:   make(map[string]float64),
			}
			for _, row := range rows {
				if !row.validState || !row.validExpiry {
					data.Complete = false
				}
				state := row.row.State.String()
				vg.Counts[state]++

				if (state == "unused" || state == "valid") && row.row.Expirytime.Int() > 0 {
					expiry := float64(row.row.Expirytime.Int())
					if !vg.HasNextExpiryTimestamp || expiry < vg.NextExpiryTimestamp {
						vg.NextExpiryTimestamp = expiry
						vg.HasNextExpiryTimestamp = true
					}
				}
			}

			data.Groups = append(data.Groups, vg)
		}
	}

	// Sort by (Provider, Group) for deterministic output and tests.
	sort.Slice(data.Groups, func(i, j int) bool {
		if data.Groups[i].Provider != data.Groups[j].Provider {
			return data.Groups[i].Provider < data.Groups[j].Provider
		}
		return data.Groups[i].Group < data.Groups[j].Group
	})

	return data, nil
}

// SameCaptivePortalSource reports whether two clients share the same underlying connection.
// Context-bound copies share it; independently constructed clients do not.
func (c *Client) SameCaptivePortalSource(other *Client) bool {
	return other != nil && c.httpClient == other.httpClient
}
