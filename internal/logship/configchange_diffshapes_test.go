package logship

import (
	"strings"
	"testing"
)

// These bodies are verbatim diff -U3 output for synthetic config.xml revisions,
// without the two file-header lines. Keeping the real hunk shape matters: the
// context window decides whether an opening tag is visible to the redactor.
const (
	diffReplaceFarBelowOpen = `@@ -5,7 +5,7 @@
 SYNTH-L1
 SYNTH-L2
 SYNTH-L3
-SYNTH-OLD4
+SYNTH-NEW4
 SYNTH-L5
 SYNTH-L6
 SYNTH-L7`
	diffTailBeforeSibling = `@@ -10,7 +10,7 @@
 SYNTH-L6
 SYNTH-L7
 </key>
-<mode>old</mode>
+<mode>new</mode>
 <f3>c</f3>
 <f4>d</f4>
 <f5>e</f5>`
	diffTailOnClosingLine = `@@ -9,7 +9,7 @@
 SYNTH-L5
 SYNTH-L6
 SYNTH-L7</key>
-<mode>old</mode>
+<mode>new</mode>
 <f3>c</f3>
 <f4>d</f4>
 <f5>e</f5>`
	diffInHunkControl = `@@ -2,7 +2,7 @@
 <filler1>a</filler1>
 <filler2>b</filler2>
 <key>
-SYNTH-OLD1
+SYNTH-NEW1
 SYNTH-L2
 SYNTH-L3
 SYNTH-L4`
	diffPairBothSides = `@@ -1,6 +1,6 @@
 <fileTags>
 <tag>
 <property>oinkcode</property>
-<value>SYNTH-IDS-OLD</value>
+<value>SYNTH-IDS-NEW</value>
 </tag>
 </fileTags>`
	diffPairAddedSensitive = `@@ -1,6 +1,6 @@
 <fileTags>
 <tag>
-<property>ruleset</property>
-<value>visible-old</value>
+<property>oinkcode</property>
+<value>SYNTH-IDS-NEW</value>
 </tag>
 </fileTags>`
	diffPairRemovedSensitive = `@@ -1,6 +1,6 @@
 <fileTags>
 <tag>
-<property>oinkcode</property>
-<value>SYNTH-IDS-OLD</value>
+<property>ruleset</property>
+<value>visible-new</value>
 </tag>
 </fileTags>`
	diffPairVisible = `@@ -1,6 +1,6 @@
 <fileTags>
 <tag>
 <property>ruleset</property>
-<value>visible-old</value>
+<value>visible-new</value>
 </tag>
 </fileTags>`
)

func TestConfigChangeRecord_RedactsValuesAcrossHunkShapes(t *testing.T) {
	cases := []struct {
		name   string
		diff   string
		absent []string
		kept   []string
	}{
		{name: "replace four lines below the opening tag", diff: diffReplaceFarBelowOpen, absent: []string{"SYNTH-"}},
		{name: "tail context before a changed sibling", diff: diffTailBeforeSibling, absent: []string{"SYNTH-"}, kept: []string{"</key>", "-<mode>old</mode>", "+<mode>new</mode>", " <f3>c</f3>"}},
		{name: "value ends on the closing-tag line", diff: diffTailOnClosingLine, absent: []string{"SYNTH-"}, kept: []string{"[redacted]</key>", "-<mode>old</mode>", "+<mode>new</mode>"}},
		{name: "opening tag inside the hunk keeps ordinary content", diff: diffInHunkControl, absent: []string{"SYNTH-"}, kept: []string{" <filler1>a</filler1>", " <filler2>b</filler2>", " <key>"}},
		{name: "sensitive property on both sides", diff: diffPairBothSides, absent: []string{"SYNTH-"}, kept: []string{" <property>oinkcode</property>", "-<value>[redacted]</value>", "+<value>[redacted]</value>"}},
		{name: "property becomes sensitive on the added side", diff: diffPairAddedSensitive, absent: []string{"SYNTH-"}, kept: []string{"-<value>visible-old</value>", "+<value>[redacted]</value>"}},
		{name: "property was sensitive on the removed side", diff: diffPairRemovedSensitive, absent: []string{"SYNTH-"}, kept: []string{"-<value>[redacted]</value>", "+<value>visible-new</value>"}},
		{name: "ordinary property keeps its value", diff: diffPairVisible, kept: []string{"-<value>visible-old</value>", "+<value>visible-new</value>"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := configChangeRecord(ConfigChangeRevision{ID: "rev-2"}, tc.diff).Body
			for _, absent := range tc.absent {
				if strings.Contains(body, absent) {
					t.Errorf("body carries %q:\n%s", absent, body)
				}
			}
			for _, kept := range tc.kept {
				if !strings.Contains(body, kept) {
					t.Errorf("body lost %q:\n%s", kept, body)
				}
			}
		})
	}
}

