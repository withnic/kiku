package rules

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
	"github.com/withnic/kiku/internal/config"
	"github.com/withnic/kiku/internal/formatter"
	"github.com/withnic/kiku/internal/fs"
	"github.com/withnic/kiku/internal/parser"
)

type StructureResult struct {
	Err         error
	Diagnostics []parser.Diagnostic
	Output      []byte
}

type structurePlan struct {
	doc *parser.Document
	config.Rules
	*formatter.Rewriter
	enabled     map[string]bool
	diagnostics []parser.Diagnostic
	comments    map[int]*token.Token
	bindings    map[int]bool
	indent      map[int]int
	mode        string
	nodeEnds    map[ast.Node]int
	sortedMaps  map[*ast.MappingNode][]*ast.MappingValueNode
}

func Structure(doc *parser.Document, cfg *config.Resolved, path string, fix bool) StructureResult {
	result := StructureResult{Output: doc.RawBytes}
	if doc.AST == nil {
		return result
	}
	p := structurePlan{
		doc: doc, Rules: cfg.Rules, Rewriter: formatter.NewRewriter(doc.NormalizedText),
		enabled: make(map[string]bool), comments: make(map[int]*token.Token),
		bindings: make(map[int]bool), indent: make(map[int]int),
		nodeEnds: make(map[ast.Node]int), sortedMaps: make(map[*ast.MappingNode][]*ast.MappingValueNode),
	}
	for _, rule := range []string{"sequence-style", "quotes", "truthy", "null-style", "indentation", "comments", "document-start", "sort-keys"} {
		p.enabled[rule] = cfg.RuleEnabled(rule, path)
	}
	for _, t := range lexer.Tokenize(p.Text) {
		switch t.Type {
		case token.CommentType:
			p.comments[t.Position.Line] = t
		case token.AnchorType, token.AliasType:
			p.bindings[t.Position.Line] = true
		}
	}
	// Comment spacing is planned before the structural spans that carry it.
	if p.enabled["comments"] {
		p.commentStyle()
	}
	for _, d := range doc.AST.Docs {
		p.mode = p.SequenceStyle.Mode
		p.walk(d.Body, placement{parent: d.Start}, 0, 0)
	}
	if p.enabled["document-start"] {
		p.documentStart()
	}
	result.Diagnostics = p.diagnostics
	result.Err = p.Err
	if fix && p.Err == nil {
		eol := fs.OutputEOL(doc.OriginalEOL, "", false)
		text := p.Output(fs.LineEndings(string(doc.RawBytes)), eol)
		if doc.HadBOM {
			text = "\ufeff" + text
		}
		result.Output = []byte(text)
	}
	return result
}

func (p *structurePlan) allowed(id string, line int) bool {
	return p.enabled[id] && !p.doc.IgnoreMap.Ignored(id, line) && !p.doc.Protected(line)
}

// A moved multiline span uses the prevailing ending for generated lines.
func (p *structurePlan) hasIgnoredMixedEOL(line int) bool {
	return p.doc.OriginalEOL == "mixed" && p.doc.IgnoreMap.Ignored("line-endings", line)
}

func (p *structurePlan) warn(id string, t *token.Token, fixable bool, message string) {
	p.diagnostics = append(p.diagnostics, parser.Diagnostic{
		FilePath: p.doc.Path, Line: t.Position.Line, Column: t.Position.Column,
		RuleID: id, Severity: parser.SeverityWarning, Fixable: fixable, Message: message,
	})
}

type placement struct {
	parent                     *token.Token
	flow, key, mapping, tagged bool
}

func (p *structurePlan) first(n ast.Node) *token.Token {
	if m, ok := n.(*ast.MappingNode); ok && (m.Start == nil || m.Start.Type != token.MappingStartType) && len(m.Values) > 0 {
		return m.Values[0].Key.GetToken()
	}
	return n.GetToken()
}

