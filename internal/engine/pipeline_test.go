package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/withnic/kiku/internal/config"
)

func TestGolden(t *testing.T) {
	files, err := filepath.Glob("../../testdata/golden/*.in.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("golden fixtures: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".in.yaml")
		t.Run(name, func(t *testing.T) {
			cfg := config.Defaults()
			switch name {
			case "sort":
				cfg.Rules.SortKeys.Enabled = true
				cfg.Rules.DocumentStart.Enabled = true
			case "hybrid":
				cfg.Rules.SequenceStyle.Mode = "hybrid"
			}
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(file, ".in.yaml") + ".out.yaml")
			if err != nil {
				t.Fatal(err)
			}
			assertFix(t, cfg, input, want)
		})
	}
}

func assertFix(t *testing.T, cfg config.Config, input, want []byte) {
	t.Helper()
	resolved, err := config.Resolve(cfg, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostics, err := Process("example.yaml", "example.yaml", input, resolved, true)
	if err != nil {
		t.Fatalf("%v\ndiagnostics: %v", err, diagnostics)
	}
	if string(out) != string(want) {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
	again, diagnostics, err := Process("example.yaml", "example.yaml", out, resolved, true)
	if err != nil || string(again) != string(out) {
		t.Fatalf("second fix: %v\n%s\ndiagnostics: %v", err, again, diagnostics)
	}
}

func TestSequenceModes(t *testing.T) {
	for _, tc := range []struct{ mode, input, want string }{
		{"flow", "items:\n  - a\n  - b\n\nkept:\n  - a # comment\n", "items: [a, b]\n\nkept:\n  - a  # comment\n"},
		{"consistent", "items: [a]\nmore:\n  - b\n---\nitems:\n  - c\nmore: [d]\n", "items: [a]\nmore: [b]\n---\nitems:\n  - c\nmore:\n  - d\n"},
		{"hybrid", "items:\n  - a\n  - b\nmore: [a, b, c, d]\n", "items: [a, b]\nmore:\n  - a\n  - b\n  - c\n  - d\n"},
		{"block", "items: [[a], [b, c]]\n", "items:\n  - - a\n  - - b\n    - c\n"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Rules.SequenceStyle.Mode = tc.mode
			assertFix(t, cfg, []byte(tc.input), []byte(tc.want))
		})
	}
}

func TestCombinedRules(t *testing.T) {
	t.Run("null and sort", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SortKeys.Enabled = true
		cfg.Rules.NullStyle.Style = "tilde"
		assertFix(t, cfg, []byte("z: # note\na: null\ny:\n"), []byte("a: ~\ny: ~\nz: ~  # note\n"))
	})
	t.Run("sorted nested map in flow sequence", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SortKeys.Enabled = true
		cfg.Rules.SequenceStyle.Mode = "flow"
		assertFix(t, cfg, []byte("seq:\n  - z: 1\n    a: yes\n"), []byte("seq: [{a: true, z: 1}]\n"))
	})
	t.Run("multiline literals retain contents", func(t *testing.T) {
		cfg := config.Defaults()
		assertFix(t, cfg, []byte("root:\n    literal: |2\n        body  \n    other: ok\n"), []byte("root:\n  literal: |2\n      body  \n  other: ok\n"))
	})
	t.Run("flow conversion on marker line", func(t *testing.T) {
		cfg := config.Defaults()
		assertFix(t, cfg, []byte("--- [a, b]\n---\nfoo: True\n"), []byte("---\n- a\n- b\n---\nfoo: true\n"))
	})
	t.Run("scientific numbers and quote escapes", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.Quotes.Prefer = "single"
		assertFix(t, cfg, []byte("num: \"1e5\"\nother: \"08\"\nescape: \"x\\ty\"\napostrophe: \" can't \"\n"), []byte("num: '1e5'\nother: '08'\nescape: \"x\\ty\"\napostrophe: ' can''t '\n"))
	})
	t.Run("byte exact CRLF", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.LineEndings.Enabled = false
		assertFix(t, cfg, []byte("seq: [a, b]\r\n#comment\r\n"), []byte("seq:\r\n  - a\r\n  - b\r\n# comment\r\n"))
	})
}