func TestRedactConfigChangeDiff_ValueNeedsAnAdjacentVisibleProperty(t *testing.T) {
	cases := []struct {
		name string
		diff string
	}{
		{"no property", "@@ -1 +1 @@\n+<value>SYNTH-V</value>"},
		{"sensitive property", "@@ -1,2 +1,2 @@\n <property>api_token</property>\n+<value>SYNTH-V</value>"},
		{"property only on the other side", "@@ -1,2 +1,2 @@\n-<property>ruleset</property>\n+<value>SYNTH-V</value>"},
		{"property in an earlier hunk", "@@ -1 +1 @@\n <property>ruleset</property>\n@@ -9 +9 @@\n+<value>SYNTH-V</value>"},
		{"property already consumed", "@@ -1,3 +1,3 @@\n <property>ruleset</property>\n <value>ok</value>\n+<value>SYNTH-V</value>"},
		{"element between property and value", "@@ -1,3 +1,3 @@\n <property>ruleset</property>\n </tag>\n+<value>SYNTH-V</value>"},
		{"property split across lines", "@@ -1,3 +1,3 @@\n <property>rule\n set</property>\n+<value>SYNTH-V</value>"},
		{"empty property", "@@ -1,2 +1,2 @@\n <property></property>\n+<value>SYNTH-V</value>"},
		{"multi-line value", "@@ -1,2 +1,2 @@\n+<value>SYNTH-V\n+SYNTH-W</value>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactConfigChangeDiff(tc.diff)
			if strings.Contains(got, "SYNTH-") {
				t.Fatalf("value survived:\n%s", got)
			}
		})
	}
}

func TestRedactConfigChangeDiff_OrphanTextFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		diff string
		kept []string
	}{
		{name: "tail with a tag-like fragment", diff: "@@ -1,2 +1,2 @@\n SYNTH-A<SYNTH-B>SYNTH-C</key>\n <descr>ok</descr>", kept: []string{"</key>", "<descr>ok</descr>"}},
		{name: "text after a closing tag", diff: "@@ -1 +1 @@\n <a>ok</a>SYNTH-A", kept: []string{"<a>ok</a>"}},
		{name: "text under a closed sibling", diff: "@@ -1,3 +1,3 @@\n <parent>\n <a>ok</a>\n SYNTH-A", kept: []string{"<a>ok</a>"}},
		{name: "text before a mismatched closing tag", diff: "@@ -1,2 +1,2 @@\n <descr>\n SYNTH-A</key>", kept: []string{"<descr>", "</key>"}},
		{name: "unterminated tag", diff: "@@ -1 +1 @@\n <descr>ok<SYNTH-A", kept: []string{"<descr>ok"}},
		{name: "orphan character data", diff: "@@ -1 +1 @@\n <![CDATA[SYNTH-A]]>"},
		{name: "removed line that looks like a file header", diff: "@@ -1 +1 @@\n--- SYNTH-A\n+++ SYNTH-B"},
		{name: "text after the hunk range", diff: "@@ -1 +1 @@ SYNTH-A\n <descr>ok</descr>", kept: []string{"@@ -1 +1 @@", "<descr>ok</descr>"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactConfigChangeDiff(tc.diff)
			if strings.Contains(got, "SYNTH-") {
				t.Fatalf("orphan text survived:\n%s", got)
			}
			for _, kept := range tc.kept {
				if !strings.Contains(got, kept) {
					t.Errorf("lost %q:\n%s", kept, got)
				}
			}
		})
	}
}

func TestRedactConfigChangeDiff_KeepsDiffStructureLines(t *testing.T) {
	diff := "--- old\t2026-01-01\n+++ new\t2026-01-02\n@@ -1 +1 @@\n-<descr>a</descr>\n\\ No newline at end of file\n+<descr>b</descr>\n\\ No newline at end of file\n"
	if got := redactConfigChangeDiff(diff); got != diff {
		t.Errorf("structure lines changed:\ngot:\n%s\nwant:\n%s", got, diff)
	}
}

func TestRedactConfigChangeDiff_KeepsMultilineOrdinaryText(t *testing.T) {
	diff := "@@ -1,3 +1,3 @@\n <descr>first line\n-second old</descr>\n+second new</descr>"
	if got := redactConfigChangeDiff(diff); got != diff {
		t.Errorf("ordinary multi-line text changed:\ngot:\n%s\nwant:\n%s", got, diff)
	}
}
