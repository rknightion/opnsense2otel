package logship

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rknightion/opnsense2otel/v5/internal/options"
	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

// configChangeSourceName is the stable source identity used for the persisted
// cursor and the OTLP resource's opnsense.source attribute.
const configChangeSourceName = "configchange"

// configChangeMaxBodyBytes leaves a deliberately source-specific bound below
// the pipeline-wide record limit. A configuration diff is useful evidence, but
// one unexpectedly broad change must not consume the whole bounded queue.
const configChangeMaxBodyBytes = 192 * 1024

// The Poll observations are intentionally a closed, numeric-only schema. They
// are self-logs, so unlike the config diff itself they must never carry an
// upstream revision ID, a URI, or another value that the source did not define.
// The live delivery proof reads these records back through the exporter source
// to distinguish an empty Poll from a record that was later lost in delivery.
const (
	configChangePollSummaryMessage     = "config-change poll observation"
	configChangeDiffObservationMessage = "config-change diff observation"

	configChangePollBranchNoRevisions        = "no_revisions"
	configChangePollBranchBaseline           = "baseline"
	configChangePollBranchCursorNotInHistory = "cursor_not_in_history"
	configChangePollBranchEmitted            = "emitted"
)

// configChangeTruncationMarker is included in the cap, so an operator can tell
// a deliberately shortened diff from an upstream diff that happened to end.
const configChangeTruncationMarker = "\n[config diff truncated at 192 KiB]"

// ConfigChangeRevision is the subset of one backup-history row needed by the
// source. The application wiring adapts opnsense.ConfigBackupRevision to this
// type so logship does not make opnsense depend on an internal package.
type ConfigChangeRevision struct {
	ID        string
	Timestamp time.Time
	User      string
	// Description is the backup revision's human-readable context. OPNsense
	// writes the request URI as its first whitespace-delimited token, followed
	// by a localized "made changes" phrase; URI is preferred when an adapter
	// can supply it directly.
	Description string
	URI         string
}

// ConfigChangeFetcher is the narrow API seam for the config-change source.
// FetchConfigChangeRevisions returns the current retained backup history; its
// order is immaterial because the source orders it by timestamp and ID. The
// diff route's host argument is always "this", the local firewall provider.
type ConfigChangeFetcher interface {
	FetchConfigChangeRevisions(context.Context) ([]ConfigChangeRevision, error)
	FetchConfigChangeDiff(ctx context.Context, host, oldRevision, newRevision string) (string, error)
}

type opnsenseConfigChangeFetcher struct{ client *opnsense.Client }

func (f opnsenseConfigChangeFetcher) FetchConfigChangeRevisions(ctx context.Context) ([]ConfigChangeRevision, error) {
	revisions, apiErr := f.client.WithContext(ctx).FetchConfigBackupRevisions()
	if apiErr != nil {
		return nil, apiErr
	}
	result := make([]ConfigChangeRevision, 0, len(revisions))
	for _, revision := range revisions {
		result = append(result, ConfigChangeRevision{
			ID:          revision.ID,
			Timestamp:   revision.Timestamp,
			User:        revision.User,
			Description: revision.Description,
		})
	}
	return result, nil
}

func (f opnsenseConfigChangeFetcher) FetchConfigChangeDiff(ctx context.Context, host, oldRevision, newRevision string) (string, error) {
	diff, apiErr := f.client.WithContext(ctx).FetchConfigBackupDiff(host, oldRevision, newRevision)
	if apiErr != nil {
		return "", apiErr
	}
	return diff, nil
}

func init() {
	RegisterSource(func(deps Deps) (Source, error) {
		if !options.LogsConfigChangeEnabled() {
			return nil, nil
		}
		return NewConfigChangeSource(opnsenseConfigChangeFetcher{client: deps.Client}, deps.Logger), nil
	})
}

// configChangeCursor is persisted under the configchange StatefulSource key.
// A revision filename is the route's stable identity; timestamps are not used as
// the cursor because backup files can share a whole-second revision timestamp.
type configChangeCursor struct {
	LastRevision string `json:"last_revision"`
}

