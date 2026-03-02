package conformance

import "fmt"

type Usage struct {
	USD    float64
	Tokens int64
}

type BudgetGuard struct {
	MaxUSD    float64
	MaxTokens int64
}

func (g BudgetGuard) Enforce(usage Usage) error {
	if g.MaxUSD > 0 && usage.USD > g.MaxUSD {
		return fmt.Errorf("max-usd exceeded: used=%.4f limit=%.4f", usage.USD, g.MaxUSD)
	}
	if g.MaxTokens > 0 && usage.Tokens > g.MaxTokens {
		return fmt.Errorf("max-tokens exceeded: used=%d limit=%d", usage.Tokens, g.MaxTokens)
	}
	return nil
}
