package config

type RuleOptions struct {
	Enabled bool     `yaml:"enabled"`
	Ignore  []string `yaml:"ignore"`
}

type EncodingOptions struct {
	RuleOptions `yaml:",inline"`
	ForbidBOM   bool `yaml:"forbid_bom"`
}

type LineEndingsOptions struct {
	RuleOptions `yaml:",inline"`
	Type        string `yaml:"type"`
}

type LineLengthOptions struct {
	RuleOptions    `yaml:",inline"`
	Max            int  `yaml:"max"`
	IgnoreComments bool `yaml:"ignore_comments"`
	IgnoreURLs     bool `yaml:"ignore_urls"`
}

type BlankLinesOptions struct {
	RuleOptions    `yaml:",inline"`
	MaxConsecutive int `yaml:"max_consecutive"`
}

type IndentationOptions struct {
	RuleOptions `yaml:",inline"`
	Spaces      int `yaml:"spaces"`
}

type SequenceStyleOptions struct {
	RuleOptions         `yaml:",inline"`
	Mode                string `yaml:"mode"`
	AllowEmptyFlow      bool   `yaml:"allow_empty_flow"`
	HybridMaxItems      int    `yaml:"hybrid_max_items"`
	HybridMaxLineLength int    `yaml:"hybrid_max_line_length"`
}

type QuotesOptions struct {
	RuleOptions `yaml:",inline"`
	Prefer      string `yaml:"prefer"`
	Unnecessary string `yaml:"unnecessary"`
}

type TruthyOptions struct {
	RuleOptions `yaml:",inline"`
	Style       string `yaml:"style"`
}

type NullStyleOptions struct {
	RuleOptions `yaml:",inline"`
	Style       string `yaml:"style"`
}

type CommentsOptions struct {
	RuleOptions           `yaml:",inline"`
	RequireSpaceAfterHash bool `yaml:"require_space_after_hash"`
	MinSpacesFromContent  int  `yaml:"min_spaces_from_content"`
}

type DocumentStartOptions struct {
	RuleOptions `yaml:",inline"`
	Present     bool `yaml:"present"`
}

type SortKeysOptions struct {
	RuleOptions `yaml:",inline"`
	Recursive   bool `yaml:"recursive"`
}

type Rules struct {
	Encoding           EncodingOptions      `yaml:"encoding"`
	LineEndings        LineEndingsOptions   `yaml:"line_endings"`
	LineLength         LineLengthOptions    `yaml:"line_length"`
	TrailingWhitespace RuleOptions          `yaml:"trailing_whitespace"`
	FinalNewline       RuleOptions          `yaml:"final_newline"`
	BlankLines         BlankLinesOptions    `yaml:"blank_lines"`
	Indentation        IndentationOptions   `yaml:"indentation"`
	SequenceStyle      SequenceStyleOptions `yaml:"sequence_style"`
	Quotes             QuotesOptions        `yaml:"quotes"`
	Truthy             TruthyOptions        `yaml:"truthy"`
	NullStyle          NullStyleOptions     `yaml:"null_style"`
	Comments           CommentsOptions      `yaml:"comments"`
	DocumentStart      DocumentStartOptions `yaml:"document_start"`
	SortKeys           SortKeysOptions      `yaml:"sort_keys"`
}

func (r Rules) options() []struct {
	id      string
	options RuleOptions
} {
	return []struct {
		id      string
		options RuleOptions
	}{
		{"encoding", r.Encoding.RuleOptions},
		{"line-endings", r.LineEndings.RuleOptions},
		{"line-length", r.LineLength.RuleOptions},
		{"trailing-whitespace", r.TrailingWhitespace},
		{"final-newline", r.FinalNewline},
		{"blank-lines", r.BlankLines.RuleOptions},
		{"indentation", r.Indentation.RuleOptions},
		{"sequence-style", r.SequenceStyle.RuleOptions},
		{"quotes", r.Quotes.RuleOptions},
		{"truthy", r.Truthy.RuleOptions},
		{"null-style", r.NullStyle.RuleOptions},
		{"comments", r.Comments.RuleOptions},
		{"document-start", r.DocumentStart.RuleOptions},
		{"sort-keys", r.SortKeys.RuleOptions},
	}
}

func Defaults() Config {
	enabled := RuleOptions{Enabled: true}
	return Config{
		Version: 1,
		Include: []string{"."},
		Ignore:  []string{".git/**", "node_modules/**", "vendor/**"},
		Rules: Rules{
			Encoding:           EncodingOptions{RuleOptions: enabled, ForbidBOM: true},
			LineEndings:        LineEndingsOptions{RuleOptions: enabled, Type: "lf"},
			LineLength:         LineLengthOptions{RuleOptions: enabled, Max: 120, IgnoreComments: true, IgnoreURLs: true},
			TrailingWhitespace: enabled,
			FinalNewline:       enabled,
			BlankLines:         BlankLinesOptions{RuleOptions: enabled, MaxConsecutive: 1},
			Indentation:        IndentationOptions{RuleOptions: enabled, Spaces: 2},
			SequenceStyle:      SequenceStyleOptions{RuleOptions: enabled, Mode: "block", AllowEmptyFlow: true, HybridMaxItems: 3, HybridMaxLineLength: 60},
			Quotes:             QuotesOptions{RuleOptions: enabled, Prefer: "double", Unnecessary: "remove"},
			Truthy:             TruthyOptions{RuleOptions: enabled, Style: "lower"},
			NullStyle:          NullStyleOptions{RuleOptions: enabled, Style: "empty"},
			Comments:           CommentsOptions{RuleOptions: enabled, RequireSpaceAfterHash: true, MinSpacesFromContent: 2},
			DocumentStart:      DocumentStartOptions{},
			SortKeys:           SortKeysOptions{Recursive: true},
		},
	}
}
