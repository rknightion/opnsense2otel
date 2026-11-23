// Command configredactionverify checks delivered configuration bodies without
// rendering their contents. It is for the live-delivery proof only; it must
// never sit in the export path before data reaches the configured sink.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

const (
	redactionMarker = "[redacted]"
	noNewlineLine   = "\\ No newline at end of file"
	truncationLine  = "[config diff truncated at 192 KiB]"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

type request struct {
	ConfigChange []string `json:"configchange"`
	ConfigState  []string `json:"configstate"`
}

// result is intentionally fixed-schema: it contains no body, key, value or
// decoder error that could disclose firewall configuration in a CI summary.
type result struct {
	ConfigChangeBodies            int  `json:"configchange_bodies"`
	ConfigChangeSensitiveElements int  `json:"configchange_sensitive_elements"`
	ConfigChangeBodiesRedacted    bool `json:"configchange_bodies_redacted"`
	ConfigStateBodies             int  `json:"configstate_bodies"`
	ConfigStateSensitiveKeys      int  `json:"configstate_sensitive_keys"`
	ConfigStateBodiesRedacted     bool `json:"configstate_bodies_redacted"`
}

func main() {
	var input request
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 8<<20))
	if decoder.Decode(&input) != nil {
		input = request{}
	}
	_, _ = os.Stdout.Write(mustJSON(verify(input)))
}