func TestTruthyPreservesMappingKeys(t *testing.T) {
	t.Run("block mapping keys", func(t *testing.T) {
		cfg := config.Defaults()
		assertFix(t, cfg, []byte("on:\n  push:\n    branches: [main]\nyes: one\ntrue: two\nflag: YES\n"),
			[]byte("on:\n  push:\n    branches:\n      - main\nyes: one\ntrue: two\nflag: true\n"))
	})
	t.Run("flow sequence mapping key", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SequenceStyle.Mode = "flow"
		assertFix(t, cfg, []byte("items:\n  - on: YES\n"), []byte("items: [{on: true}]\n"))
	})
}

func TestSortKeysWithMultilineLiteral(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.SortKeys.Enabled = true
	assertFix(t, cfg, []byte("z: |\n  line1\n  line2\na: 1\n"), []byte("a: 1\nz: |\n  line1\n  line2\n"))
}

func TestMultibyteEdgeCases(t *testing.T) {
	t.Run("hybrid length counts runes not bytes", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SequenceStyle.Mode = "hybrid"
		cfg.Rules.SequenceStyle.HybridMaxLineLength = 20
		// "[あいうえお, かきくけこ]" is 14 runes (36 bytes > 20 bytes), so hybrid must keep flow.
		assertFix(t, cfg, []byte("言葉:\n  - あいうえお\n  - かきくけこ\n"), []byte("言葉: [あいうえお, かきくけこ]\n"))
	})
	t.Run("sort Japanese keys with comments and emoji", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SortKeys.Enabled = true
		input := "# 最後のメモ\nん: '終' #🚀\n# 最初のメモ\nあ: |\n  日本語\n"
		want := "# 最初のメモ\nあ: |\n  日本語\n# 最後のメモ\nん: 終  # 🚀\n"
		assertFix(t, cfg, []byte(input), []byte(want))
	})
	t.Run("flow sequence quotes Japanese value with flow indicators", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SequenceStyle.Mode = "flow"
		assertFix(t, cfg, []byte("記号:\n  - 日本語,区切り\n  - 絵文字🚀\n"), []byte("記号: [\"日本語,区切り\", 絵文字🚀]\n"))
	})
	t.Run("single quote preference preserves unicode and escapes", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.Quotes.Prefer = "single"
		assertFix(t, cfg, []byte("文字: \"日本語: '引用'\"\n制御: \"🚀\\n次行\"\n"), []byte("文字: '日本語: ''引用'''\n制御: \"🚀\\n次行\"\n"))
	})
	t.Run("unicode hex escapes in flow sequence convert cleanly", func(t *testing.T) {
		cfg := config.Defaults()
		assertFix(t, cfg,
			[]byte("items: [\"\\x07bell\", \"line\\u2028sep\", \"\\U0001F680\"]\n"),
			[]byte("items:\n  - \"\\x07bell\"\n  - \"line\\u2028sep\"\n  - 🚀\n"))
	})
}

func TestConsistentFlowAllowsEmptySequence(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.SequenceStyle.Mode = "consistent"
	cfg.Rules.SequenceStyle.AllowEmptyFlow = false
	resolved, err := config.Resolve(cfg, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, diagnostics, err := Process("example.yaml", "example.yaml", []byte("first: [a]\nempty: []\n"), resolved, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}

func TestDirectivesAndDocumentMarkers(t *testing.T) {
	t.Run("paragraph separator survives zero blank line limit", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.BlankLines.MaxConsecutive = 0
		assertFix(t, cfg, []byte("# kiku:ignore-section\nkeep: YES\n\n\nchange: YES\n"), []byte("# kiku:ignore-section\nkeep: YES\n\nchange: true\n"))
	})
	t.Run("remove inline marker before multiline sequence", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.DocumentStart.Enabled = true
		assertFix(t, cfg, []byte("--- [a,\n  b]\n---\nkey: yes\n"), []byte("- a\n- b\n---\nkey: true\n"))
	})
	t.Run("add start to comment-only document", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.DocumentStart.Enabled = true
		cfg.Rules.DocumentStart.Present = true
		assertFix(t, cfg, []byte("#comment\n"), []byte("---\n# comment\n"))
	})
	t.Run("keep anchor prefix and normalize value", func(t *testing.T) {
		cfg := config.Defaults()
		assertFix(t, cfg, []byte("x: &foo YES\ny: *foo\n"), []byte("x: &foo true\ny: *foo\n"))
	})
	t.Run("sort a compact mapping and move its comments", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Rules.SortKeys.Enabled = true
		assertFix(t, cfg, []byte("seq:\n  - z: 1\n    # a note\n    a: 2\n"), []byte("seq:\n  -\n    # a note\n    a: 2\n    z: 1\n"))
	})
}

