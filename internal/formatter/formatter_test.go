package formatter

import (
	"testing"

	"github.com/withnic/kiku/internal/parser"
)

func TestVerifyRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		options             VerifyOptions
		ok                  bool
	}{
		{"nested data", "x: [{a: 1}]\n", "x: [{a: 2}]\n", VerifyOptions{}, false},
		{"changed null type", "x: []\n", "x:\n", VerifyOptions{}, false},
		{"sequence order", "x: [a,b]\n", "x: [b,a]\n", VerifyOptions{SortKeys: true}, false},
		{"key order", "a: 1\nb: 2\n", "b: 2\na: 1\n", VerifyOptions{}, false},
		{"comment order", "a: 1 # a\nb: 2 # b\n", "# b\n# a\na: 1\nb: 2\n", VerifyOptions{}, false},
		{"duplicate comment", "a: 1 # a\n# a\n", "a: 1 # a\n", VerifyOptions{SortKeys: true}, false},
		{"comments follow keys", "a: 1 # a\nb: 2 # b\n", "b: 2  #b\na: 1  # a\n", VerifyOptions{SortKeys: true}, true},
		{"plain truthy", "a: YES\n", "a: true\n", VerifyOptions{Truthy: true}, true},
		{"truthy key rejected", "on: push\n", "true: push\n", VerifyOptions{Truthy: true}, false},
		{"different truthy spelling", "a: YES\n", "a: On\n", VerifyOptions{Truthy: true}, false},
		{"quoted truthy", "a: 'yes'\n", "a: true\n", VerifyOptions{Truthy: true}, false},
		{"invalid output", "a: 1\n", "a: [\n", VerifyOptions{}, false},
		{"anchor semantics", "a: &x 1\nb: &x 2\nc: *x\n", "b: &x 2\na: &x 1\nc: *x\n", VerifyOptions{SortKeys: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, diags := parser.Parse("example.yaml", []byte(tc.before), false, 2)
			if doc.AST == nil {
				t.Fatalf("invalid fixture: %v", diags)
			}
			err := VerifyRoundTrip(doc, []byte(tc.after), tc.options)
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected verification result: %v", err)
			}
		})
	}
}