func mustJSON(value result) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"configchange_bodies":0,"configchange_sensitive_elements":0,"configchange_bodies_redacted":false,"configstate_bodies":0,"configstate_sensitive_keys":0,"configstate_bodies_redacted":false}`)
	}
	return append(encoded, '\n')
}

func verify(input request) result {
	result := result{
		ConfigChangeBodies:         len(input.ConfigChange),
		ConfigChangeBodiesRedacted: len(input.ConfigChange) > 0,
		ConfigStateBodies:          len(input.ConfigState),
		ConfigStateBodiesRedacted:  len(input.ConfigState) > 0,
	}
	for _, body := range input.ConfigChange {
		clean, sensitive := verifyConfigChange(body)
		result.ConfigChangeSensitiveElements += sensitive
		result.ConfigChangeBodiesRedacted = result.ConfigChangeBodiesRedacted && clean
	}
	for _, body := range input.ConfigState {
		clean, sensitive := verifyConfigState(body)
		result.ConfigStateSensitiveKeys += sensitive
		result.ConfigStateBodiesRedacted = result.ConfigStateBodiesRedacted && clean
	}
	return result
}

// verifyConfigChange applies the redactor's rules to delivered output without
// sharing its code, so a defect in one is not a blind spot in the other. Unified
// diffs are not XML documents, so a general XML decoder would reject valid input.
// Each side of each hunk is scanned separately. A sensitive element, or a value
// with no visible property directly before it, must hold only the exact marker,
// and so must text that no open element owns. Anything unscannable fails.
func verifyConfigChange(body string) (bool, int) {
	if strings.TrimSpace(body) == "" {
		return false, 0
	}
	lines := strings.Split(body, "\n")
	var old, updated diffSide
	seenXML, inHunk := false, false
	sensitive := 0
	for i, line := range lines {
		if header := hunkHeader.FindString(line); header != "" {
			if rest := strings.TrimSpace(line[len(header):]); rest != "" && rest != redactionMarker {
				return false, sensitive
			}
			inHunk = true
			old, updated = diffSide{}, diffSide{}
			continue
		}
		if line == noNewlineLine || (!inHunk && isFileHeader(lines, i)) ||
			(i == len(lines)-1 && line == truncationLine) {
			continue
		}
		prefix := byte(0)
		lineBody := line
		if len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' ') {
			prefix, lineBody = line[0], line[1:]
		}
		var clean, saw bool
		var count int
		switch prefix {
		case '-':
			clean, saw, count = old.scan(lineBody)
		case '+':
			clean, saw, count = updated.scan(lineBody)
		default:
			oldClean, oldSaw, oldCount := old.scan(lineBody)
			newClean, newSaw, newCount := updated.scan(lineBody)
			clean, saw, count = oldClean && newClean, oldSaw || newSaw, max(oldCount, newCount)
		}
		sensitive += count
		if !clean {
			return false, sensitive
		}
		seenXML = seenXML || saw
	}
	return seenXML, sensitive
}

func isFileHeader(lines []string, i int) bool {
	oldHeader := func(line string) bool {
		return strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "---\t")
	}
	newHeader := func(line string) bool {
		return strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "+++\t")
	}
	if oldHeader(lines[i]) {
		return i+1 < len(lines) && newHeader(lines[i+1])
	}
	return newHeader(lines[i]) && i > 0 && oldHeader(lines[i-1])
}

// diffSide is the verifier's state for one side of one hunk.
type diffSide struct {
	sensitive    string
	text         string
	valueVisible bool
}

func (s *diffSide) scan(line string) (clean, sawXML bool, sensitive int) {
	propertyOnLine := false
	for cursor := 0; cursor < len(line); {
		if s.sensitive != "" {
			closing := "</" + s.sensitive + ">"
			end := strings.Index(line[cursor:], closing)
			if end < 0 {
				return line[cursor:] == redactionMarker, true, sensitive
			}
			if line[cursor:cursor+end] != redactionMarker {
				return false, true, sensitive
			}
			cursor += end + len(closing)
			s.sensitive = ""
			continue
		}
		start, end := nextTag(line, cursor)
		text := line[cursor:start]
		tag := ""
		if end >= 0 {
			tag = line[start:end]
		}
		kind, name := classifyTag(tag)
		if trimmed := strings.TrimSpace(text); trimmed != "" &&
			(s.text == "" || (kind == tagClose && name != s.text)) {
			if trimmed != redactionMarker {
				return false, sawXML, sensitive
			}
			// A hunk wholly inside a value has no tags, only markers.
			sawXML = true
			s.text, s.valueVisible = "", false
		}
		if end < 0 {
			return start == len(line), sawXML, sensitive
		}
		cursor = end
		sawXML = true
		valueVisible := s.valueVisible
		s.valueVisible = false
		switch kind {
		case tagMarkup:
			propertyOnLine = false
			if s.text == "" {
				return false, true, sensitive
			}
		case tagClose:
			if propertyOnLine && name == s.text && strings.EqualFold(name, "property") {
				property := strings.TrimSpace(text)
				s.valueVisible = property != "" && !opnsense.SensitiveConfigKey(property)
			}
			s.text = ""
		case tagSelfClosing:
			s.text = ""
		default:
			s.text = ""
			if opnsense.SensitiveConfigKey(name) || (strings.EqualFold(name, "value") && !valueVisible) {
				sensitive++
				s.sensitive = name
			} else {
				s.text = name
				propertyOnLine = strings.EqualFold(name, "property")
			}
		}
	}
	return true, sawXML, sensitive
}

type tagKind int

const (
	tagInvalid tagKind = iota
	tagMarkup
	tagOpen
	tagClose
	tagSelfClosing
)

// nextTag returns the next well-formed tag at or after from. Without one, end is
// -1 and start is the unterminated '<', or len(line) when there is none.
func nextTag(line string, from int) (start, end int) {
	for search := from; ; {
		lt := strings.IndexByte(line[search:], '<')
		if lt < 0 {
			return len(line), -1
		}
		start = search + lt
		gt := strings.IndexByte(line[start:], '>')
		if gt < 0 {
			return start, -1
		}
		gt += start
		start += strings.LastIndexByte(line[start:gt], '<')
		if kind, _ := classifyTag(line[start : gt+1]); kind != tagInvalid {
			return start, gt + 1
		}
		search = gt + 1
	}
}

func classifyTag(tag string) (tagKind, string) {
	if len(tag) < 3 || tag[0] != '<' || tag[len(tag)-1] != '>' {
		return tagInvalid, ""
	}
	inner := tag[1 : len(tag)-1]
	kind := tagOpen
	switch {
	case inner[0] == '!' || inner[0] == '?':
		return tagMarkup, ""
	case inner[0] == '/':
		kind, inner = tagClose, strings.TrimRight(inner[1:], " \t")
	case inner[len(inner)-1] == '/':
		kind, inner = tagSelfClosing, inner[:len(inner)-1]
	}
	if cut := strings.IndexAny(inner, " \t"); cut >= 0 {
		if kind == tagClose {
			return tagInvalid, ""
		}
		inner = inner[:cut]
	}
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == ':', c >= 0x80:
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '.'):
		default:
			return tagInvalid, ""
		}
	}
	if inner == "" {
		return tagInvalid, ""
	}
	return kind, inner
}

func verifyConfigState(body string) (bool, int) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || (first != json.Delim('{') && first != json.Delim('[')) {
		return false, 0
	}
	clean, sensitive, err := verifyConfigStateToken(decoder, first)
	if err != nil {
		return false, sensitive
	}
	if _, err := decoder.Token(); err != io.EOF {
		return false, sensitive
	}
	return clean, sensitive
}

// Inspect tokens instead of decoding into maps: a later duplicate key must
// never overwrite and hide an earlier credential-bearing subtree.
func verifyConfigStateToken(decoder *json.Decoder, token json.Token) (bool, int, error) {
	delim, compound := token.(json.Delim)
	if !compound {
		return true, 0, nil
	}
	if delim != '{' && delim != '[' {
		return false, 0, errors.New("invalid snapshot structure")
	}
	seen := make(map[string]bool)
	clean, sensitive := true, 0
	hasProperty, hasValue, propertyVisible := false, false, true
	for decoder.More() {
		isProperty := false
		if delim == '{' {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok || seen[key] {
				return false, sensitive, errors.New("invalid snapshot key")
			}
			seen[key] = true
			if opnsense.SensitiveConfigKey(key) {
				clean = false
				sensitive++
			}
			isProperty = strings.EqualFold(key, "property")
			hasValue = hasValue || strings.EqualFold(key, "value")
		}
		child, err := decoder.Token()
		if err != nil {
			return false, sensitive, err
		}
		if isProperty {
			hasProperty = true
			name, ok := child.(string)
			if !ok || strings.TrimSpace(name) == "" || opnsense.SensitiveConfigKey(name) {
				propertyVisible = false
			}
		}
		childClean, childSensitive, err := verifyConfigStateToken(decoder, child)
		sensitive += childSensitive
		if err != nil {
			return false, sensitive, err
		}
		clean = clean && childClean
	}
	if hasProperty && hasValue && !propertyVisible {
		clean = false
		sensitive++
	}
	closing, err := decoder.Token()
	if err != nil || (delim == '{' && closing != json.Delim('}')) || (delim == '[' && closing != json.Delim(']')) {
		return false, sensitive, errors.New("invalid snapshot structure")
	}
	return clean, sensitive, nil
}
