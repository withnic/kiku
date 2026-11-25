package parser

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
	"github.com/withnic/kiku/internal/fs"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Diagnostic struct {
	FilePath string   `json:"file_path"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	RuleID   string   `json:"rule_id"`
	Severity Severity `json:"severity"`
	Fixable  bool     `json:"fixable"`
	Message  string   `json:"message"`
}

type Document struct {
	Path           string
	RawBytes       []byte
	HadBOM         bool
	OriginalEOL    string
	NormalizedText string
	Lines          []string
	IgnoreMap      *IgnoreMap
	AST            *ast.File

	protected   []bool
	quoteStarts []bool
}

// Protected reports whether a one-based line intersects the contents of a multiline scalar.
func (d *Document) Protected(line int) bool {
	return line >= 1 && line <= len(d.protected) && d.protected[line-1]
}

// QuotedStart reports whether the line opens a quote that continues on the next line.
func (d *Document) QuotedStart(line int) bool {
	return line >= 1 && line <= len(d.quoteStarts) && d.quoteStarts[line-1]
}

func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// Parse runs Layer 1. In fix mode only, no-tabs diagnostics describe repairs
// already applied to NormalizedText. AST is nil when processing must stop.
func Parse(path string, raw []byte, fix bool, indentSpaces int) (*Document, []Diagnostic) {
	text, ending := fs.NormalizeEOL(string(raw))
	text, bom := strings.CutPrefix(text, "\ufeff")
	doc := &Document{
		Path: fs.NormalizePath(path), RawBytes: raw, HadBOM: bom,
		OriginalEOL: ending, NormalizedText: text, Lines: SplitLines(text),
	}
	if !utf8.ValidString(text) {
		line, column := 1, 1
		for _, content := range doc.Lines {
			column = 1
			for len(content) > 0 {
				r, size := utf8.DecodeRuneInString(content)
				if r == utf8.RuneError && size == 1 {
					return doc, []Diagnostic{doc.layer1("invalid-utf8", line, column, false, "invalid UTF-8 byte sequence")}
				}
				content = content[size:]
				column++
			}
			line++
		}
	}

	tokens := lexer.Tokenize(text)
	doc.protected = protectedLines(tokens, len(doc.Lines))
	if indentSpaces < 1 || indentSpaces > 8 {
		indentSpaces = 2
	}
	diagnostics := doc.indentationTabs(fix, indentSpaces)
	if len(diagnostics) != 0 {
		if !fix {
			return doc, diagnostics
		}
		text = strings.Join(doc.Lines, "\n")
		if strings.HasSuffix(doc.NormalizedText, "\n") {
			text += "\n"
		}
		doc.NormalizedText = text
		tokens = lexer.Tokenize(text)
		doc.protected = protectedLines(tokens, len(doc.Lines))
	}

	// Check uniqueness within each AST mapping, including different spellings
	// of the same scalar key and source positions in separate documents.
	tree, err := yamlparser.Parse(tokens, yamlparser.ParseComments, yamlparser.AllowDuplicateMapKey())
	if err != nil {
		line, column := 1, 1
		var located interface {
			GetToken() *token.Token
			GetMessage() string
		}
		message := err.Error()
		if errors.As(err, &located) {
			if tk := located.GetToken(); tk != nil && tk.Position != nil {
				line, column = tk.Position.Line, tk.Position.Column
			}
			message = located.GetMessage()
		}
		return doc, append(diagnostics, doc.layer1("syntax-error", line, column, false, message))
	}
	metadata := sourceMetadata{doc: doc, ends: make(map[int]int), scalarEnds: scalarEnds(tokens)}
	doc.quoteStarts = make([]bool, len(doc.Lines))
	for tk, end := range metadata.scalarEnds {
		line := tokenLine(tk)
		if (tk.Type == token.SingleQuoteType || tk.Type == token.DoubleQuoteType) &&
			line >= 1 && line <= len(doc.Lines) && end > line {
			doc.quoteStarts[line-1] = true
		}
	}
	for _, part := range tree.Docs {
		metadata.anchors = make(map[string]bool)
		metadata.spanEnd(part.Body)
	}
	if len(metadata.errors) != 0 {
		return doc, append(diagnostics, metadata.errors...)
	}
	doc.AST = tree
	doc.IgnoreMap = buildIgnoreMap(doc.Lines, tokens, metadata.ends, metadata.inlineEnds)
	return doc, diagnostics
}

func (d *Document) layer1(id string, line, column int, fixable bool, message string) Diagnostic {
	return Diagnostic{d.Path, line, column, id, SeverityError, fixable, message}
}

func (d *Document) indentationTabs(fix bool, spaces int) []Diagnostic {
	var diagnostics []Diagnostic
	for i, line := range d.Lines {
		if d.Protected(i + 1) {
			continue
		}
		size := len(line) - len(strings.TrimLeft(line, " \t"))
		prefix := line[:size]
		for column, c := range prefix {
			if c == '\t' {
				diagnostics = append(diagnostics, d.layer1("no-tabs", i+1, column+1, true, "tab in indentation"))
			}
		}
		if fix {
			d.Lines[i] = strings.ReplaceAll(prefix, "\t", strings.Repeat(" ", spaces)) + line[size:]
		}
	}
	return diagnostics
}

func scalarEnds(tokens token.Tokens) map[*token.Token]int {
	ends := make(map[*token.Token]int)
	for i, tk := range tokens {
		line := tk.Position.Line
		switch tk.Type {
		case token.SingleQuoteType, token.DoubleQuoteType:
			ends[tk] = line + strings.Count(strings.TrimLeft(tk.Origin, " \t\n"), "\n")
		case token.LiteralType, token.FoldedType:
			next := i + 1
			if next < len(tokens) && tokens[next].Type == token.CommentType && tokens[next].Position.Line == line {
				next++
			}
			if next >= len(tokens) {
				continue
			}
			body := tokens[next]
			switch {
			case body.Type == token.StringType && body.Origin != "":
				ends[tk] = line + 1 + strings.Count(strings.TrimSuffix(body.Origin, "\n"), "\n")
			case body.Type == token.InvalidType:
				// The scanner may have consumed valid content before it hit
				// an indentation error. Keep those earlier lines unchanged.
				ends[tk] = body.Position.Line - 1
			}
		}
	}
	return ends
}

func protectedLines(tokens token.Tokens, count int) []bool {
	protected := make([]bool, count)
	for tk, end := range scalarEnds(tokens) {
		for line := tk.Position.Line + 1; line <= end && line <= count; line++ {
			protected[line-1] = true
		}
	}
	return protected
}

type sourceMetadata struct {
	doc        *Document
	ends       map[int]int
	inlineEnds map[int]int
	scalarEnds map[*token.Token]int
	anchors    map[string]bool
	errors     []Diagnostic
}

func (m *sourceMetadata) remember(start *token.Token, end int) {
	if start != nil && start.Position != nil {
		line := start.Position.Line
		m.ends[line] = max(m.ends[line], end)
	}
}

func (m *sourceMetadata) rememberFlow(start *token.Token, end int) {
	if start != nil && start.Position != nil {
		line := start.Position.Line
		if m.inlineEnds == nil {
			m.inlineEnds = make(map[int]int)
		}
		m.inlineEnds[line] = max(m.inlineEnds[line], end)
	}
}

func tokenLine(tk *token.Token) int {
	if tk == nil || tk.Position == nil {
		return 0
	}
	return tk.Position.Line
}

func (m *sourceMetadata) spanEnd(node ast.Node) int {
	if node == nil {
		return 0
	}
	tk := node.GetToken()
	end := max(tokenLine(tk), m.scalarEnds[tk])
	switch n := node.(type) {
	case *ast.MappingNode:
		seen := make(map[string]bool)
		for _, entry := range n.Values {
			id := keyIdentity(entry.Key)
			if seen[id] {
				key := entry.Key.GetToken()
				m.errors = append(m.errors, m.doc.layer1("key-duplicates", key.Position.Line, key.Position.Column, false,
					fmt.Sprintf("duplicate mapping key %q", key.Value)))
			}
			seen[id] = true
			end = max(end, m.spanEnd(entry))
		}
		if n.IsFlowStyle {
			end = max(end, tokenLine(n.End))
			m.remember(n.Start, end)
			m.rememberFlow(n.Start, end)
		}
	case *ast.MappingValueNode:
		end = max(end, m.spanEnd(n.Key), m.spanEnd(n.Value))
		m.remember(n.Key.GetToken(), end)
	case *ast.MappingKeyNode:
		end = max(end, m.spanEnd(n.Value))
	case *ast.SequenceNode:
		for _, entry := range n.Entries {
			entryEnd := max(tokenLine(entry.Start), m.spanEnd(entry.Value))
			m.remember(entry.Start, entryEnd)
			end = max(end, entryEnd)
		}
		if n.IsFlowStyle {
			end = max(end, tokenLine(n.End))
			m.remember(n.Start, end)
			m.rememberFlow(n.Start, end)
		}
	case *ast.TagNode:
		end = max(end, m.spanEnd(n.Value))
	case *ast.AnchorNode:
		m.anchors[n.Name.GetToken().Value] = true
		end = max(end, m.spanEnd(n.Value))
	case *ast.AliasNode:
		name := n.Value.GetToken().Value
		if !m.anchors[name] {
			m.errors = append(m.errors, m.doc.layer1("syntax-error", tk.Position.Line, tk.Position.Column, false,
				fmt.Sprintf("alias %q has no preceding anchor in this document", name)))
		}
	case *ast.StringNode:
		m.remember(tk, end)
		m.rememberFlow(tk, end)
	case *ast.LiteralNode:
		m.remember(tk, end)
	}
	return end
}

func keyIdentity(key ast.MapKeyNode) string {
	if explicit, ok := key.(*ast.MappingKeyNode); ok {
		if inner, ok := explicit.Value.(ast.MapKeyNode); ok {
			return keyIdentity(inner)
		}
	}
	if scalar, ok := key.(ast.ScalarNode); ok {
		switch value := scalar.GetValue().(type) {
		case int64, uint64:
			return fmt.Sprintf("integer:%v", value)
		case float64:
			if value == 0 {
				return "float64:0" // Positive and negative zero identify the same key.
			}
			return fmt.Sprintf("%T:%v", value, value)
		default:
			return fmt.Sprintf("%T:%v", value, value)
		}
	}
	return fmt.Sprintf("%T:%s", key, key.GetToken().Value)
}
