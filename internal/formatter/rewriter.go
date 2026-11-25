package formatter

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/token"
)

// Source translates AST rune columns to byte spans in normalized source.
type Source struct {
	Text  string
	lines []int
}

func NewSource(text string) Source {
	s := Source{Text: text, lines: []int{0}}
	for i, c := range text {
		if c == '\n' {
			s.lines = append(s.lines, i+1)
		}
	}
	return s
}

func (s Source) LineStart(line int) int {
	if line < 1 {
		return 0
	}
	if line > len(s.lines) {
		return len(s.Text)
	}
	return s.lines[line-1]
}

func (s Source) Offset(t *token.Token) int {
	lineStart := s.LineStart(t.Position.Line)
	pos := lineStart
	end := s.LineStart(t.Position.Line + 1)
	for col := 1; col < t.Position.Column && pos < end; col++ {
		_, width := utf8.DecodeRuneInString(s.Text[pos:end])
		pos += width
	}
	// The lexer can advance a plain scalar's reported start by the
	// trailing whitespace it discarded before a newline.
	if t.Type == token.StringType || t.Type == token.BoolType {
		raw := strings.Trim(t.Origin, " \t\r\n")
		line := strings.TrimSuffix(s.Text[lineStart:end], "\n")
		trimmed := strings.TrimRight(line, " \t")
		trailing := len(line) - len(trimmed)
		start := pos - trailing
		if trailing > 0 && raw != "" && !strings.Contains(raw, "\n") &&
			!strings.HasPrefix(s.Text[pos:end], raw) &&
			start >= lineStart && start+len(raw) == lineStart+len(trimmed) &&
			strings.HasSuffix(trimmed, raw) {
			return start
		}
	}
	return pos
}

// TokenEnd locates the byte end of t in normalized source. Quoted tokens are
// scanned directly because go-yaml omits hex digits from Origin for \x/\u/\U escapes.
func (s Source) TokenEnd(t *token.Token) int {
	start := s.Offset(t)
	if t.Type == token.ImplicitNullType {
		return start
	}
	if t.Type == token.DoubleQuoteType && start < len(s.Text) && s.Text[start] == '"' {
		for i := start + 1; i < len(s.Text); i++ {
			switch s.Text[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
	}
	if t.Type == token.SingleQuoteType && start < len(s.Text) && s.Text[start] == '\'' {
		for i := start + 1; i < len(s.Text); i++ {
			if s.Text[i] == '\'' {
				if i+1 < len(s.Text) && s.Text[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
	}
	raw := strings.Trim(t.Origin, " \t\n\r")
	if raw != "" && strings.HasPrefix(s.Text[start:], raw) {
		return start + len(raw)
	}
	if strings.HasPrefix(s.Text[start:], t.Value) {
		return start + len(t.Value)
	}
	return start
}

type Edit struct {
	Start, End int
	Text       string
}

type Rewriter struct {
	Source
	edits []Edit
	Err   error
}

func NewRewriter(text string) *Rewriter {
	return &Rewriter{Source: NewSource(text)}
}

// Replace folds already-rendered child edits into their parent's replacement.
// The caller passes disjoint or nested spans in bottom-up order.
func (r *Rewriter) Replace(start, end int, text string) {
	if r.Err != nil {
		return
	}
	if start < 0 || end < start || end > len(r.Text) {
		r.Err = fmt.Errorf("invalid source span [%d, %d)", start, end)
		return
	}
	i, _ := slices.BinarySearchFunc(r.edits, start, func(e Edit, at int) int { return e.Start - at })
	if i > 0 && r.edits[i-1].End > start {
		r.Err = fmt.Errorf("source edits overlap at byte %d", start)
		return
	}
	j := i
	for j < len(r.edits) && r.edits[j].Start < end && r.edits[j].End <= end {
		j++
	}
	if j < len(r.edits) && r.edits[j].Start < end {
		r.Err = fmt.Errorf("source edits overlap at byte %d", end)
		return
	}
	r.edits = slices.Replace(r.edits, i, j, Edit{start, end, text})
}

// CoverEnd extends a source range to include edits that cross its end.
func (r *Rewriter) CoverEnd(start, end int) int {
	for _, edit := range r.edits {
		if edit.Start >= start && edit.Start < end {
			end = max(end, edit.End)
		}
	}
	return end
}

func (r *Rewriter) Render(start, end int) string {
	return r.render(start, end, nil, "\n")
}

func (r *Rewriter) Output(endings []string, fallback string) string {
	return r.render(0, len(r.Text), endings, fallback)
}

func (r *Rewriter) render(start, end int, endings []string, fallback string) string {
	var out strings.Builder
	write := func(text string, at int, original bool) {
		if fallback == "\n" && len(endings) == 0 {
			out.WriteString(text)
			return
		}
		line, found := slices.BinarySearch(r.lines, at)
		if !found {
			line--
		}
		for {
			before, after, ok := strings.Cut(text, "\n")
			out.WriteString(before)
			if !ok {
				break
			}
			if original && line < len(endings) {
				out.WriteString(endings[line])
			} else {
				out.WriteString(fallback)
			}
			line++
			text = after
		}
	}
	i, _ := slices.BinarySearchFunc(r.edits, start, func(e Edit, at int) int { return e.Start - at })
	for _, edit := range r.edits[i:] {
		if edit.Start > end || edit.End > end || (edit.Start == end && end != len(r.Text)) {
			break
		}
		if edit.Start < start {
			continue
		}
		write(r.Text[start:edit.Start], start, true)
		write(edit.Text, edit.Start, false)
		start = edit.End
	}
	write(r.Text[start:end], start, true)
	return out.String()
}
