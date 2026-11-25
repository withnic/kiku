package rules

import (
	"strings"
	"testing"

	"github.com/withnic/kiku/internal/config"
	"github.com/withnic/kiku/internal/parser"
)

func runHygiene(t *testing.T, input string, cfg config.Config, fix bool) HygieneResult {
	t.Helper()
	resolved, err := config.Resolve(cfg, ".", nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, diagnostics := parser.Parse("a.yaml", []byte(input), fix, cfg.Rules.Indentation.Spaces)
	if doc.AST == nil {
		t.Fatalf("Layer 1 rejected input: %+v", diagnostics)
	}
	return Hygiene(doc, resolved, "a.yaml", fix)
}

func TestHygieneFix(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.Indentation.Enabled = false
	cfg.Rules.Indentation.Spaces = 4
	input := "\ufeffa:\r\n\tb: value \t\r\n\r\n\r\ntext: |+\r\n  line  \r\n\r\n\r\nquoted: \"first  \r\n second\"  \r\nlast: x\r\n\r\n"
	want := "a:\n    b: value\n\ntext: |+\n  line  \n\n\nquoted: \"first  \n second\"  \nlast: x\n"
	got := runHygiene(t, input, cfg, true)
	if string(got.Output) != want || got.NormalizedText != want {
		t.Fatalf("output = %q, want %q", got.Output, want)
	}
	if again := runHygiene(t, want, cfg, true); string(again.Output) != want || len(again.Diagnostics) != 0 {
		t.Fatalf("second pass: %+v", again)
	}
}

func TestHygieneLengthAndIgnore(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.LineLength.Max = 10
	input := strings.Join([]string{
		"a: 日本語日本語日本語",
		"# This comment is excluded",
		"b: https://example.com",
		"c: xyz # kiku:ignore trailing_whitespace  ",
		"# kiku:ignore-section trailing-whitespace",
		"d: x  ",
		"",
		"e: value  ",
	}, "\n")
	got := runHygiene(t, input, cfg, false)
	length := 0
	for _, diag := range got.Diagnostics {
		if diag.RuleID == "line-length" {
			length++
			if diag.Line != 1 || diag.Column != 11 || diag.Fixable {
				t.Errorf("unexpected diagnostic %+v", diag)
			}
		}
	}
	if length != 1 || string(got.Output) != input {
		t.Fatalf("unexpected check output %+v", got)
	}
	want := strings.TrimSuffix(input, "  ") + "\n"
	if got := runHygiene(t, input, cfg, true); string(got.Output) != want {
		t.Fatalf("fix = %q, want %q", got.Output, want)
	}
}

func TestHygieneEOLAndEOF(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		configure         func(*config.Config)
	}{
		{"empty", "", "", nil},
		{"trim EOF", "a: x\n\n\n", "a: x\n", nil},
		{"trim blank EOF", "a: x\n  ", "a: x\n", func(c *config.Config) { c.Rules.TrailingWhitespace.Enabled = false }},
		{"keep literal EOF", "a: |+\n  x\n\n\n", "a: |+\n  x\n\n\n", nil},
		{"empty literal", "a: |\nb: x  ", "a: |\nb: x\n", nil},
		{"disabled CRLF", "a: x  \r\n", "a: x\r\n", func(c *config.Config) { c.Rules.LineEndings.Enabled = false }},
		{"excluded CRLF", "a: x  \r\n", "a: x\r\n", func(c *config.Config) { c.Rules.LineEndings.Ignore = []string{"a.yaml"} }},
		{"force CRLF", "a: x\n# y", "a: x\r\n# y\r\n", func(c *config.Config) { c.Rules.LineEndings.Type = "crlf" }},
		{"inline CRLF", "a: x # kiku:ignore line_endings\r\nb: y\r\n", "a: x # kiku:ignore line_endings\r\nb: y\n", nil},
		{"BOM disabled", "\ufeffa: x\n", "\ufeffa: x\n", func(c *config.Config) { c.Rules.Encoding.Enabled = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			if tc.configure != nil {
				tc.configure(&cfg)
			}
			if got := runHygiene(t, tc.input, cfg, true); string(got.Output) != tc.want {
				t.Fatalf("output = %q, want %q", got.Output, tc.want)
			}
		})
	}
}

func TestTrailingBlankLinesNoDuplicateDiagnostics(t *testing.T) {
	cfg := config.Defaults()
	got := runHygiene(t, "a: x\n\n\n", cfg, false)
	if len(got.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want 2 final-newline diagnostics", got.Diagnostics)
	}
	for i, wantLine := range []int{2, 3} {
		d := got.Diagnostics[i]
		if d.RuleID != "final-newline" || d.Line != wantLine {
			t.Errorf("diagnostic[%d] = %+v, want final-newline at line %d", i, d, wantLine)
		}
	}

	cfg.Rules.FinalNewline.Enabled = false
	disabled := runHygiene(t, "a: x\n\n\n", cfg, true)
	if len(disabled.Diagnostics) != 1 || disabled.Diagnostics[0].RuleID != "blank-lines" || disabled.Diagnostics[0].Line != 3 {
		t.Fatalf("disabled final-newline diagnostics = %+v", disabled.Diagnostics)
	}
	if string(disabled.Output) != "a: x\n\n" {
		t.Fatalf("output = %q, want %q", disabled.Output, "a: x\n\n")
	}
}
