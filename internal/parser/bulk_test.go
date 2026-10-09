package parser

import (
	"strings"
	"testing"
)

func TestParseBatch(t *testing.T) {
	entries, err := ParseBatch("Tahu Telor 20k\n\nNice 8 Ball Cafe 100k\r\n")
	if err != nil || len(entries) != 2 || entries[1].Description != "Nice 8 Ball Cafe" || entries[0].AmountMinor != 2000000 {
		t.Fatal(entries, err)
	}
	for _, tc := range []struct{ text, want string }{{"ok 20k\nbad", "line 2"}, {"one 20k", "at least two"}, {strings.Repeat("a 1k\n", 21), "at most 20"}, {"a 92233720368547758\nb 92233720368547758", "total is too large"}} {
		if _, err := ParseBatch(tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: %v", tc.want, err)
		}
	}
}
