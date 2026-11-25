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
	pos := s.LineStart(t.Position.Line)
	end := s.LineStart(t.Position.Line + 1)
	for col := 1; col < t.Position.Column && pos < end; col++ {
		_, width := utf8.DecodeRuneInString(s.Text[pos:end])
		pos += width
	}
	return pos
}

// TokenEnd uses Origin for scalar spelling, including escaped quote content.
func (s Source) TokenEnd(t *token.Token) int {
	start := s.Offset(t)
	if t.Type == token.ImplicitNullType {
		return start
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