func (p *structurePlan) end(n ast.Node) int {
	if end, ok := p.nodeEnds[n]; ok {
		return end
	}
	var end int
	switch n := n.(type) {
	case *ast.MappingNode:
		if n.IsFlowStyle && n.End != nil {
			end = p.TokenEnd(n.End)
		} else if len(n.Values) > 0 {
			end = p.end(n.Values[len(n.Values)-1])
		}
	case *ast.MappingValueNode:
		end = max(p.TokenEnd(n.Start), p.end(n.Value))
	case *ast.SequenceNode:
		if n.IsFlowStyle && n.End != nil {
			end = p.TokenEnd(n.End)
		} else if len(n.Values) > 0 {
			end = p.end(n.Values[len(n.Values)-1])
		}
	case *ast.AnchorNode:
		end = p.end(n.Value)
	case *ast.AliasNode:
		end = p.end(n.Value)
	case *ast.TagNode:
		end = p.end(n.Value)
	case *ast.LiteralNode:
		line := n.Start.Position.Line + 1
		for p.doc.Protected(line) {
			line++
		}
		end = p.LineStart(line)
	default:
		end = p.TokenEnd(n.GetToken())
	}
	p.nodeEnds[n] = end
	return end
}

func (p *structurePlan) indentToken(t *token.Token, desired int) int {
	line := t.Position.Line
	if n, ok := p.indent[line]; ok {
		return n
	}
	start := p.LineStart(line)
	at := p.Offset(t)
	raw := p.Text[start:at]
	actual := len(raw) - len(strings.TrimLeft(raw, " "))
	if actual != len(raw) {
		return actual
	}
	if !p.allowed("indentation", line) {
		desired = actual
	}
	p.indent[line] = desired
	if actual != desired {
		p.warn("indentation", t, true, "unexpected indentation")
		p.Replace(start, at, spaces(desired))
		for previous := line - 1; previous >= 1; previous-- {
			comment := p.comments[previous]
			if comment == nil || !p.allowed("indentation", previous) ||
				p.doc.IgnoreMap.ControlLine(previous) || comment.Position.Column != actual+1 {
				break
			}
			from, to := p.LineStart(previous), p.Offset(comment)
			if strings.TrimSpace(p.Text[from:to]) != "" {
				break
			}
			p.Replace(from, to, spaces(desired))
		}
	}
	return desired
}

func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }

