package parser

import (
	"strings"
	"testing"
)

func TestLayer1(t *testing.T) {
	for _, tc := range []struct {
		input, id    string
		line, column int
	}{
		{"a: 日本\n二: \xff\n", "invalid-utf8", 2, 4},
		{"a:\n\tb: c\n", "no-tabs", 2, 1},
		{"a: [1, 2\n", "syntax-error", 1, 4},
		{"a: 1\n\"a\": 2\n", "key-duplicates", 2, 1},
		{"a: [{foo: 1, foo: 2}]\n", "key-duplicates", 1, 14},
		{"m: {0x10: x, 16: y}\n", "key-duplicates", 1, 14},
	} {
		t.Run(tc.id+tc.input, func(t *testing.T) {
			doc, got := Parse("path.yaml", []byte(tc.input), false, 2)
			if doc.AST != nil || len(got) != 1 || got[0].RuleID != tc.id || got[0].Line != tc.line ||
				got[0].Column != tc.column || got[0].Severity != SeverityError || got[0].Fixable != (tc.id == "no-tabs") {
				t.Fatalf("got AST %v, diagnostics %+v", doc.AST != nil, got)
			}
		})
	}
}

func TestFixTabsAndProtectScalars(t *testing.T) {
	input := "\ufeffa:\r\n\tb: x\r\ntext: |+ # header\r\n  \tliteral  \r\n\r\nquoted: \"first\r\n  \tsecond\"\r\nempty: |\r\nlast: y\r\n"
	doc, got := Parse(`folder\a.yml`, []byte(input), true, 4)
	if doc.AST == nil || len(got) != 1 || got[0].RuleID != "no-tabs" ||
		!doc.HadBOM || doc.OriginalEOL != "\r\n" || !strings.Contains(doc.NormalizedText, "\n    b: x\n") ||
		doc.Path != "folder/a.yml" {
		t.Fatalf("document %+v, diagnostics %+v", doc, got)
	}
	for i := range doc.Lines {
		want := i == 3 || i == 4 || i == 6
		if doc.Protected(i+1) != want {
			t.Errorf("line %d (%q): protected = %v", i+1, doc.Lines[i], !want)
		}
	}
	if doc, got := Parse("", []byte("a: 1\n---\na: 2\nb: {a: 3}\n"), false, 2); doc.AST == nil || len(got) != 0 {
		t.Fatalf("separate mappings: %+v", got)
	}
}

func TestIgnoreMap(t *testing.T) {
	input := `# kiku:ignore trailing_whitespace -- 理由
list:
- a
- b
outside: 1
flow: [a, # kiku:ignore trailing-whitespace
  b]
# kiku-disable-next-line trailing_whitespace, comments
-ignored-looking-key:
  nested:
    key: 1
# kiku:ignore-section trailing_whitespace
first: 1
second: 2

# kiku:disable trailing-whitespace
third: 3
# kiku-enable trailing-whitespace
fourth: 4
# kiku:ignore-paragraph trailing-whitespace
fifth: 5
---
literal: |
  # kiku-disable
quoted: "# kiku-disable"
last: value
`
	doc, diagnostics := Parse("test.yaml", []byte(input), false, 2)
	if len(diagnostics) != 0 || doc.AST == nil {
		t.Fatalf("parse: %+v", diagnostics)
	}
	for i := range doc.Lines {
		line := i + 1
		want := map[int]bool{2: true, 3: true, 4: true, 6: true, 7: true, 9: true, 10: true, 11: true, 12: true, 13: true, 14: true, 16: true, 17: true, 20: true, 21: true}[line]
		if doc.IgnoreMap.Ignored("trailing-whitespace", line) != want {
			t.Errorf("line %d (%q): ignored = %v", line, doc.Lines[i], !want)
		}
		if doc.IgnoreMap.Ignored("no-tabs", line) {
			t.Errorf("Layer 1 ignored on line %d", line)
		}
	}
	for _, line := range []int{1, 6, 8, 12, 16, 18, 20} {
		if !doc.IgnoreMap.ControlLine(line) {
			t.Errorf("line %d not recognized as control comment", line)
		}
	}
	for _, line := range []int{24, 25} {
		if doc.IgnoreMap.ControlLine(line) {
			t.Errorf("scalar mistaken for control comment on line %d", line)
		}
	}
}

func TestEnableRuleWithinWildcardDisable(t *testing.T) {
	input := "# kiku-disable\na: 1\n# kiku:enable line_length\nb: 2\n# kiku:ignore\nc: 3\n# kiku:enable\nd: 4\n"
	doc, diags := Parse("", []byte(input), false, 2)
	if doc.AST == nil {
		t.Fatal(diags)
	}
	for _, tc := range []struct {
		line             int
		length, comments bool
	}{
		{2, true, true},
		{4, false, true},
		{6, true, true},
		{8, false, false},
	} {
		if doc.IgnoreMap.Ignored("line-length", tc.line) != tc.length ||
			doc.IgnoreMap.Ignored("comments", tc.line) != tc.comments {
			t.Errorf("line %d: wrong active rules", tc.line)
		}
	}
}

func TestTabRescueAfterValidLiteralContent(t *testing.T) {
	doc, got := Parse("", []byte("a: |\n  \tfirst\n \tsecond\n"), true, 2)
	if doc.AST == nil || len(got) != 1 || got[0].Line != 3 ||
		doc.NormalizedText != "a: |\n  \tfirst\n   second\n" {
		t.Fatalf("normalized = %q, diagnostics = %+v", doc.NormalizedText, got)
	}
}

func TestQuotedScalarOriginExcludesLeadingTrivia(t *testing.T) {
	doc, got := Parse("", []byte("a:\n  \"first\n  second\"\nb: [\n  'single',\n  x]\n"), false, 2)
	if doc.AST == nil {
		t.Fatal(got)
	}
	for i := range doc.Lines {
		line := i + 1
		if doc.Protected(line) != (line == 3) || doc.QuotedStart(line) != (line == 2) {
			t.Errorf("incorrect protection at line %d", line)
		}
	}
}

func TestAnchorScope(t *testing.T) {
	const first = "a: &list [*list]\nb: *list\n"
	if doc, got := Parse("", []byte(first), false, 2); doc.AST == nil || len(got) != 0 {
		t.Fatalf("valid anchors: %+v", got)
	}
	if doc, got := Parse("", []byte(first+"---\na: *list\n"), false, 2); doc.AST != nil ||
		len(got) != 1 || got[0].RuleID != "syntax-error" || got[0].Line != 4 {
		t.Fatalf("alias crosses documents: %+v", got)
	}
}
