package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/withnic/kiku/internal/parser"
)

func report(out io.Writer, format string, diagnostics []parser.Diagnostic) error {
	if format == "json" {
		if diagnostics == nil {
			diagnostics = []parser.Diagnostic{}
		}
		return json.NewEncoder(out).Encode(diagnostics)
	}
	for _, d := range diagnostics {
		var err error
		if format == "github" {
			_, err = fmt.Fprintf(out, "::%s file=%s,line=%d,col=%d,title=%s::%s\n",
				d.Severity, githubProperty.Replace(d.FilePath), d.Line, d.Column,
				githubProperty.Replace(d.RuleID), githubMessage.Replace(d.Message))
		} else {
			_, err = fmt.Fprintf(out, "%s:%d:%d: %s: %s (%s)\n",
				d.FilePath, d.Line, d.Column, d.Severity, d.Message, d.RuleID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

var (
	githubMessage  = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	githubProperty = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)