func (p *structurePlan) walk(n ast.Node, where placement, desired, depth int) {
	if n == nil || n.Type() == ast.CommentType || n.Type() == ast.DirectiveType {
		return
	}
	t := p.first(n)
	line := t.Position.Line
	if line < 1 || line > len(p.doc.Lines) {
		return
	}
	actual := len(p.doc.Lines[line-1]) - len(strings.TrimLeft(p.doc.Lines[line-1], " "))
	switch n := n.(type) {
	case *ast.MappingNode:
		if !n.IsFlowStyle {
			for _, entry := range n.Values {
				if p.doc.IgnoreMap.Ignored("indentation", entry.Key.GetToken().Position.Line) {
					desired = t.Position.Column - 1
					break
				}
			}
		}
	case *ast.SequenceNode:
		if !n.IsFlowStyle {
			for _, entry := range n.Entries {
				if p.doc.IgnoreMap.Ignored("indentation", entry.Start.Position.Line) {
					desired = t.Position.Column - 1
					break
				}
			}
		}
	}
	indent := desired
	inline := where.parent != nil && where.parent.Position.Line == line
	if !inline {
		indent = p.indentToken(t, desired)
	}
	if inline && where.parent.Type == token.SequenceEntryType && !where.tagged {
		// A compact block collection starts after the sequence indicator.
		indent = desired
	}
	switch n := n.(type) {
	case *ast.MappingNode:
		for _, entry := range n.Values {
			key := entry.Key.GetToken()
			entryIndent := indent
			if n.IsFlowStyle && key.Position.Line != line {
				entryIndent += p.Indentation.Spaces
			}
			if key.Position.Line != line {
				p.indentToken(key, entryIndent)
			}
			p.walk(entry.Key, placement{parent: key, flow: where.flow || n.IsFlowStyle, key: true}, entryIndent, depth+1)
			childIndent := entryIndent + p.Indentation.Spaces
			p.walk(entry.Value, placement{parent: entry.Start, flow: where.flow || n.IsFlowStyle, mapping: true}, childIndent, depth+1)
		}
		if n.IsFlowStyle && n.End != nil && n.End.Position.Line != line {
			p.indentToken(n.End, indent)
		}
		p.sortMapping(n, depth)
	case *ast.SequenceNode:
		if p.mode == "consistent" && len(n.Values) > 0 {
			p.mode = "block"
			if n.IsFlowStyle {
				p.mode = "flow"
			}
		}
		// Flow children stay in flow context until their enclosing span can
		// legally introduce block syntax.
		for i, child := range n.Values {
			parent := n.Start
			childIndent := indent + p.Indentation.Spaces
			if !n.IsFlowStyle {
				parent = n.Entries[i].Start
				p.indentToken(parent, indent)
				if parent.Position.Line == p.first(child).Position.Line {
					childIndent = indent + p.Offset(p.first(child)) - p.Offset(parent)
				}
			}
			p.walk(child, placement{parent: parent, flow: n.IsFlowStyle}, childIndent, depth+1)
		}
		if n.IsFlowStyle && n.End != nil && n.End.Position.Line != line {
			p.indentToken(n.End, indent)
		}
		p.sequence(n, where, indent)
	case *ast.AnchorNode:
		if n.Value != nil {
			p.walk(n.Value, placement{parent: n.Start, flow: where.flow, mapping: where.mapping, tagged: true}, indent, depth)
		}
	case *ast.MappingKeyNode:
		p.walk(n.Value, placement{parent: t, flow: where.flow, key: true, tagged: true}, indent, depth)
	case *ast.TagNode:
		// Explicit tags determine scalar types independently of spelling.
	case *ast.LiteralNode:
		if target, ok := p.indent[line]; ok {
			p.shiftLiteral(n, target-actual)
		}
	default:
		p.scalar(n, where)
	}
}

func (p *structurePlan) shiftLiteral(n *ast.LiteralNode, delta int) {
	for line := n.Start.Position.Line + 1; delta != 0 && p.doc.Protected(line); line++ {
		content := p.doc.Lines[line-1]
		if content == "" {
			continue
		}
		old := len(content) - len(strings.TrimLeft(content, " "))
		start := p.LineStart(line)
		p.Replace(start, start+old, spaces(old+delta))
	}
}

func (p *structurePlan) commentStyle() {
	for line := 1; line <= len(p.doc.Lines); line++ {
		t := p.comments[line]
		if t == nil || !p.allowed("comments", line) {
			continue
		}
		start := p.Offset(t)
		content := p.doc.Lines[line-1]
		column := start - p.LineStart(line)
		if column >= len(content) || content[column] != '#' {
			continue
		}
		if p.Comments.RequireSpaceAfterHash && column+1 < len(content) && !strings.ContainsRune(" #", rune(content[column+1])) {
			p.warn("comments", t, true, "expected a space after #")
			p.Replace(start+1, start+1, " ")
		}
		prefix := content[:column]
		trimmed := strings.TrimRight(prefix, " \t")
		if trimmed != "" && (column-len(trimmed) < p.Comments.MinSpacesFromContent) {
			p.warn("comments", t, true, "insufficient space before inline comment")
			p.Replace(p.LineStart(line)+len(trimmed), start, spaces(p.Comments.MinSpacesFromContent))
		}
	}
}

