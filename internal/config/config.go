package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/withnic/kiku/internal/fs"
)

const ErrorExitCode = 2

type Config struct {
	Version int      `yaml:"version"`
	Include []string `yaml:"include"`
	Ignore  []string `yaml:"ignore"`
	Rules   Rules    `yaml:"rules"`
}

func (c Config) Validate() error {
	r := c.Rules
	checks := []struct {
		field string
		valid bool
	}{
		{"version", c.Version == 1},
		{"rules.line_endings.type", slices.Contains([]string{"lf", "crlf"}, r.LineEndings.Type)},
		{"rules.line_length.max", r.LineLength.Max >= 1},
		{"rules.blank_lines.max_consecutive", r.BlankLines.MaxConsecutive >= 0},
		{"rules.indentation.spaces", r.Indentation.Spaces >= 1 && r.Indentation.Spaces <= 8},
		{"rules.sequence_style.mode", slices.Contains([]string{"block", "flow", "consistent", "hybrid"}, r.SequenceStyle.Mode)},
		{"rules.sequence_style.hybrid_max_items", r.SequenceStyle.HybridMaxItems >= 1},
		{"rules.sequence_style.hybrid_max_line_length", r.SequenceStyle.HybridMaxLineLength >= 1},
		{"rules.quotes.prefer", slices.Contains([]string{"double", "single"}, r.Quotes.Prefer)},
		{"rules.quotes.unnecessary", slices.Contains([]string{"remove", "keep"}, r.Quotes.Unnecessary)},
		{"rules.truthy.style", r.Truthy.Style == "lower"},
		{"rules.null_style.style", slices.Contains([]string{"empty", "null", "tilde"}, r.NullStyle.Style)},
		{"rules.comments.min_spaces_from_content", r.Comments.MinSpacesFromContent >= 1},
	}
	for _, check := range checks {
		if !check.valid {
			return fmt.Errorf("%s: invalid value", check.field)
		}
	}
	return nil
}

// Resolved is a configuration snapshot shared by workers. Treat it as read-only.
type Resolved struct {
	Config
	Path    string
	BaseDir string
	Matcher PathMatcher
	rules   map[string]ruleFilter
}

type ruleFilter struct {
	enabled bool
	matcher PathMatcher
}

func (r *Resolved) RuleEnabled(id, relativePath string) bool {
	filter, ok := r.rules[id]
	return ok && filter.enabled && !filter.matcher.Match(relativePath)
}

func (r *Resolved) RelativePath(absolutePath string) (string, error) {
	return fs.RelativePath(r.BaseDir, absolutePath)
}

func Resolve(c Config, baseDir string, extraIgnore []string) (*Resolved, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	c.Ignore = append(slices.Clone(c.Ignore), extraIgnore...)
	global, err := NewPathMatcher(c.Ignore)
	if err != nil {
		return nil, fmt.Errorf("ignore: %w", err)
	}
	result := &Resolved{Config: c, BaseDir: baseDir, Matcher: global, rules: make(map[string]ruleFilter)}
	for _, rule := range c.Rules.options() {
		matcher, err := NewPathMatcher(rule.options.Ignore)
		if err != nil {
			return nil, fmt.Errorf("rules.%s.ignore: %w", strings.ReplaceAll(rule.id, "-", "_"), err)
		}
		result.rules[rule.id] = ruleFilter{rule.options.Enabled, matcher}
	}
	return result, nil
}

type Loader struct {
	cwd, explicit string
	extra         []string
	mu            sync.Mutex
	search        map[string]string
	loaded        map[string]*Resolved
}

func NewLoader(cwd, explicit string, cliIgnore []string) (*Loader, error) {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	if explicit != "" && !filepath.IsAbs(explicit) {
		explicit = filepath.Join(cwd, explicit)
	}
	if _, err := NewPathMatcher(cliIgnore); err != nil {
		return nil, err
	}
	return &Loader{cwd: cwd, explicit: explicit, extra: slices.Clone(cliIgnore),
		search: make(map[string]string), loaded: make(map[string]*Resolved)}, nil
}

