package main

import "testing"

func TestVerifyAcceptsRedactedConfigChangeAndCleanSnapshot(t *testing.T) {
	result := verify(request{
		ConfigChange: []string{"--- old\n+++ new\n+<password>[redacted]</password>\n+<description>safe</description>"},
		ConfigState:  []string{`{"rule":{"description":"safe"}}`},
	})
	if !result.ConfigChangeBodiesRedacted || !result.ConfigStateBodiesRedacted {
		t.Fatalf("result = %+v, want both body families clean", result)
	}
	if result.ConfigChangeSensitiveElements != 1 || result.ConfigStateSensitiveKeys != 0 {
		t.Fatalf("coverage = %+v, want one redacted config-change element and no configstate keys", result)
	}
}

func TestVerifyRejectsUnredactedOrMalformedBodies(t *testing.T) {
	tests := []struct {
		name string
		in   request
	}{
		{
			name: "unredacted config change",
			in:   request{ConfigChange: []string{"+<api_key>not-safe</api_key>"}, ConfigState: []string{`{"safe":true}`}},
		},
		{
			name: "sensitive configstate key",
			in:   request{ConfigChange: []string{"+<description>safe</description>"}, ConfigState: []string{`{"nested":{"wg_private_key":"not-safe"}}`}},
		},
		{
			name: "malformed configstate JSON",
			in:   request{ConfigChange: []string{"+<description>safe</description>"}, ConfigState: []string{"{"}},
		},
		{
			name: "malformed config change XML",
			in:   request{ConfigChange: []string{"+<description"}, ConfigState: []string{`{"safe":true}`}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := verify(test.in)
			if result.ConfigChangeBodiesRedacted && result.ConfigStateBodiesRedacted {
				t.Fatalf("result = %+v, want at least one failing family", result)
			}
		})
	}
}

func TestVerifyUsesTheSharedSensitiveVocabulary(t *testing.T) {
	result := verify(request{
		ConfigChange: []string{"+<ldap_bindpw>[redacted]</ldap_bindpw>"},
		ConfigState:  []string{`{"community":"must-not-ship"}`},
	})
	if !result.ConfigChangeBodiesRedacted || result.ConfigStateBodiesRedacted {
		t.Fatalf("result = %+v, want shared-vocabulary handling", result)
	}
}

func TestVerifyRejectsHiddenSensitiveSnapshotAndNonSnapshotValues(t *testing.T) {
	for _, body := range []string{`{"rule":{"password":"synthetic-secret"},"rule":{}}`, `null`, `"not a snapshot"`} {
		clean, _ := verifyConfigState(body)
		if clean {
			t.Fatal("invalid or duplicate-key snapshot passed verification")
		}
	}
}

func TestVerifyAcceptsWrappedRedactedDiff(t *testing.T) {
	// The source preserves an opening tag on its own line, then replaces each
	// following value line. This is deliberately synthetic parser tolerance.
	clean, count := verifyConfigChange("+<password>\n+[redacted]\n+[redacted]</password>")
	if !clean || count != 1 {
		t.Fatalf("clean=%t count=%d, want true/1", clean, count)
	}
}

func TestVerifyDoesNotHideSensitivePayloadBehindDiffHeaderPrefix(t *testing.T) {
	clean, _ := verifyConfigChange("+<description>safe</description>\n+++<password>synthetic-secret</password>")
	if clean {
		t.Fatal("payload beginning with plus signs bypassed sensitive-element inspection")
	}
}

