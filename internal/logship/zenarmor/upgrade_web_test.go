package zenarmor

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// The reboot web parse_error is malformed JSON: method contains literal 0x02
// and 0x08, not JSON escapes. Intentionally reject decoding, but keep shipping
// and capturing the lossless raw body. Sanitising bytes would rewrite sender
// evidence and could turn a corrupt HTTP method into an apparently valid one.
// The fixture retains field names/types only; all values are synthetic.
func TestUpgradeWebUnescapedControlsRejectedWithoutLoss(t *testing.T) {
	escaped, err := os.ReadFile("testdata/boot_web_controls.json")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(escaped) {
		t.Fatal("escaped fixture is not valid JSON")
	}
	doc := bytes.ReplaceAll(escaped, []byte(`\u0002`), []byte{2})
	doc = bytes.ReplaceAll(doc, []byte(`\b`), []byte{8})
	if json.Valid(doc) {
		t.Fatal("fixture mutation did not reproduce malformed JSON")
	}
	rec, d, parsed := parseDocFull("web", doc, nil)
	if parsed || d != nil {
		t.Fatal("unescaped JSON control bytes must not decode")
	}
	if rec.Body != string(doc) {
		t.Fatal("rejected body changed")
	}
	if len(rec.Attributes) != 0 {
		t.Fatal("partial attributes emitted from invalid JSON")
	}
	shipped, ok := buildRecord("web", doc, nil)
	if !ok || shipped.Body != string(doc) {
		t.Fatal("invalid web document dropped or rewritten")
	}
	// A legally escaped control byte is NOT the same malformed wire shape. Do
	// not introduce a blanket rejection of web documents or control-valued strings.
	validRec, parsed := parseDoc("web", escaped, nil)
	if !parsed {
		t.Fatal("valid escaped JSON rejected")
	}
	if validRec.Body != string(escaped) {
		t.Fatal("valid body changed")
	}
}