func (p *structurePlan) documentStart() {
	id := "document-start"
	var header *token.Token
	for _, d := range p.doc.AST.Docs {
		if d.Start != nil {
			header = d.Start
			break
		}
		if d.Body != nil && d.Body.Type() == ast.DirectiveType {
			return
		}
		if d.Body != nil && d.Body.Type() != ast.CommentType {
			break
		}
	}
	if (header != nil) == p.DocumentStart.Present || p.Text == "" {
		return
	}
	if header == nil {
		if p.allowed(id, 1) {
			t := &token.Token{Position: &token.Position{Line: 1, Column: 1}}
			p.warn(id, t, true, "missing document start marker")
			p.Replace(0, 0, "---\n")
		}
		return
	}
	line := header.Position.Line
	if !p.allowed(id, line) || p.doc.IgnoreMap.ControlLine(line) {
		return
	}
	// Removing the first marker must not collapse an empty first document.
	// The verifier independently checks document boundaries.
	for _, d := range p.doc.AST.Docs {
		if d.Start == header && (d.Body == nil || d.Body.Type() == ast.CommentType) {
			p.warn(id, header, false, "cannot remove marker from empty document")
			return
		}
	}
	start := p.Offset(header)
	end := p.CoverEnd(start, p.LineStart(line+1))
	rendered := p.Render(start, end)
	body := strings.TrimLeft(strings.TrimPrefix(rendered, "---"), " \t")
	body = strings.TrimPrefix(body, "\n")
	p.warn(id, header, true, "unexpected document start marker")
	p.Replace(start, end, body)
}

func (p *structurePlan) scalar(n ast.Node, where placement) {
	t := n.GetToken()
	line := t.Position.Line
	start, end := p.Offset(t), p.TokenEnd(t)
	if !where.key && p.allowed("truthy", line) {
		if want := formatter.Truthy(t); want != "" && want != t.Value && end > start && !strings.Contains(p.Text[start:end], "\n") {
			p.warn("truthy", t, true, "use "+want)
			p.Replace(start, end, want)
			return
		}
	}
	if _, ok := n.(*ast.NullNode); ok && p.allowed("null-style", line) {
		want := p.NullStyle.Style
		if want == "tilde" {
			want = "~"
		}
		if want == "empty" {
			if where.flow || where.tagged || !where.mapping || t.Type == token.ImplicitNullType {
				return
			}
			want = ""
			start = p.TokenEnd(where.parent)
			gap := p.Text[start:p.Offset(t)]
			if strings.TrimSpace(gap) != "" || strings.Count(gap, "\n") > 1 ||
				!p.allowed("null-style", where.parent.Position.Line) ||
				where.parent.Position.Line != line && p.doc.IgnoreMap.ControlLine(line) {
				return
			}
			if c := p.comments[line]; c != nil && p.allowed("comments", line) {
				// Include the old gap and its pending comment-spacing edit.
				comment := p.Offset(c)
				want = spaces(max(comment-end, p.Comments.MinSpacesFromContent))
				end = comment
			}
		} else if t.Type == token.ImplicitNullType {
			if where.tagged || where.parent == nil || where.flow && !where.mapping {
				return
			}
			start = p.Offset(where.parent)
			end = start + 1
			want = p.Text[start:end] + " " + want
		} else if p.Text[start:end] == want {
			return
		}
		p.warn("null-style", t, true, "use configured null style")
		p.Replace(start, end, want)
		return
	}
	if p.allowed("quotes", line) && (t.Type == token.SingleQuoteType || t.Type == token.DoubleQuoteType) &&
		end > start && !p.doc.QuotedStart(line) {
		want := p.quote(t.Value, p.Text[start:end], where.flow)
		if p.Text[start:end] != want {
			p.warn("quotes", t, true, "use configured quote style")
			p.Replace(start, end, want)
		}
	}
}

