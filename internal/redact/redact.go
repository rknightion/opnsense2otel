package redact

import (
	"net/url"
	"strings"
	"unicode"
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
		"wapipass", "oinkcode",
	}
	// A term is deliberately narrower than a substring. prv is the private
	// certificate half, and appears in compound keys such as prv_payload, but
	// it is too short to match at an arbitrary position.
	sensitiveConfigTerms = map[string]struct{}{
		"prv": {},
		// Auth ApiKeyField stores the public credential half as key. Match
		// separated terms too, without treating keyexpiry or monkey as secrets.
		"key": {},
		// Short names used by ACME DNS provider fields. As substrings they would
		// also match bypass and compass.
		"hmac": {}, "pw": {}, "pass": {}, "authdata": {}, "otpcode": {},
	}
	// api names a credential only as the last term (dns_namecheap_api). Earlier
	// in a name it describes something else, such as api_url.
	sensitiveConfigFinalTerm = "api"
	sensitiveConfigExact     = map[string]struct{}{
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
	fields := strings.FieldsFunc(key, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '%'
	})
	for _, field := range fields {
		if isSensitiveConfigTerm(strings.ToLower(field)) {
			return true
		}
		words := camelCaseWords(field)
		final := len(words) - 1
		for i, word := range words {
			if isSensitiveConfigWord(word, i, words) {
				return true
			}
			// A one-word term can be written as two: authData, otpCode.
			if i < final && isSensitiveConfigWord(word+words[i+1], i+1, words) {
				return true
			}
		}
	}
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	if strings.ToLower(last) == sensitiveConfigFinalTerm {
		return true
	}
	words := camelCaseWords(last)
	return words[len(words)-1] == sensitiveConfigFinalTerm
}

func isSensitiveConfigTerm(term string) bool {
	_, ok := sensitiveConfigTerms[term]
	return ok
}

// isSensitiveConfigWord applies the vocabulary to the camelCase word at index i.
// key counts only as the field's last word and not after public: dnsCfKey holds
// a key, KeyExpiry holds a date and publicKey is the shareable half.
func isSensitiveConfigWord(word string, i int, words []string) bool {
	if word != "key" {
		return isSensitiveConfigTerm(word)
	}
	return i == len(words)-1 && (i == 0 || words[i-1] != "public")
}

// camelCaseWords splits one separator-free field into lowercase words. A word
// starts at an uppercase letter after a lowercase letter or digit, and at the
// last capital of an acronym that runs into a word (PWField is pw, field).
func camelCaseWords(field string) []string {
	runes := []rune(field)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		if !unicode.IsUpper(runes[i]) {
			continue
		}
		previous := runes[i-1]
		if unicode.IsLower(previous) || unicode.IsDigit(previous) ||
			(unicode.IsUpper(previous) && i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
			words = append(words, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	return append(words, strings.ToLower(string(runes[start:])))
}

// ConfigPropertyValue removes the generic value of a property/value pair unless
// its property is a visible name outside the credential vocabulary. The value's
// own key says nothing about what it holds, so this fails closed.
func ConfigPropertyValue(fields map[string]any) {
	hasProperty, visible := false, true
	for key, child := range fields {
		if !strings.EqualFold(key, "property") {
			continue
		}
		hasProperty = true
		name, ok := child.(string)
		if !ok || strings.TrimSpace(name) == "" || SensitiveKey(name) {
			visible = false
		}
	}
	if !hasProperty || visible {
		return
	}
	for key := range fields {
		if strings.EqualFold(key, "value") {
			delete(fields, key)
		}
	}
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
