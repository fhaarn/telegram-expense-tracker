package parser

import (
	"fmt"
	"math"
	"strings"
)

const MaxBatchItems = 20

// ParseBatch validates the entire message before any persistent draft is created.
func ParseBatch(text string) ([]Entry, error) {
	var entries []Entry
	var total int64
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, err := Parse(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: use a description followed by an amount, like Tahu Telor 20k", i+1)
		}
		if len(entries) == MaxBatchItems {
			return nil, fmt.Errorf("send at most %d items per message", MaxBatchItems)
		}
		if e.AmountMinor > math.MaxInt64-total {
			return nil, fmt.Errorf("batch total is too large")
		}
		total += e.AmountMinor
		entries = append(entries, e)
	}
	if len(entries) < 2 {
		return nil, fmt.Errorf("include at least two expense lines")
	}
	return entries, nil
}
