package formatter

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
	"github.com/withnic/kiku/internal/parser"
)

type VerifyOptions struct {
	SortKeys, Truthy, DocumentStart bool
}

// Truthy returns the canonical spelling of supported plain boolean literals.
func Truthy(t *token.Token) string {
	if t.Type != token.StringType && t.Type != token.BoolType {
		return ""
	}
	switch t.Value {
	case "true", "True", "TRUE", "yes", "Yes", "YES", "on", "On", "ON":
		return "true"
	case "false", "False", "FALSE", "no", "No", "NO", "off", "Off", "OFF":
		return "false"
	}
	return ""
}

func VerifyRoundTrip(before *parser.Document, output []byte, options VerifyOptions) error {
	after, diagnostics := parser.Parse(before.Path, output, false, 2)
	if after.AST == nil {
		return fmt.Errorf("formatted YAML is invalid: %v", diagnostics)
	}
	left, right := documentBodies(before.AST, options.DocumentStart), documentBodies(after.AST, options.DocumentStart)
	if len(left) != len(right) {
		return fmt.Errorf("formatting changed the document count")
	}
	for i, node := range left {
		if !equalNode(node, right[i], options) {
			return fmt.Errorf("formatting changed YAML data in document %d", i+1)
		}
	}
	a, b := tokenMetadata(before.NormalizedText, options.DocumentStart), tokenMetadata(after.NormalizedText, options.DocumentStart)
	if !slices.Equal(a.boundaries, b.boundaries) {
		return fmt.Errorf("formatting changed YAML document markers")
	}
	if options.SortKeys {
		slices.Sort(a.comments)
		slices.Sort(b.comments)
	}
	if !slices.Equal(a.comments, b.comments) {
		return fmt.Errorf("formatting changed YAML comments")
	}
	if !slices.Equal(a.bindings, b.bindings) {
		return fmt.Errorf("formatting changed anchor or alias order")
	}
	oldAnchors, newAnchors := anchors(left), anchors(right)
	if len(oldAnchors) != len(newAnchors) {
		return fmt.Errorf("formatting changed YAML anchors")
	}
	for i, old := range oldAnchors {
		if !equalNode(old, newAnchors[i], options) {
			return fmt.Errorf("formatting changed YAML anchor bindings")
		}
	}
	return nil
}

type anchorVisitor struct{ nodes []*ast.AnchorNode }

func (v *anchorVisitor) Visit(n ast.Node) ast.Visitor {
	if anchor, ok := n.(*ast.AnchorNode); ok {
		v.nodes = append(v.nodes, anchor)
	}
	return v
}

func anchors(nodes []ast.Node) []*ast.AnchorNode {
	var v anchorVisitor
	for _, n := range nodes {
		if n != nil {
			ast.Walk(&v, n)
		}
	}
	slices.SortFunc(v.nodes, func(a, b *ast.AnchorNode) int {
		return cmp.Compare(a.Start.Position.Offset, b.Start.Position.Offset)
	})
	return v.nodes
}

func documentBodies(file *ast.File, optionalStart bool) []ast.Node {
	var bodies []ast.Node
	for _, d := range file.Docs {
		if d.Body != nil && d.Body.Type() != ast.CommentType || d.Start != nil || d.End != nil {
			bodies = append(bodies, d.Body)
		}
	}
	if optionalStart && len(bodies) == 1 && (bodies[0] == nil || bodies[0].Type() == ast.CommentType) {
		return nil
	}
	return bodies
}

type metadata struct{ comments, bindings, boundaries []string }

func tokenMetadata(text string, optionalStart bool) metadata {
	var m metadata
	first := true
	for _, t := range lexer.Tokenize(text) {
		switch t.Type {
		case token.CommentType:
			m.comments = append(m.comments, strings.TrimSpace(t.Value))
		case token.DocumentHeaderType:
			if !first || !optionalStart {
				m.boundaries = append(m.boundaries, t.Value)
			}
			first = false
		case token.DocumentEndType:
			m.boundaries = append(m.boundaries, t.Value)
			first = false
		case token.AnchorType, token.AliasType:
			first = false
			if t.Next != nil {
				m.bindings = append(m.bindings, t.Value+t.Next.Value)
			}
		default:
			first = false
		}
	}
	return m
}

func equalNode(a, b ast.Node, opts VerifyOptions) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Type() == ast.CommentType || b.Type() == ast.CommentType {
		return a.Type() == b.Type()
	}
	if opts.Truthy {
		if want := Truthy(a.GetToken()); want != "" && b.GetToken().Type == token.BoolType && b.GetToken().Value == want {
			return true
		}
	}
	switch a := a.(type) {
	case *ast.MappingNode:
		b, ok := b.(*ast.MappingNode)
		if !ok || len(a.Values) != len(b.Values) {
			return false
		}
		if !opts.SortKeys {
			for i, x := range a.Values {
				if !equalNode(x, b.Values[i], opts) {
					return false
				}
			}
			return true
		}
		index := make(map[string][]*ast.MappingValueNode, len(b.Values))
		for _, entry := range b.Values {
			k := entry.Key.GetToken().Value
			index[k] = append(index[k], entry)
		}
		for _, x := range a.Values {
			key := x.Key.GetToken().Value
			matched := false
			for i, y := range index[key] {
				if equalNode(x, y, opts) {
					index[key] = slices.Delete(index[key], i, i+1)
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	case *ast.MappingValueNode:
		b, ok := b.(*ast.MappingValueNode)
		keyOpts := opts
		keyOpts.Truthy = false
		return ok && equalNode(a.Key, b.Key, keyOpts) && equalNode(a.Value, b.Value, opts)
	case *ast.MappingKeyNode:
		b, ok := b.(*ast.MappingKeyNode)
		return ok && equalNode(a.Value, b.Value, opts)
	case *ast.SequenceNode:
		b, ok := b.(*ast.SequenceNode)
		if !ok || len(a.Values) != len(b.Values) {
			return false
		}
		for i, x := range a.Values {
			if !equalNode(x, b.Values[i], opts) {
				return false
			}
		}
		return true
	case *ast.AnchorNode:
		b, ok := b.(*ast.AnchorNode)
		return ok && equalNode(a.Name, b.Name, VerifyOptions{}) && equalNode(a.Value, b.Value, opts)
	case *ast.AliasNode:
		b, ok := b.(*ast.AliasNode)
		return ok && equalNode(a.Value, b.Value, VerifyOptions{})
	case *ast.TagNode:
		b, ok := b.(*ast.TagNode)
		return ok && a.Start.Value == b.Start.Value && equalNode(a.Value, b.Value, VerifyOptions{SortKeys: opts.SortKeys})
	case *ast.LiteralNode:
		b, ok := b.(*ast.LiteralNode)
		return ok && a.Start.Value == b.Start.Value && a.Value.Value == b.Value.Value
	case ast.ScalarNode:
		b, ok := b.(ast.ScalarNode)
		return ok && a.Type() == b.Type() && fmt.Sprint(a.GetValue()) == fmt.Sprint(b.GetValue())
	default:
		return a.Type() == b.Type() && a.String() == b.String()
	}
}
