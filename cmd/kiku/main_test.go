package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/withnic/kiku/internal/parser"
)

// TestCLIProcess runs the real main entry point in the subprocess's directory.
func TestCLIProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append(os.Args[:1], os.Args[i+1:]...)
			main()
		}
	}
}

func invoke(t *testing.T, dir string, want int, args ...string) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
	cmd.Dir = dir
	// Race-instrumented binaries normally pause for one second at exit.
	cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	if code != want {
		t.Fatalf("kiku %v exited %d, want %d\nstdout: %s\nstderr: %s", args, code, want, stdout, &stderr)
	}
	return string(stdout), stderr.String()
}

func put(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckE2E(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, ".kiku.yaml", `include:
  - src
ignore:
  - skip.yaml
  - src/pruned/**
rules:
  truthy:
    ignore:
      - quiet.yaml
`)
	put(t, dir, ".kikuignore", "# comment\n\nignored.yml\nre:^src/generated-[0-9]+\\.yaml$\n")
	put(t, dir, "outside.txt", "outside: yes\n")
	put(t, dir, "src/z.YML", "z: yes  \n")
	put(t, dir, "src/a.yaml", "a: yes\n")
	for _, name := range []string{"skip.yaml", "ignored.yml", "generated-42.yaml", "one.yaml", "two.yaml", "excluded.txt"} {
		put(t, dir, "src/"+name, "broken: [\n")
	}
	put(t, dir, "src/quiet.yaml", "quiet: yes  \n")
	put(t, dir, "src/pruned/.kiku.yaml", "unknown: true\n")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(dir, filepath.Join(dir, "src", "cycle")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "src", "skip.yaml"), filepath.Join(dir, "src", "linked.yaml")); err != nil {
			t.Fatal(err)
		}
	}

	output, _ := invoke(t, dir, 1, "check", "--format", "json", "--ignore", "one.yaml", "--ignore=two.yaml")
	var diagnostics []parser.Diagnostic
	if err := json.Unmarshal([]byte(output), &diagnostics); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range diagnostics {
		if d.Line != 1 || d.Column < 1 || d.Severity != parser.SeverityWarning || !d.Fixable || d.Message == "" {
			t.Fatalf("unexpected diagnostic %+v", d)
		}
		got = append(got, d.FilePath+":"+d.RuleID)
	}
	want := "src/a.yaml:truthy src/quiet.yaml:trailing-whitespace src/z.YML:truthy src/z.YML:trailing-whitespace"
	if strings.Join(got, " ") != want {
		t.Fatalf("diagnostics = %v, want %s", got, want)
	}
	again, _ := invoke(t, dir, 1, "check", "src/z.YML", "src", "--format=json", "--ignore=one.yaml", "--ignore=two.yaml")
	if output != again {
		t.Fatalf("output order changed\n%s\n%s", output, again)
	}
	output, _ = invoke(t, dir, 1, "check", "outside.txt", "--format=github")
	if !strings.HasPrefix(output, "::warning file=outside.txt,line=1,col=") || !strings.Contains(output, ",title=truthy::") {
		t.Fatalf("annotation = %q", output)
	}

	put(t, dir, "settings.conf", "rules:\n  truthy:\n    enabled: false\n")
	output, _ = invoke(t, dir, 0, "check", "outside.txt", "--config=settings.conf", "--format=json")
	if output != "[]\n" {
		t.Fatalf("empty report = %q", output)
	}
	invoke(t, dir, 2, "check", "missing.yaml")
	invoke(t, dir, 2, "check", "--config=missing.conf", "outside.txt")
	invoke(t, dir, 2, "check", "--ignore=re:[", "outside.txt")
	invoke(t, dir, 2, "check", "--format=unknown", "outside.txt")
	invoke(t, dir, 0, "check", "src/pruned")

	invoke(t, dir, 0, "fix", "src", "--ignore=one.yaml", "--ignore=two.yaml")
	if got := read(t, filepath.Join(dir, "src", "quiet.yaml")); got != "quiet: yes\n" {
		t.Fatalf("rule-specific ignore during fix: %q", got)
	}
	if got := read(t, filepath.Join(dir, "src", "z.YML")); got != "z: true\n" {
		t.Fatalf("truthy with trailing whitespace before LF: %q", got)
	}
	invoke(t, dir, 0, "check", "src", "--ignore=one.yaml", "--ignore=two.yaml")
}

func TestNearestConfigE2E(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, ".kiku.yaml", "include: []\n")
	put(t, dir, "nested/.kiku.yaml", "rules:\n  truthy:\n    enabled: false\n")
	put(t, dir, "nested/sample.yaml", "ready: yes\n")
	invoke(t, dir, 0, "check")
	invoke(t, dir, 1, "check", "nested/sample.yaml", "--config=.kiku.yaml")
}

