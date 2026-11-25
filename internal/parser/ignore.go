package parser

import (
	"strings"
	"unicode"

	"github.com/goccy/go-yaml/token"
)

type IgnoreMap struct {
	lines        []map[string]bool
	control      []bool
	paragraphEnd []bool
}

func (m *IgnoreMap) Ignored(rule string, line int) bool {
	switch rule {
	case "invalid-utf8", "no-tabs", "syntax-error", "key-duplicates":
		return false
	}
	if m == nil || line < 1 || line > len(m.lines) {
		return false
	}
	rules := m.lines[line-1]
	if ignored, ok := rules[rule]; ok {
		return ignored
	}
	return rules["*"]
}

func (m *IgnoreMap) ControlLine(line int) bool {
	return m != nil && line >= 1 && line <= len(m.control) && m.control[line-1]
}

// ParagraphEnd reports a blank line that terminates an ignore section.
func (m *IgnoreMap) ParagraphEnd(line int) bool {
	return m != nil && line >= 1 && line <= len(m.paragraphEnd) && m.paragraphEnd[line-1]
}

func buildIgnoreMap(lines []string, tokens token.Tokens, ends, inlineEnds map[int]int) *IgnoreMap {
	result := &IgnoreMap{lines: make([]map[string]bool, len(lines)), control: make([]bool, len(lines)), paragraphEnd: make([]bool, len(lines))}
	type directive struct {
		action string
		rules  []string
		inline bool
	}
	directives := make(map[int]directive)
	for _, tk := range tokens {
		if tk.Type != token.CommentType {
			continue
		}
		line := tk.Position.Line
		if line < 1 || line > len(lines) {
			continue
		}
		body, _, _ := strings.Cut(strings.TrimSpace(tk.Value), "--")
		parts := strings.FieldsFunc(body, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		if len(parts) == 0 {
			continue
		}
		action := parts[0]
		switch action {
		case "kiku-disable", "kiku:disable":
			action = "disable"
		case "kiku-enable", "kiku:enable":
			action = "enable"
		case "kiku:ignore":
			action = "ignore"
		case "kiku-disable-next-line":
			action = "next"
		case "kiku:ignore-section", "kiku:ignore-paragraph":
			action = "paragraph"
		default:
			continue
		}
		var rules []string
		for _, rule := range parts[1:] {
			if rule == "*" {
				rules = nil
				break
			}
			rules = append(rules, strings.ReplaceAll(rule, "_", "-"))
		}
		// An actual comment token can occur after a scalar on the same line.
		inline := !strings.HasPrefix(strings.TrimLeft(lines[line-1], " \t"), "#")
		directives[line] = directive{action, rules, inline}
		result.control[line-1] = true
	}

	add := func(start, end int, rules []string) {
		for line := start; line <= end && line <= len(lines); line++ {
			if result.lines[line-1] == nil {
				result.lines[line-1] = make(map[string]bool)
			}
			for _, rule := range rules {
				result.lines[line-1][rule] = true
			}
		}
	}
	active := make(map[string]bool)
	for line := 1; line <= len(lines); line++ {
		d := directives[line]
		if len(d.rules) == 0 {
			d.rules = []string{"*"}
		}
		switch d.action {
		case "enable":
			for _, rule := range d.rules {
				if rule == "*" {
					clear(active)
				} else {
					active[rule] = false
				}
			}
		case "disable":
			for _, rule := range d.rules {
				if rule == "*" {
					clear(active)
				}
				active[rule] = true
			}
		case "ignore", "next":
			if d.action == "ignore" && d.inline {
				add(line, max(line, inlineEnds[line]), d.rules)
			} else {
				start := line + 1
				add(start, max(start, ends[start]), d.rules)
			}
		case "paragraph":
			end := line
			for end < len(lines) {
				next := lines[end]
				if strings.TrimSpace(next) == "" {
					result.paragraphEnd[end] = true
					break
				}
				if documentBoundary(next) {
					break
				}
				end++
			}
			add(line, end, d.rules)
		}
		if len(active) != 0 {
			rules := result.lines[line-1]
			if rules == nil {
				rules = make(map[string]bool)
				result.lines[line-1] = rules
			}
			if !rules["*"] {
				for rule, ignored := range active {
					if _, local := rules[rule]; !local {
						rules[rule] = ignored
					}
				}
			}
		}
	}
	return result
}

func documentBoundary(line string) bool {
	for _, marker := range []string{"---", "..."} {
		if rest, ok := strings.CutPrefix(line, marker); ok &&
			(rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			return true
		}
	}
	return false
}
