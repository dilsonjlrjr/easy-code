package http

import (
	"math/big"
	"strings"
)

// decimalSum adds decimals in text without floating point (Money, RNF-16).
func decimalSum(values ...string) string {
	total := new(big.Rat)
	for _, value := range values {
		if value == "" {
			continue
		}
		if r, ok := new(big.Rat).SetString(value); ok {
			total.Add(total, r)
		}
	}
	text := total.FloatString(8)
	if strings.Contains(text, ".") {
		text = strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
	}
	return text
}
