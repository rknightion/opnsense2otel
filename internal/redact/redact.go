package redact

import (
	"net/url"
	"strings"
)

const redacted = "[REDACTED]"

// sensitiveConfigSubstrings, sensitiveConfigTerms and sensitiveConfigExact are
// the shared vocabulary for every path that ships firewall configuration off the
// box: JSON snapshot rows, config.xml revision diffs and API error bodies.
// Keeping one vocabulary is the point — when the paths drifted, a field could
// be redacted in one and shipped in another.
//
// Substring matching is deliberate: it catches wgPrivateKey, radius_secret and
// api-key alike. The exact set carries OPNsense element names too short to
// match safely as a substring — <prv> is the private half of a certificate and
// <psk> is an IPsec pre-shared key.
var (
	sensitiveConfigSubstrings = []string{
		"password", "passphrase", "secret", "privatekey", "privkey",
		"sharedkey", "token", "apikey", "authkey", "credential", "passwd",
	}
	// A term is deliberately narrower than a substring. prv is the private
	// certificate half, and appears in compound keys such as prv_payload, but
	// it is too short to match at an arbitrary position.
	sensitiveConfigTerms = map[string]struct{}{
		"prv": {},
		// Auth ApiKeyField stores the public credential half as key. Match
		// separated terms too, without treating keyexpiry or monkey as secrets.
		"key": {},
	}
	sensitiveConfigExact = map[string]struct{}{
		"psk": {}, "pass": {},
		"otpseed": {}, "ldapbindpw": {}, "enckey": {}, "community": {},
	}
	sensitiveConfigNormalizer = strings.NewReplacer("_", "", "-", "", ".", "", "%", "")
)

// SensitiveKey reports whether a configuration key or XML element name
// names a credential that must never leave the firewall in a log record.
//
// The comparison is made on a separator-free lowercase form so snake_case,
// kebab-case, dotted and camelCase wire keys all resolve to the same
// vocabulary. Over-matching here is cheap; under-matching ships a credential.
func SensitiveKey(key string) bool {
	normalized := strings.ToLower(sensitiveConfigNormalizer.Replace(key))
	if _, ok := sensitiveConfigExact[normalized]; ok {
		return true
	}
	for _, needle := range sensitiveConfigSubstrings {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	if strings.HasPrefix(normalized, "prv") {
		return true
	}
	for _, term := range strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '%'
	}) {
		if _, ok := sensitiveConfigTerms[term]; ok {
			return true
		}
	}
	return false
}

// URL removes credentials from absolute URLs.
// Only URL components with credential semantics are changed; ordinary strings
// and the URL's scheme, host, path and non-sensitive query parameters survive.
func URL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}

	changed := false
	if parsed.User != nil {
		parsed.User = url.User(redacted)
		changed = true
	}
	if rawQuery, redacted := redactURLQuery(parsed.RawQuery); redacted {
		parsed.RawQuery = rawQuery
		changed = true
	}
	if !changed {
		return value
	}
	return parsed.String()
}

func redactURLQuery(rawQuery string) (string, bool) {
	parts := strings.Split(rawQuery, "&")
	changed := false
	for i, part := range parts {
		rawKey, _, hasValue := strings.Cut(part, "=")
		if !hasValue {
			continue
		}
		key, err := url.QueryUnescape(rawKey)
		if err != nil || !SensitiveKey(key) {
			continue
		}
		parts[i] = rawKey + "=" + url.QueryEscape(redacted)
		changed = true
	}
	return strings.Join(parts, "&"), changed
}
