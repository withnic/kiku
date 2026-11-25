package engine

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"

	"github.com/withnic/kiku/internal/config"
	"github.com/withnic/kiku/internal/formatter"
	"github.com/withnic/kiku/internal/parser"
	"github.com/withnic/kiku/internal/rules"
)

// Process handles one file. A nil output means callers must not write it.
// Layer 1 failures are reported as diagnostics, and verification failures
// are returned as errors.
func Process(path, relativePath string, raw []byte, cfg *config.Resolved, fix bool) ([]byte, []parser.Diagnostic, error) {
	doc, diagnostics := parser.Parse(path, raw, fix, cfg.Rules.Indentation.Spaces)
	if doc.AST == nil {
		return nil, diagnostics, nil
	}
	structure := rules.Structure(doc, cfg, relativePath, fix)
	hygiene := rules.Hygiene(doc, cfg, relativePath, fix && bytes.Equal(structure.Output, raw))
	diagnostics = append(diagnostics, structure.Diagnostics...)
	diagnostics = append(diagnostics, hygiene.Diagnostics...)
	slices.SortStableFunc(diagnostics, func(a, b parser.Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column), cmp.Compare(a.RuleID, b.RuleID))
	})
	if !fix {
		return nil, diagnostics, nil
	}
	if structure.Err != nil {
		return nil, diagnostics, structure.Err
	}
	if !bytes.Equal(structure.Output, raw) {
		// Refresh the AST protection and directive spans after lines have moved.
		formatted, problems := parser.Parse(path, structure.Output, false, cfg.Rules.Indentation.Spaces)
		if formatted.AST == nil {
			return nil, diagnostics, fmt.Errorf("structural rewrite produced invalid YAML: %v", problems)
		}
		hygiene = rules.Hygiene(formatted, cfg, relativePath, true)
	}
	if err := formatter.VerifyRoundTrip(doc, hygiene.Output, formatter.VerifyOptions{
		Truthy:        cfg.RuleEnabled("truthy", relativePath),
		SortKeys:      cfg.RuleEnabled("sort-keys", relativePath),
		DocumentStart: cfg.RuleEnabled("document-start", relativePath),
	}); err != nil {
		return nil, diagnostics, err
	}
	return hygiene.Output, diagnostics, nil
}