// ConfigChangeSource emits one Record for each revision added after its cursor.
// It has intentionally conservative bootstrap and retention behaviour: a fresh
// source seeds itself from the latest retained revision, and a persisted cursor
// no longer present in the bounded history is likewise advanced to latest.
// Neither case replays historical configuration content. This is at-most-once
// across restarts when --logs.state-file is usable; as with every logship source,
// a cursor can be persisted after queue admission but before sink acknowledgement,
// so delivery is never claimed to be exactly-once.
type ConfigChangeSource struct {
	client ConfigChangeFetcher
	log    *slog.Logger

	mu           sync.Mutex
	lastRevision string
	pollSequence uint64
}

type configChangeDiffObservation struct {
	bytes int
	lines int
}

// NewConfigChangeSource exposes the source for tests and alternate embeddings.
// The package's init registration adapts the public OPNsense client to this
// narrow fetcher interface without creating an import cycle.
func NewConfigChangeSource(client ConfigChangeFetcher, log *slog.Logger) *ConfigChangeSource {
	if log == nil {
		log = slog.Default()
	}
	return &ConfigChangeSource{client: client, log: log}
}

// Name implements Source.
func (s *ConfigChangeSource) Name() string { return configChangeSourceName }

// Poll implements Source. A baseline does not emit a diff because the endpoint
// needs a predecessor and replaying the retained backup window on enablement
// would turn old changes into new events.
func (s *ConfigChangeSource) Poll(ctx context.Context) ([]Record, error) {
	pollSequence := s.nextPollSequence()
	revisions, err := s.client.FetchConfigChangeRevisions(ctx)
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		s.observePoll(configChangePollBranchNoRevisions, pollSequence, 0)
		return nil, nil
	}

	ordered := append([]ConfigChangeRevision(nil), revisions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Timestamp.Equal(ordered[j].Timestamp) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Timestamp.Before(ordered[j].Timestamp)
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastRevision == "" {
		s.lastRevision = ordered[len(ordered)-1].ID
		s.observePoll(configChangePollBranchBaseline, pollSequence, 0)
		return nil, nil
	}

	previousIndex := -1
	for i, revision := range ordered {
		if revision.ID == s.lastRevision {
			previousIndex = i
			break
		}
	}
	if previousIndex == -1 {
		// History has a retention bound. Without the prior revision the first
		// unified diff cannot be reconstructed, and emitting retained rows risks
		// replaying old changes after a restore, so safely re-baseline instead.
		// Revision IDs originate at the firewall. Keep this legacy stderr message
		// for its fixed harness classifier, but do not attach either ID now that
		// source diagnostics also ship through the self-log path.
		s.log.Warn("config-change log source cursor is no longer in backup history; re-baselining without replay")
		s.lastRevision = ordered[len(ordered)-1].ID
		s.observePoll(configChangePollBranchCursorNotInHistory, pollSequence, 0)
		return nil, nil
	}

	records := make([]Record, 0, len(ordered)-previousIndex-1)
	diffObservations := make([]configChangeDiffObservation, 0, len(ordered)-previousIndex-1)
	for i := previousIndex + 1; i < len(ordered); i++ {
		previous, current := ordered[i-1], ordered[i]
		diff, err := s.client.FetchConfigChangeDiff(ctx, "this", previous.ID, current.ID)
		if err != nil {
			// Do not advance the cursor after a partial batch. Retrying the same
			// sequence next poll preserves one record per observed revision.
			return nil, fmt.Errorf("fetch config diff %q to %q: %w", previous.ID, current.ID, err)
		}
		record := configChangeRecord(current, diff)
		records = append(records, record)
		// Retain only the numeric observation until the complete batch is
		// available. The Record above necessarily owns the diff for delivery;
		// this side channel must not add a second raw copy of it.
		diffObservations = append(diffObservations, configChangeDiffObservation{
			bytes: len(record.Body), lines: configChangeDiffLineCount(record.Body),
		})
	}

	// Advance only once every diff is available. The pipeline owns delivery and
	// persistence; this cursor means this source will not fetch the same diff on
	// a later successful poll.
	s.lastRevision = ordered[len(ordered)-1].ID
	s.observePoll(configChangePollBranchEmitted, pollSequence, len(records))
	for index, observation := range diffObservations {
		s.observeDiff(pollSequence, index+1, observation.bytes, observation.lines)
	}
	return records, nil
}