func TestVerifyRejectsUnmarkedTextOutsideAnOpenElement(t *testing.T) {
	for name, body := range map[string]string{
		"orphan continuation line":    "@@ -1,2 +1,2 @@\n SYNTH-A\n <descr>ok</descr>",
		"tail on a closing-tag line":  "@@ -1,2 +1,2 @@\n SYNTH-A</key>\n <descr>ok</descr>",
		"added orphan":                "@@ -1,2 +1,2 @@\n <descr>ok</descr>\n+SYNTH-A",
		"removed orphan":              "@@ -1,2 +1,2 @@\n <descr>ok</descr>\n-SYNTH-A",
		"text after a closing tag":    "@@ -1 +1 @@\n <a>ok</a>SYNTH-A",
		"text under a closed sibling": "@@ -1,3 +1,3 @@\n <parent>\n <a>ok</a>\n SYNTH-A",
		"mismatched closing tag":      "@@ -1,2 +1,2 @@\n <descr>\n SYNTH-A</key>",
		"orphan character data":       "@@ -1 +1 @@\n <![CDATA[SYNTH-A]]>",
		"header-shaped removed line":  "@@ -1,2 +1,2 @@\n <descr>ok</descr>\n--- SYNTH-A",
		"text after the hunk range":   "@@ -1 +1 @@ SYNTH-A\n <descr>ok</descr>",
		"truncation text mid-body":    "@@ -1,2 +1,2 @@\n[config diff truncated at 192 KiB]\n <descr>ok</descr>",
		"unterminated tag after xml":  "@@ -1,2 +1,2 @@\n <descr>ok</descr>\n <descr>ok<SYNTH-A",
		"header pair inside a hunk":   "@@ -1,2 +1,2 @@\n <descr>ok</descr>\n--- SYNTH-A\n+++ SYNTH-B",
		"open state is per side":      "@@ -1,3 +1,3 @@\n-<descr>\n+<a>ok</a>\n+SYNTH-A",
	} {
		t.Run(name, func(t *testing.T) {
			if clean, _ := verifyConfigChange(body); clean {
				t.Fatal("unmarked text passed verification")
			}
		})
	}
}

func TestVerifyRejectsValueWithoutAVisibleProperty(t *testing.T) {
	for name, body := range map[string]string{
		"no property":         "@@ -1 +1 @@\n+<value>SYNTH-A</value>",
		"sensitive property":  "@@ -1,2 +1,2 @@\n <property>oinkcode</property>\n+<value>SYNTH-A</value>",
		"other side":          "@@ -1,2 +1,2 @@\n-<property>ruleset</property>\n+<value>SYNTH-A</value>",
		"earlier hunk":        "@@ -1 +1 @@\n <property>ruleset</property>\n@@ -9 +9 @@\n+<value>SYNTH-A</value>",
		"consumed property":   "@@ -1,3 +1,3 @@\n <property>ruleset</property>\n <value>ok</value>\n+<value>SYNTH-A</value>",
		"intervening element": "@@ -1,3 +1,3 @@\n <property>ruleset</property>\n </tag>\n+<value>SYNTH-A</value>",
		"split property":      "@@ -1,3 +1,3 @@\n <property>rule\n set</property>\n+<value>SYNTH-A</value>",
	} {
		t.Run(name, func(t *testing.T) {
			if clean, _ := verifyConfigChange(body); clean {
				t.Fatal("unredacted value passed verification")
			}
		})
	}
}

func TestVerifyAcceptsRedactorOutputShapes(t *testing.T) {
	for name, body := range map[string]string{
		"orphan markers":    "@@ -5,7 +5,7 @@\n [redacted]\n-[redacted]\n+[redacted]\n [redacted]</key>\n <mode>m</mode>",
		"only markers":      "@@ -5,7 +5,7 @@\n [redacted]\n-[redacted]\n+[redacted]\n [redacted]",
		"redacted pair":     "@@ -1,3 +1,3 @@\n <property>oinkcode</property>\n-<value>[redacted]</value>\n+<value>[redacted]</value>",
		"visible pair":      "@@ -1,3 +1,3 @@\n <property>ruleset</property>\n-<value>old</value>\n+<value>new</value>",
		"file headers":      "--- old\t2026-01-01\n+++ new\t2026-01-02\n@@ -1 +1 @@\n-<descr>a</descr>\n\\ No newline at end of file\n+<descr>b</descr>",
		"multi-line text":   "@@ -1,3 +1,3 @@\n <descr>first line\n-second old</descr>\n+second new</descr>",
		"truncation marker": "@@ -1 +1 @@\n <descr>ok</descr>\n[config diff truncated at 192 KiB]",
	} {
		t.Run(name, func(t *testing.T) {
			if clean, _ := verifyConfigChange(body); !clean {
				t.Fatal("redactor output failed verification")
			}
		})
	}
}

func TestVerifyRejectsSensitivePropertyValueInSnapshot(t *testing.T) {
	for _, body := range []string{
		`{"property":"oinkcode","value":"synthetic"}`,
		`{"value":"synthetic","property":"oinkcode"}`,
		`{"Property":"oinkcode","Value":"synthetic"}`,
		`{"property":1,"value":"synthetic"}`,
		`{"property":{"name":"ruleset"},"value":"synthetic"}`,
	} {
		if clean, _ := verifyConfigState(body); clean {
			t.Errorf("snapshot passed verification: %s", body)
		}
	}
	if clean, _ := verifyConfigState(`{"property":"ruleset","value":"visible"}`); !clean {
		t.Error("ordinary property and value failed verification")
	}
}