func (p *structurePlan) quote(value, original string, flow bool) string {
	control := strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' })
	if p.Quotes.Unnecessary == "remove" && !control && plainSafe(value, flow) {
		return value
	}
	if p.Quotes.Prefer == "single" && !control {
		if strings.HasPrefix(original, "'") {
			return original
		}
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	if strings.HasPrefix(original, `"`) {
		return original
	}
	return doubleQuote(value)
}

// Some valid decimal spellings are classified as strings by the lexer.
var yamlDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func plainSafe(s string, flow bool) bool {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", rune(s[0])) ||
		strings.Contains(s, ": ") || strings.Contains(s, ":\t") || strings.HasSuffix(s, ":") ||
		strings.Contains(s, " #") || strings.Contains(s, "\t#") || (flow && strings.ContainsAny(s, ",[]{}")) || s == "..." {
		return false
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", ".inf", "+.inf", "-.inf", ".nan":
		return false
	}
	if yamlDecimal.MatchString(s) {
		return false
	}
	ts := lexer.Tokenize(s)
	return len(ts) == 1 && ts[0].Type == token.StringType && ts[0].Value == s
}

func doubleQuote(s string) string {
	const hex = "0123456789abcdef"
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\n':
			out.WriteString("\\n")
		case '\r':
			out.WriteString("\\r")
		case '\t':
			out.WriteString("\\t")
		default:
			if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
				out.WriteString("\\u")
				for shift := 12; shift >= 0; shift -= 4 {
					out.WriteByte(hex[(r>>shift)&15])
				}
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}

func (p *structurePlan) flat(n ast.Node, isKey bool) (string, bool) {
	switch n := n.(type) {
	case *ast.SequenceNode:
		var parts []string
		for _, child := range n.Values {
			s, ok := p.flat(child, false)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case *ast.MappingNode:
		var parts []string
		entries := n.Values
		if sorted := p.sortedMaps[n]; sorted != nil {
			entries = sorted
		}
		for _, entry := range entries {
			k, ok := p.flat(entry.Key, true)
			v, ok2 := p.flat(entry.Value, false)
			if !ok || !ok2 {
				return "", false
			}
			parts = append(parts, k+": "+v)
		}
		return "{" + strings.Join(parts, ", ") + "}", true
	case *ast.LiteralNode, *ast.TagNode, *ast.AnchorNode, *ast.AliasNode, *ast.MappingKeyNode:
		return "", false
	}
	t := n.GetToken()
	if _, ok := n.(*ast.NullNode); ok {
		if p.allowed("null-style", t.Position.Line) {
			switch p.NullStyle.Style {
			case "null":
				return "null", true
			case "tilde":
				return "~", true
			}
		}
		if t.Type == token.ImplicitNullType {
			return "null", true
		}
		return t.Value, true
	}
	start, end := p.Offset(t), p.TokenEnd(t)
	if end <= start {
		return "", false
	}
	s := p.Text[start:end]
	if strings.Contains(s, "\n") {
		return "", false
	}
	if !isKey {
		if want := formatter.Truthy(t); want != "" && p.allowed("truthy", t.Position.Line) {
			return want, true
		}
	}
	if str, ok := n.(*ast.StringNode); ok {
		quoted := t.Type == token.SingleQuoteType || t.Type == token.DoubleQuoteType
		if quoted && p.allowed("quotes", t.Position.Line) {
			return p.quote(str.Value, s, true), true
		}
		if !quoted && strings.ContainsAny(s, ",[]{}") {
			if p.Quotes.Prefer == "single" {
				return "'" + strings.ReplaceAll(str.Value, "'", "''") + "'", true
			}
			return doubleQuote(str.Value), true
		}
	}
	return s, true
}

func (p *structurePlan) sequence(n *ast.SequenceNode, where placement, indent int) {
	id := "sequence-style"
	line := n.Start.Position.Line
	if !p.allowed(id, line) {
		return
	}
	if len(n.Values) == 0 {
		if p.mode != "flow" && !p.SequenceStyle.AllowEmptyFlow {
			p.warn(id, n.Start, false, "empty sequence requires flow syntax")
		}
		return
	}
	start, end := p.Offset(n.Start), p.end(n)
	lastLine := line + strings.Count(strings.TrimSuffix(p.Text[start:end], "\n"), "\n")
	safe := true
	for i := line; i <= lastLine; i++ {
		if !p.allowed(id, i) || p.doc.IgnoreMap.ControlLine(i) || p.hasIgnoredMixedEOL(i) || p.comments[i] != nil &&
			(p.Offset(p.comments[i]) < end || !n.IsFlowStyle) || strings.TrimSpace(p.doc.Lines[i-1]) == "" {
			safe = false
		}
	}
	flow, flat := p.targetFlow(n, safe)

	if n.IsFlowStyle == flow {
		return
	}
	if where.key || where.tagged {
		safe = false
	}
	if where.flow && !flow {
		if where.mapping {
			p.warn(id, n.Start, false, "block sequence requires block mapping context")
		}
		return
	}
	var replacement string
	if safe && flow {
		if flat == "" {
			flat, safe = p.flat(n, false)
		}
		replacement = flat
		if where.parent != nil && where.parent.Position.Line < line {
			parentLine := where.parent.Position.Line
			gap := p.TokenEnd(where.parent)
			if gap <= start && strings.TrimSpace(p.Text[gap:start]) == "" &&
				strings.Count(p.Text[gap:start], "\n") == 1 && !p.doc.IgnoreMap.ControlLine(parentLine) && p.allowed(id, parentLine) {
				start = gap
				replacement = " " + flat
			}
		}
	}
	if safe && !flow {
		replacement, safe = p.blockItems(n, indent)
		prefix := spaces(indent)

		if where.parent != nil && where.parent.Position.Line == line && (where.mapping || where.parent.Type == token.DocumentHeaderType) {
			before := p.TokenEnd(where.parent)
			if strings.TrimSpace(p.Text[before:start]) != "" {
				safe = false
			}
			start = before
			replacement = "\n" + prefix + replacement
		}
	}
	p.warn(id, n.Start, safe, "use configured sequence style")
	if safe {
		p.Replace(start, end, replacement)
	}
}

func (p *structurePlan) targetFlow(n *ast.SequenceNode, safe bool) (bool, string) {
	if p.mode != "hybrid" {
		return p.mode == "flow", ""
	}
	if !safe || len(n.Values) > p.SequenceStyle.HybridMaxItems {
		return false, ""
	}
	for _, v := range n.Values {
		switch v.Type() {
		case ast.StringType, ast.IntegerType, ast.FloatType, ast.BoolType, ast.NullType, ast.InfinityType, ast.NanType:
		default:
			return false, ""
		}
	}
	flat, ok := p.flat(n, false)
	return ok && utf8.RuneCountInString(flat) <= p.SequenceStyle.HybridMaxLineLength, flat
}

func (p *structurePlan) blockItems(n *ast.SequenceNode, indent int) (string, bool) {
	var items []string
	for _, child := range n.Values {
		t := p.first(child)
		start, end := p.Offset(t), p.end(child)
		if end <= start {
			return "", false
		}
		s := p.Render(start, end)
		if seq, ok := child.(*ast.SequenceNode); ok && len(seq.Values) > 0 {
			flow, _ := p.targetFlow(seq, true)
			if !flow {
				part, ok := p.blockItems(seq, indent+2)
				if !ok {
					return "", false
				}
				items = append(items, "- "+part)
				continue
			}
		}
		if str, ok := child.(*ast.StringNode); ok && p.allowed("quotes", t.Position.Line) &&
			(t.Type == token.SingleQuoteType || t.Type == token.DoubleQuoteType) && !p.doc.QuotedStart(t.Position.Line) {
			s = p.quote(str.Value, p.Text[start:end], false)
		}
		if strings.Contains(s, "\n") {
			return "", false
		}
		items = append(items, "- "+s)
	}
	return strings.Join(items, "\n"+spaces(indent)), true
}

func (p *structurePlan) sortMapping(n *ast.MappingNode, depth int) {
	id := "sort-keys"
	if !p.enabled[id] || depth > 0 && !p.SortKeys.Recursive || len(n.Values) < 2 {
		return
	}
	line := p.first(n).Position.Line
	if !p.allowed(id, line) {
		return
	}
	type chunk struct {
		entry      *ast.MappingValueNode
		key        string
		start, end int
	}
	var entries []chunk
	safe := true
	compact := false
	for index, entry := range n.Values {
		t := entry.Key.GetToken()
		start, end := p.Offset(t), p.end(entry)
		key := t.Value
		if !n.IsFlowStyle {
			at := p.LineStart(t.Position.Line)
			if strings.TrimSpace(p.Text[at:start]) != "" {
				compact = compact || index == 0
				safe = safe && index == 0
			} else {
				start = at
				for l := t.Position.Line - 1; l >= 1 && p.comments[l] != nil; l-- {
					comment := p.comments[l]
					if comment.Position.Column != t.Position.Column ||
						!strings.HasPrefix(strings.TrimLeft(p.doc.Lines[l-1], " "), "#") ||
						index > 0 && p.LineStart(l) < entries[index-1].end {
						break
					}
					start = p.LineStart(l)
				}
			}
			if end == 0 || p.Text[end-1] != '\n' {
				if i := strings.IndexByte(p.Text[end:], '\n'); i >= 0 {
					end += i
				} else {
					end = len(p.Text)
				}
			} else {
				end--
			}
		}
		entries = append(entries, chunk{entry, key, start, end})
	}
	sorted := slices.Clone(entries)
	cmp := func(a, b chunk) int { return strings.Compare(a.key, b.key) }
	if slices.IsSortedFunc(sorted, cmp) {
		return
	}
	slices.SortStableFunc(sorted, cmp)
	last := entries[len(entries)-1].end
	firstLine := line - strings.Count(p.Text[entries[0].start:p.Offset(p.first(n))], "\n")
	for l := firstLine; l <= line+strings.Count(p.Text[p.Offset(p.first(n)):last], "\n"); l++ {
		if p.doc.IgnoreMap.Ignored(id, l) || p.doc.IgnoreMap.ControlLine(l) || p.hasIgnoredMixedEOL(l) || n.IsFlowStyle && p.comments[l] != nil {
			safe = false
		}
	}
	var boundOriginal, boundSorted []*ast.MappingValueNode
	bound := make(map[*ast.MappingValueNode]bool)
	for _, e := range entries {
		from := e.entry.Key.GetToken().Position.Line
		to := from + strings.Count(p.Text[p.Offset(e.entry.Key.GetToken()):e.end], "\n")
		for l := from; l <= to; l++ {
			if p.bindings[l] {
				bound[e.entry] = true
				boundOriginal = append(boundOriginal, e.entry)
				break
			}
		}
	}
	for _, e := range sorted {
		if bound[e.entry] {
			boundSorted = append(boundSorted, e.entry)
		}
	}
	safe = safe && slices.Equal(boundOriginal, boundSorted)
	p.warn(id, p.first(n), safe, "mapping keys are out of order")
	if !safe {
		return
	}
	var out strings.Builder
	for i, e := range sorted {
		p.sortedMaps[n] = append(p.sortedMaps[n], e.entry)
		if i > 0 {
			out.WriteString(p.Render(entries[i-1].end, entries[i].start))
		}
		part := p.Render(e.start, e.end)
		if compact {
			first := entries[0]
			t := p.first(n)
			delta := p.indent[line] - (len(p.doc.Lines[line-1]) - len(strings.TrimLeft(p.doc.Lines[line-1], " ")))
			padding := spaces(t.Position.Column - 1 + delta)
			switch {
			case i == 0:
				part = strings.TrimLeft(part, " ")
				if strings.HasPrefix(part, "#") {
					out.WriteString("\n" + padding)
				}
			case e.entry == first.entry:
				out.WriteString(padding)
			}
		}
		out.WriteString(part)
	}
	p.Replace(entries[0].start, last, out.String())
}