func (s *ConfigChangeSource) nextPollSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pollSequence++
	return s.pollSequence
}

func (s *ConfigChangeSource) observePoll(branch string, pollSequence uint64, emittedRecords int) {
	s.log.Info(configChangePollSummaryMessage,
		"branch", branch,
		"poll_sequence", pollSequence,
		"emitted_records", emittedRecords,
	)
}

func (s *ConfigChangeSource) observeDiff(pollSequence uint64, recordIndex, diffBytes, diffLines int) {
	s.log.Info(configChangeDiffObservationMessage,
		"branch", configChangePollBranchEmitted,
		"poll_sequence", pollSequence,
		"record_index", recordIndex,
		"diff_bytes", diffBytes,
		"diff_lines", diffLines,
	)
}

func configChangeDiffLineCount(diff string) int {
	if diff == "" {
		return 0
	}
	lines := strings.Count(diff, "\n")
	if !strings.HasSuffix(diff, "\n") {
		lines++
	}
	return lines
}

// LoadState implements StatefulSource. Corrupt, empty and incomplete state is
// intentionally a fresh baseline rather than a history replay.
func (s *ConfigChangeSource) LoadState(data []byte) {
	var state configChangeCursor
	if len(data) == 0 || json.Unmarshal(data, &state) != nil || state.LastRevision == "" {
		return
	}
	s.mu.Lock()
	s.lastRevision = state.LastRevision
	s.mu.Unlock()
}

// SaveState implements StatefulSource. The pipeline atomically flushes this
// cursor every 30 seconds and on shutdown when --logs.state-file is configured.
func (s *ConfigChangeSource) SaveState() ([]byte, bool) {
	s.mu.Lock()
	lastRevision := s.lastRevision
	s.mu.Unlock()
	if lastRevision == "" {
		return nil, false
	}
	data, err := json.Marshal(configChangeCursor{LastRevision: lastRevision})
	if err != nil {
		return nil, false
	}
	return data, true
}

func configChangeRecord(revision ConfigChangeRevision, diff string) Record {
	return Record{
		Timestamp: revision.Timestamp,
		Body:      truncateConfigChangeDiff(redactConfigChangeDiff(html.UnescapeString(diff))),
		Attributes: map[string]string{
			"revision": revision.ID,
			"user":     revision.User,
			"uri":      configChangeURI(revision),
		},
	}
}

func configChangeURI(revision ConfigChangeRevision) string {
	if revision.URI != "" {
		return revision.URI
	}
	for _, field := range strings.Fields(revision.Description) {
		if strings.HasPrefix(field, "/") {
			return field
		}
	}
	return ""
}

func truncateConfigChangeDiff(diff string) string {
	if len(diff) <= configChangeMaxBodyBytes {
		return diff
	}
	prefixEnd := configChangeMaxBodyBytes - len(configChangeTruncationMarker)
	// html.UnescapeString returns valid UTF-8 for valid input. Preserve that
	// property when the byte budget happens to bisect a multibyte rune.
	for prefixEnd > 0 && !utf8.RuneStart(diff[prefixEnd]) {
		prefixEnd--
	}
	return diff[:prefixEnd] + configChangeTruncationMarker
}

var _ Source = (*ConfigChangeSource)(nil)
var _ StatefulSource = (*ConfigChangeSource)(nil)

// configChangeRedactionMarker replaces the value of any config.xml element whose
// name names a credential. The element itself is kept: an operator reading the
// diff still needs to see THAT a WireGuard key or a RADIUS secret changed, and
// on which revision, without the value leaving the firewall.
const configChangeRedactionMarker = "[redacted]"