func TestFixE2E(t *testing.T) {
	dir := t.TempDir()
	const original = "# 日本語 🌸\nready: yes  "
	const fixed = "# 日本語 🌸\nready: true\n"
	path := put(t, dir, "good.yaml", original)
	output, _ := invoke(t, dir, 0, "fix", "good.yaml", "--stdout")
	if output != fixed || read(t, path) != original {
		t.Fatalf("stdout = %q, original = %q", output, read(t, path))
	}
	output, _ = invoke(t, dir, 0, "fix", "--diff", "good.yaml")
	want := "--- a/good.yaml\n+++ b/good.yaml\n@@ -1,2 +1,2 @@\n # 日本語 🌸\n-ready: yes  \n\\ No newline at end of file\n+ready: true\n"
	if output != want || read(t, path) != original {
		t.Fatalf("diff = %q, original = %q", output, read(t, path))
	}
	invoke(t, dir, 2, "fix", "--diff", "--stdout", "good.yaml")
	put(t, dir, "empty.yaml", "")
	output, _ = invoke(t, dir, 0, "fix", "--stdout", "empty.yaml")
	if output != "" {
		t.Fatalf("empty file stdout = %q", output)
	}
	invoke(t, dir, 2, "fix", "--stdout", ".")
	var invalid []struct {
		path, contents string
		info           os.FileInfo
	}
	for name, contents := range map[string]string{
		"syntax_error.yaml":   read(t, filepath.Join("..", "..", "testdata", "invalid", "syntax_error.yaml")),
		"duplicate_keys.yaml": read(t, filepath.Join("..", "..", "testdata", "invalid", "duplicate_keys.yaml")),
		"invalid-utf8.yaml":   "key: \xff\n",
	} {
		path := put(t, dir, name, contents)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		invalid = append(invalid, struct {
			path, contents string
			info           os.FileInfo
		}{path, contents, info})
	}
	_, stderr := invoke(t, dir, 1, "fix", ".")
	for _, rule := range []string{"syntax-error", "key-duplicates", "invalid-utf8"} {
		if !strings.Contains(stderr, "("+rule+")") {
			t.Fatalf("missing %s diagnostic in %q", rule, stderr)
		}
	}
	if got := read(t, path); got != fixed {
		t.Fatalf("valid file fixed to %q", got)
	}
	for _, entry := range invalid {
		info, err := os.Stat(entry.path)
		if err != nil {
			t.Fatal(err)
		}
		if read(t, entry.path) != entry.contents || !info.ModTime().Equal(entry.info.ModTime()) || !os.SameFile(entry.info, info) {
			t.Errorf("invalid file was modified: %s", entry.path)
		}
	}
	output, _ = invoke(t, dir, 0, "fix", "--diff", "good.yaml")
	if output != "" {
		t.Fatalf("unchanged file diff = %q", output)
	}
}

func TestFixLineEndingsE2E(t *testing.T) {
	for _, tc := range []struct{ name, rules, from, to string }{
		{"to LF", "", "\r\n", "\n"},
		{"to CRLF", "    type: crlf\n", "\n", "\r\n"},
		{"disabled", "    enabled: false\n", "\r\n", "\r\n"},
		{"ignored", "    ignore: [sample.yaml]\n", "\r\n", "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.rules != "" {
				put(t, dir, ".kiku.yaml", "rules:\n  line_endings:\n"+tc.rules)
			}
			path := put(t, dir, "sample.yaml", "ready: yes"+tc.from+"value: 1"+tc.from)
			invoke(t, dir, 0, "fix", "sample.yaml")
			want := "ready: true" + tc.to + "value: 1" + tc.to
			if got := read(t, path); got != want {
				t.Fatalf("fixed bytes = %q, want %q", got, want)
			}
		})
	}
}

func TestInitE2E(t *testing.T) {
	dir := t.TempDir()
	invoke(t, dir, 0, "init")
	path := filepath.Join(dir, ".kiku.yaml")
	template := read(t, path)
	if !strings.HasPrefix(template, "# ") || !strings.Contains(template, "sequence_style:") {
		t.Fatalf("unexpected template %q", template)
	}
	invoke(t, dir, 0, "check")
	put(t, dir, ".kiku.yaml", "# custom\n")
	invoke(t, dir, 2, "init")
	if got := read(t, path); got != "# custom\n" {
		t.Fatalf("existing configuration = %q", got)
	}
	invoke(t, dir, 0, "init", "--force")
	if got := read(t, path); got != template {
		t.Fatalf("forced configuration differs from template")
	}
	output, _ := invoke(t, dir, 0, "version")
	if !strings.HasPrefix(output, "kiku v") {
		t.Fatalf("version = %q", output)
	}
}
