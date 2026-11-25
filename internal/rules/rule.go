package rules

// Rule identifies a configurable Layer 2 style rule.
type Rule interface {
	ID() string
}

type rule[P any] struct {
	id   string
	plan func(*P)
}

func (r rule[P]) ID() string { return r.id }