// redactConfigChangeDiff strips credential values out of an upstream config.xml
// unified diff before it becomes a shipped log body.
//
// The diff is the whole point of this source, and a config.xml diff carries user
// password hashes, API keys and secrets, WireGuard and IPsec private keys, RADIUS
// shared secrets and certificate private halves whenever those sections change.
// Shipping it verbatim would hand every credential on the firewall to whoever can
// read the log backend, so the element vocabulary is shared with the config
// snapshot path via opnsense.SensitiveConfigKey rather than duplicated.
//
// Redaction runs BEFORE truncation so a body that is later cut short has already
// had its secrets removed, and so the truncation bound is measured against what
// is actually shipped.
//
// The scanner works per side of the diff, because a removed closing tag does not
// close the added value, and context lines are scrubbed against both sides. A
// hunk header clears both: the unified context window can begin or end inside a
// multi-line value, so its opening tag may simply not be in the hunk. Text is
// therefore shipped only when the element holding it is known. Anything else is
// replaced with the marker, and a generic <value> needs the <property> directly
// before it to be visible and not name a credential. Over-redaction is the safe
// direction here.
func redactConfigChangeDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var (
		b        strings.Builder
		old, new configDiffSide
		inHunk   bool
	)
	b.Grow(len(diff))
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if header := configDiffHunkHeader.FindString(line); header != "" {
			inHunk = true
			old, new = configDiffSide{}, configDiffSide{}
			b.WriteString(header)
			// Text after the range is a copied source line, not diff structure.
			if strings.TrimSpace(line[len(header):]) != "" {
				b.WriteByte(' ')
				b.WriteString(configChangeRedactionMarker)
			}
			continue
		}
		if line == configDiffNoNewlineLine || (!inHunk && isConfigDiffFileHeader(lines, i)) {
			b.WriteString(line)
			continue
		}
		var prefix byte
		body := line
		if len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' ') {
			prefix, body = line[0], line[1:]
		}
		var redacted string
		switch prefix {
		case '-':
			redacted = old.redact(body)
		case '+':
			redacted = new.redact(body)
		default:
			oldBody := old.redact(body)
			newBody := new.redact(body)
			switch {
			case oldBody == newBody, newBody == body:
				redacted = oldBody
			case oldBody == body:
				redacted = newBody
			default:
				// Both sides removed different spans. Their intersection is not
				// safe to infer from rewritten offsets, so suppress this line.
				redacted = configChangeRedactionMarker
			}
		}
		if prefix != 0 {
			b.WriteByte(prefix)
		}
		b.WriteString(redacted)
	}
	return b.String()
}

// configDiffNoNewlineLine is the one diff line that is neither content nor a
// header. Matching it exactly keeps any other backslash line under redaction.
const configDiffNoNewlineLine = "\\ No newline at end of file"

var configDiffHunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// isConfigDiffFileHeader recognises only an adjacent ---/+++ pair. A removed
// line whose content starts with "-- " has the same first characters, so the
// caller also requires that no hunk has started.
func isConfigDiffFileHeader(lines []string, i int) bool {
	isOld := func(line string) bool {
		return strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "---\t")
	}
	isNew := func(line string) bool {
		return strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "+++\t")
	}
	if isOld(lines[i]) {
		return i+1 < len(lines) && isNew(lines[i+1])
	}
	return isNew(lines[i]) && i > 0 && isOld(lines[i-1])
}

// configDiffSide is the scanner state for one side of one hunk.
type configDiffSide struct {
	// sensitive names a credential element left open by an earlier line.
	sensitive string
	// text names the ordinary element whose text is being read. It is empty
	// after any closing tag, so text between elements is never attributed to a
	// parent.
	text string
	// valueVisible is set only by a complete, visible, non-sensitive
	// <property> and is spent by the next tag of any kind.
	valueVisible bool
}

