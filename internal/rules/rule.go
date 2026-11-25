package rules

// Rule identifies a rule and plans its diagnostics and repairs.
type Rule interface {
	ID() string
	plan(*hygienePlan)
}

type hygieneRule struct {
	id       string
	evaluate func(*hygienePlan)
}

func (r hygieneRule) ID() string          { return r.id }
func (r hygieneRule) plan(p *hygienePlan) { r.evaluate(p) }