// Load resolves the nearest configuration starting at dir. An empty dir uses the loader's cwd.
func (l *Loader) Load(dir string) (*Resolved, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := l.explicit
	if path == "" {
		if dir == "" {
			dir = l.cwd
		} else if !filepath.IsAbs(dir) {
			dir = filepath.Join(l.cwd, dir)
		}
		var err error
		path, err = l.find(filepath.Clean(dir))
		if err != nil {
			return nil, err
		}
	}
	if cached, ok := l.loaded[path]; ok {
		return cached, nil
	}
	base := l.cwd
	c := Defaults()
	if path != "" {
		base = filepath.Dir(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := decode(raw, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	patterns, err := readIgnore(filepath.Join(base, ".kikuignore"))
	if err != nil {
		return nil, err
	}
	c.Ignore = append(c.Ignore, patterns...)
	result, err := Resolve(c, base, l.extra)
	if err != nil {
		return nil, fmt.Errorf("configuration in %s: %w", base, err)
	}
	result.Path = path
	l.loaded[path] = result
	return result, nil
}

func (l *Loader) find(dir string) (string, error) {
	var visited []string
	var found string
	for {
		if cached, ok := l.search[dir]; ok {
			found = cached
			break
		}
		visited = append(visited, dir)
		for _, name := range []string{".kiku.yaml", "kiku.yaml"} {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			switch {
			case err == nil:
				if !info.Mode().IsRegular() {
					return "", fmt.Errorf("%s: expected regular configuration file", path)
				}
				found = path
			case !errors.Is(err, os.ErrNotExist):
				return "", err
			}
			if found != "" {
				break
			}
		}
		parent := filepath.Dir(dir)
		if found != "" || parent == dir {
			break
		}
		dir = parent
	}
	for _, dir := range visited {
		l.search[dir] = found
	}
	return found, nil
}

func decode(data []byte, c *Config) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	tree, err := yamlparser.ParseBytes(data, 0)
	if err != nil {
		return err
	}
	if len(tree.Docs) == 0 {
		return nil
	}
	if len(tree.Docs) != 1 {
		return errors.New("expected a single configuration document")
	}
	if tree.Docs[0].Body == nil {
		return nil
	}
	return mergeFields(reflect.ValueOf(c).Elem(), tree.Docs[0].Body, "")
}

// Merge only fields present in YAML. The AST preserves scalar types, which
// reflection-based YAML decoders can coerce (for example 1.5 to an integer).
func mergeFields(dst reflect.Value, node ast.Node, field string) error {
	switch dst.Kind() {
	case reflect.Struct:
		if mapping, ok := node.(ast.MapNode); ok {
			entries := mapping.MapRange()
			for entries.Next() {
				key, ok := entries.Key().(*ast.StringNode)
				if !ok {
					return fmt.Errorf("%s: expected a string field name", field)
				}
				name := key.Value
				if field != "" {
					name = field + "." + name
				}
				value, ok := schemaField(dst, key.Value)
				if !ok {
					return fmt.Errorf("unknown field %s", name)
				}
				if err := mergeFields(value, entries.Value(), name); err != nil {
					return err
				}
			}
			return nil
		}
	case reflect.Bool:
		if scalar, ok := node.(*ast.BoolNode); ok {
			dst.SetBool(scalar.Value)
			return nil
		}
	case reflect.Int:
		if scalar, ok := node.(*ast.IntegerNode); ok {
			switch value := scalar.Value.(type) {
			case int64:
				if !dst.OverflowInt(value) {
					dst.SetInt(value)
					return nil
				}
			case uint64:
				if int64(value) >= 0 && !dst.OverflowInt(int64(value)) {
					dst.SetInt(int64(value))
					return nil
				}
			}
		}
	case reflect.String:
		if scalar, ok := node.(*ast.StringNode); ok {
			dst.SetString(scalar.Value)
			return nil
		}
	case reflect.Slice:
		if seq, ok := node.(*ast.SequenceNode); ok {
			values := reflect.MakeSlice(dst.Type(), len(seq.Values), len(seq.Values))
			for i, item := range seq.Values {
				if err := mergeFields(values.Index(i), item, fmt.Sprintf("%s[%d]", field, i)); err != nil {
					return err
				}
			}
			dst.Set(values)
			return nil
		}
	}
	if field == "" {
		field = "configuration"
	}
	return fmt.Errorf("%s: expected %s, got %s", field, dst.Type(), node.Type())
}

func schemaField(value reflect.Value, key string) (reflect.Value, bool) {
	for i := 0; i < value.NumField(); i++ {
		switch value.Type().Field(i).Tag.Get("yaml") {
		case key:
			return value.Field(i), true
		case ",inline":
			if nested, ok := schemaField(value.Field(i), key); ok {
				return nested, true
			}
		}
	}
	return reflect.Value{}, false
}

func readIgnore(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: invalid UTF-8", path)
	}
	text, _ := fs.NormalizeEOL(strings.TrimPrefix(string(data), "\ufeff"))
	var patterns []string
	for _, line := range strings.Split(text, "\n") {
		pattern := strings.TrimSpace(line)
		if pattern != "" && !strings.HasPrefix(pattern, "#") {
			patterns = append(patterns, pattern)
		}
	}
	return patterns, nil
}
