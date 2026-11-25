package rules

import (
	"strings"
	"unicode/utf8"

	"github.com/withnic/kiku/internal/config"
	"github.com/withnic/kiku/internal/fs"
	"github.com/withnic/kiku/internal/parser"
)

// HygieneResult contains the proposed output in both normalized and rendered
// form. A caller must verify the proposal before writing it to disk.
type HygieneResult struct {
	Diagnostics    []parser.Diagnostic
	NormalizedText string
	Output         []byte
}

var hygieneRules = []Rule{
	hygieneRule{"encoding", (*hygienePlan).checkEncoding},
	hygieneRule{"line-endings", (*hygienePlan).checkLineEndings},
	hygieneRule{"line-length", (*hygienePlan).checkLineLength},
	hygieneRule{"trailing-whitespace", (*hygienePlan).checkTrailingWhitespace},
	hygieneRule{"blank-lines", (*hygienePlan).checkBlankLines},
	hygieneRule{"final-newline", (*hygienePlan).checkFinalNewline},
}

type hygienePlan struct {
	doc                   *parser.Document
	settings              config.Rules
	ruleID                string
	diagnostics           []parser.Diagnostic
	lines                 []string
	removed               []bool
	endings               []string
	targetEOL             string
	renderOriginalEndings bool
	finalNewlineEnabled   bool
	bom                   bool
	addFinal              bool
}

// Hygiene evaluates original line numbers and applies eligible repairs together,
// so deletions cannot shift a later rule's ignore scope.
func Hygiene(doc *parser.Document, cfg *config.Resolved, relativePath string, fix bool) HygieneResult {
	result := HygieneResult{NormalizedText: doc.NormalizedText, Output: doc.RawBytes}
	if doc.AST == nil {
		return result
	}

	lineEndings := cfg.RuleEnabled("line-endings", relativePath)
	plan := hygienePlan{
		doc: doc, settings: cfg.Rules, bom: doc.HadBOM,
		lines:                 append([]string(nil), doc.Lines...),
		removed:               make([]bool, len(doc.Lines)),
		endings:               fs.LineEndings(string(doc.RawBytes)),
		targetEOL:             fs.OutputEOL(doc.OriginalEOL, cfg.Rules.LineEndings.Type, lineEndings),
		renderOriginalEndings: lineEndings,
		finalNewlineEnabled:   cfg.RuleEnabled("final-newline", relativePath),
	}
	for _, rule := range hygieneRules {
		if cfg.RuleEnabled(rule.ID(), relativePath) {
			plan.ruleID = rule.ID()
			rule.plan(&plan)
		}
	}

	result.Diagnostics = plan.diagnostics
	if fix {
		result.NormalizedText, result.Output = plan.render()
	}
	return result
}

func (p *hygienePlan) ignored(line int) bool {
	return p.doc.IgnoreMap.Ignored(p.ruleID, line)
}

func (p *hygienePlan) warn(line, column int, fixable bool, message string) {
	p.diagnostics = append(p.diagnostics, parser.Diagnostic{
		FilePath: p.doc.Path, Line: line, Column: column, RuleID: p.ruleID,
		Severity: parser.SeverityWarning, Fixable: fixable, Message: message,
	})
}

func (p *hygienePlan) checkEncoding() {
	if p.bom && p.settings.Encoding.ForbidBOM && !p.ignored(1) {
		p.warn(1, 1, true, "unexpected UTF-8 BOM")
		p.bom = false
	}
}

func (p *hygienePlan) checkLineEndings() {
	for i, ending := range p.endings {
		if ending != p.targetEOL && !p.ignored(i+1) {
			p.warn(i+1, utf8.RuneCountInString(p.doc.Lines[i])+1, true, "unexpected line ending")
			p.endings[i] = p.targetEOL
		}
	}
}

func (p *hygienePlan) checkLineLength() {
	opts := p.settings.LineLength
	for i, line := range p.doc.Lines {
		number := i + 1
		if p.doc.Protected(number) || p.doc.IgnoreMap.ControlLine(number) || p.ignored(number) {
			continue
		}
		if (opts.IgnoreComments && strings.HasPrefix(strings.TrimLeft(line, " \t"), "#")) ||
			(opts.IgnoreURLs && (strings.Contains(line, "http://") || strings.Contains(line, "https://"))) {
			continue
		}
		if utf8.RuneCountInString(line) > opts.Max {
			p.warn(number, opts.Max+1, false, "line exceeds maximum length")
		}
	}
}

func (p *hygienePlan) checkTrailingWhitespace() {
	for i, line := range p.doc.Lines {
		number := i + 1
		if p.doc.Protected(number) || p.doc.QuotedStart(number) || p.ignored(number) {
			continue
		}
		if trimmed := strings.TrimRight(line, " \t"); trimmed != line {
			p.warn(number, utf8.RuneCountInString(trimmed)+1, true, "trailing whitespace")
			p.lines[i] = trimmed
		}
	}
}

func (p *hygienePlan) trailingBlankStart() int {
	start := len(p.doc.Lines)
	if !p.finalNewlineEnabled {
		return start
	}
	for i := len(p.doc.Lines) - 1; i > 0; i-- {
		if strings.Trim(p.doc.Lines[i], " \t") != "" || p.doc.Protected(i+1) || p.doc.IgnoreMap.Ignored("final-newline", i+1) {
			break
		}
		start = i
	}
	return start
}

func (p *hygienePlan) checkBlankLines() {
	limit := p.trailingBlankStart()
	blankRun := 0
	for i := 0; i < limit; i++ {
		line := p.doc.Lines[i]
		number := i + 1
		if p.doc.Protected(number) || strings.Trim(line, " \t") != "" {
			blankRun = 0
			continue
		}
		blankRun++
		if blankRun > p.settings.BlankLines.MaxConsecutive && !p.ignored(number) {
			p.warn(number, 1, true, "too many consecutive blank lines")
			p.removed[i] = true
		}
	}
}

func (p *hygienePlan) checkFinalNewline() {
	last := len(p.lines) - 1
	if last < 0 {
		return
	}
	for i := p.trailingBlankStart(); i <= last; i++ {
		p.warn(i+1, 1, true, "extra blank line at end of file")
		p.removed[i] = true
	}
	if len(p.endings) <= last && !p.removed[last] && !p.doc.Protected(last+1) && !p.ignored(last+1) {
		p.warn(last+1, utf8.RuneCountInString(p.doc.Lines[last])+1, true, "missing final newline")
		p.addFinal = true
	}
}

func (p *hygienePlan) render() (string, []byte) {
	var normalized, output strings.Builder
	if p.bom {
		output.WriteString("\ufeff")
	}
	for i, line := range p.lines {
		if p.removed[i] {
			continue
		}
		normalized.WriteString(line)
		output.WriteString(line)
		if i < len(p.endings) || (i == len(p.lines)-1 && p.addFinal) {
			eol := p.targetEOL
			if p.renderOriginalEndings {
				if i < len(p.endings) {
					eol = p.endings[i]
				} else if p.doc.IgnoreMap.Ignored("line-endings", i+1) {
					eol = fs.OutputEOL(p.doc.OriginalEOL, "", false)
				}
			}
			normalized.WriteByte('\n')
			output.WriteString(fs.ApplyEOL("\n", eol))
		}
	}
	return normalized.String(), []byte(output.String())
}
