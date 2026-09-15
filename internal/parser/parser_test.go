package parser

import (
	"math"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		in, desc, key string
		amount        int64
	}{
		{"Tahu Telor\u00a020k", "Tahu Telor", "tahu telor", 2000000},
		{"Nice 8 Ball Cafe\u2003100k", "Nice 8 Ball Cafe", "nice 8 ball cafe", 10000000},
		{"Tahu Telor 20k", "Tahu Telor", "tahu telor", 2000000},
		{"Nice 8 Ball Cafe 100k", "Nice 8 Ball Cafe", "nice 8 ball cafe", 10000000},
		{"  BENSIN   25K  ", "BENSIN", "bensin", 2500000},
		{"Nice  8 Ball Cafe 1000", "Nice  8 Ball Cafe", "nice 8 ball cafe", 100000},
	} {
		got, err := Parse(tt.in)
		if err != nil || got.Description != tt.desc || got.Key != tt.key || got.AmountMinor != tt.amount {
			t.Fatalf("%q: %#v %v", tt.in, got, err)
		}
	}
	for _, in := range []string{"20k", "cafe 8 price", "cafe 0", "cafe -1", "cafe 1.5k", "cafe 1,000", "cafe 1m", "cafe 99999999999999999999999", "cafe 92233720368547759", "cafe +1", "cafe １２３"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
	n, e := ParseAmount("92233720368547758")
	if e != nil || n > math.MaxInt64 {
		t.Fatal("boundary rejected")
	}
	if FormatAmount(3500000) != "Rp35,000" {
		t.Fatal("format")
	}
}
