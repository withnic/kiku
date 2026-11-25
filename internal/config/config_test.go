package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConfigTestFile(t *testing.T, name, text string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".kiku.yaml")
	writeConfigTestFile(t, path, `include: [src]
rules:
  line_length:
    max: 40
    ignore: ["**/generated/*.yaml"]
  indentation:
    enabled: false
  blank_lines:
    max_consecutive: 0
`)
	writeConfigTestFile(t, filepath.Join(root, "kiku.yaml"), "version: 7\n")
	writeConfigTestFile(t, filepath.Join(root, ".kikuignore"), " # comment\r\n\r\nre:^out/\r\n*.tmp\r\n")
	loader, err := NewLoader(root, "", []string{"cache/**"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loader.Load(child)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != path || !reflect.DeepEqual(cfg.Include, []string{"src"}) ||
		cfg.Rules.LineLength.Max != 40 || !cfg.Rules.LineLength.IgnoreURLs ||
		cfg.Rules.Indentation.Enabled || cfg.Rules.Indentation.Spaces != 2 || cfg.Rules.BlankLines.MaxConsecutive != 0 {
		t.Fatalf("bad overrides %+v", cfg)
	}
	for _, p := range []string{"out/a", `dir\x.tmp`, "cache", "vendor/a"} {
		if !cfg.Matcher.Match(p) {
			t.Errorf("expected ignore for %q", p)
		}
	}
	if cfg.Matcher.Match("src/a.yaml") || cfg.RuleEnabled("line-length", "generated/x.yaml") ||
		!cfg.RuleEnabled("line-length", "src/a.yaml") || !cfg.RuleEnabled("encoding", "generated/x.yaml") {
		t.Fatal("incorrect filtering")
	}
	if got, err := loader.Load(root); err != nil || got != cfg {
		t.Fatalf("cached result %p, %v", got, err)
	}
	explicit, err := NewLoader(root, "kiku.yaml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := explicit.Load(child); err == nil {
		t.Fatal("explicit configuration was bypassed")
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, tc := range []string{
		"other: false", "rules: {unknown: {}}", "rules: {line_length: {x: 1}}",
		"rules: {comments: {min_spaces_from_content: 0}}",
		"rules: {quotes: {prefer: fancy}}", "rules: {quotes: {unnecessary: nope}}",
		"rules: {truthy: {style: upper}}", "rules: {null_style: {style: nope}}",
		"rules: {line_endings: {type: cr}}", "rules: {line_length: {max: 0}}",
		"rules: {blank_lines: {max_consecutive: -1}}", "rules: {indentation: {spaces: 9}}",
		"rules: {sequence_style: {mode: nope}}", "rules: {sequence_style: {hybrid_max_items: 0}}",
		"rules: {sequence_style: {hybrid_max_line_length: 0}}",
		"version: 0", "version: 1\n---\nversion: 1", "version: 1\nversion: 1",
		"ignore: ['re:[']", "rules: {line_length: {ignore: ['re:(']}}",
		"version: null", "version: 1.5", "rules: {indentation: {spaces: '4'}}",
	} {
		t.Run(tc, func(t *testing.T) {
			dir := t.TempDir()
			writeConfigTestFile(t, filepath.Join(dir, ".kiku.yaml"), tc+"\n")
			l, err := NewLoader(dir, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.Load(""); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestIgnoreWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfigTestFile(t, filepath.Join(dir, ".kikuignore"), "re:[\n")
	l, err := NewLoader(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Load(""); err == nil {
		t.Fatal("invalid .kikuignore accepted")
	}
}