func TestFlowPairAndEmptyValue(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.NullStyle.Style = "null"
	assertFix(t, cfg, []byte("pair: [key: YES]\nflow: {k:}\n"), []byte("pair:\n  - key: true\nflow: {k: null}\n"))
}

func TestUnfixableSequenceDiagnostics(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.SequenceStyle.Mode = "hybrid"
	cfg.Rules.SequenceStyle.AllowEmptyFlow = false
	cfg.Rules.SequenceStyle.HybridMaxItems = 1
	resolved, err := config.Resolve(cfg, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("empty: []\nkeep: [a, # note\n    b]\n")
	for _, fix := range []bool{false, true} {
		out, diagnostics, err := Process("example.yaml", "example.yaml", input, resolved, fix)
		if err != nil {
			t.Fatal(err)
		}
		var lines []int
		for _, d := range diagnostics {
			if d.RuleID == "sequence-style" {
				if d.Fixable {
					t.Fatalf("unsafe diagnostic marked fixable: %+v", d)
				}
				lines = append(lines, d.Line)
			}
		}
		if len(lines) != 2 || lines[0] != 1 || lines[1] != 2 {
			t.Fatalf("sequence diagnostics: %v", lines)
		}
		if fix && !strings.Contains(string(out), "# note") {
			t.Fatalf("comment was lost: %q", out)
		}
	}
}

func TestEmptyNullInBlockMappingOnly(t *testing.T) {
	cfg := config.Defaults()
	cfg.Rules.SequenceStyle.Enabled = false
	assertFix(t, cfg, []byte("key:\n  Null\nflow: {value: null}\nseq:\n  - null\n"), []byte("key:\nflow: {value: null}\nseq:\n  - null\n"))
}

func TestValidFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/valid/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("valid fixtures: %v", err)
	}
	resolved, err := config.Resolve(config.Defaults(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			_, diags, err := Process(file, filepath.Base(file), input, resolved, false)
			if err != nil || len(diags) != 0 {
				t.Fatalf("check failed: err=%v, diagnostics=%+v", err, diags)
			}
			out, diags, err := Process(file, filepath.Base(file), input, resolved, true)
			if err != nil || len(diags) != 0 || string(out) != string(input) {
				t.Fatalf("fix altered valid fixture: err=%v, diagnostics=%+v\noutput:\n%s", err, diags, out)
			}
		})
	}
}

func TestInvalidFixtures(t *testing.T) {
	resolved, err := config.Resolve(config.Defaults(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file       string
		wantRule   string
		fixRescues bool
	}{
		{"duplicate_keys.yaml", "key-duplicates", false},
		{"syntax_error.yaml", "syntax-error", false},
		{"tabs.yaml", "no-tabs", true},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("../../testdata/invalid", tc.file)
			input, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out, diags, err := Process(path, tc.file, input, resolved, false)
			if err != nil || out != nil || len(diags) == 0 || diags[0].RuleID != tc.wantRule {
				t.Fatalf("check = (out=%v, diags=%+v, err=%v), want rule %s", out != nil, diags, err, tc.wantRule)
			}
			fixed, fixDiags, fixErr := Process(path, tc.file, input, resolved, true)
			if fixErr != nil {
				t.Fatalf("fix returned unexpected error: %v", fixErr)
			}
			if tc.fixRescues {
				if string(fixed) != "root:\n  child: value\n" {
					t.Fatalf("rescued tabs = %q", fixed)
				}
			} else if fixed != nil || len(fixDiags) == 0 || fixDiags[0].RuleID != tc.wantRule {
				t.Fatalf("unfixable Layer 1 must return nil output: fixed=%q, diags=%+v", fixed, fixDiags)
			}
		})
	}
}