// redact rewrites one diff line's content and advances the side's state.
func (s *configDiffSide) redact(body string) string {
	var b strings.Builder
	propertyOnLine := false
	for i := 0; i < len(body); {
		if s.sensitive != "" {
			closing := "</" + s.sensitive + ">"
			b.WriteString(configChangeRedactionMarker)
			idx := strings.Index(body[i:], closing)
			if idx < 0 {
				return b.String()
			}
			b.WriteString(closing)
			i += idx + len(closing)
			s.sensitive = ""
			continue
		}

		start, end := nextConfigDiffTag(body, i)
		text := body[i:start]
		tag := ""
		if end >= 0 {
			tag = body[start:end]
		}
		kind, name := parseConfigDiffTag(tag)
		if strings.TrimSpace(text) != "" &&
			(s.text == "" || (kind == configDiffTagClose && name != s.text)) {
			// Text with no known owner may be the tail of a value whose opening
			// tag is outside this hunk. Anything up to the next closing tag can
			// belong to that value, including characters that look like a tag.
			b.WriteString(text[:len(text)-len(strings.TrimLeft(text, " \t"))])
			b.WriteString(configChangeRedactionMarker)
			s.text, s.valueVisible = "", false
			closing := strings.Index(body[start:], "</")
			if closing < 0 {
				return b.String()
			}
			i = start + closing
			continue
		}
		b.WriteString(text)
		if end < 0 {
			if start < len(body) {
				// An unterminated tag cannot be classified.
				b.WriteString(configChangeRedactionMarker)
			}
			return b.String()
		}
		i = end

		valueVisible := s.valueVisible
		s.valueVisible = false
		switch kind {
		case configDiffTagMarkup:
			// Comments and character data carry text of their own.
			propertyOnLine = false
			if s.text == "" {
				b.WriteString(configChangeRedactionMarker)
			} else {
				b.WriteString(tag)
			}
		case configDiffTagClose:
			b.WriteString(tag)
			if propertyOnLine && name == s.text && strings.EqualFold(name, "property") {
				property := strings.TrimSpace(text)
				s.valueVisible = property != "" && !opnsense.SensitiveConfigKey(property)
			}
			s.text = ""
		case configDiffTagSelfClosing:
			b.WriteString(tag)
			s.text = ""
		default:
			b.WriteString(tag)
			s.text = ""
			if opnsense.SensitiveConfigKey(name) || (strings.EqualFold(name, "value") && !valueVisible) {
				s.sensitive = name
			} else {
				s.text = name
				propertyOnLine = strings.EqualFold(name, "property")
			}
		}
	}
	return b.String()
}

type configDiffTagKind int

const (
	configDiffTagInvalid configDiffTagKind = iota
	configDiffTagMarkup
	configDiffTagOpen
	configDiffTagClose
	configDiffTagSelfClosing
)

// nextConfigDiffTag returns the bounds of the next well-formed tag at or after
// from. Without one, end is -1 and start is the unterminated '<', or len(body)
// when there is none. A '<' that does not begin a tag is left as text.
func nextConfigDiffTag(body string, from int) (start, end int) {
	for search := from; ; {
		lt := strings.IndexByte(body[search:], '<')
		if lt < 0 {
			return len(body), -1
		}
		start = search + lt
		gt := strings.IndexByte(body[start:], '>')
		if gt < 0 {
			return start, -1
		}
		gt += start
		start += strings.LastIndexByte(body[start:gt], '<')
		if kind, _ := parseConfigDiffTag(body[start : gt+1]); kind != configDiffTagInvalid {
			return start, gt + 1
		}
		search = gt + 1
	}
}

func parseConfigDiffTag(tag string) (configDiffTagKind, string) {
	if len(tag) < 3 || tag[0] != '<' || tag[len(tag)-1] != '>' {
		return configDiffTagInvalid, ""
	}
	inner := tag[1 : len(tag)-1]
	kind := configDiffTagOpen
	switch {
	case inner[0] == '!' || inner[0] == '?':
		return configDiffTagMarkup, ""
	case inner[0] == '/':
		kind, inner = configDiffTagClose, strings.TrimRight(inner[1:], " \t")
	case inner[len(inner)-1] == '/':
		kind, inner = configDiffTagSelfClosing, inner[:len(inner)-1]
	}
	if cut := strings.IndexAny(inner, " \t"); cut >= 0 {
		if kind == configDiffTagClose {
			return configDiffTagInvalid, ""
		}
		inner = inner[:cut]
	}
	if !validConfigDiffTagName(inner) {
		return configDiffTagInvalid, ""
	}
	return kind, inner
}

func validConfigDiffTagName(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == ':', c >= 0x80:
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return name != ""
}
