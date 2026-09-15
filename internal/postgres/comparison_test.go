package postgres

import "testing"

func TestInviteHash(t *testing.T) {
	for _, token := range []string{"", "abc", "ABCDEF0123456789ABCDEF0123456789", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		if _, err := inviteHash(token); err == nil {
			t.Fatalf("accepted malformed token %q", token)
		}
	}
	token := "abcdef0123456789abcdef0123456789"
	h, err := inviteHash(token)
	if err != nil || len(h) != 64 || h == token {
		t.Fatal("invalid digest")
	}
	again, _ := inviteHash(token)
	if again != h {
		t.Fatal("unstable digest")
	}
}
func TestComparisonMoney(t *testing.T) {
	for raw, want := range map[string]string{"0": "Rp0", "10000000": "Rp100,000", "-10000000": "Rp-100,000", "922337203685477580800": "Rp9,223,372,036,854,775,808"} {
		if got := comparisonMoney(raw); got != want {
			t.Fatalf("%s: %s != %s", raw, got, want)
		}
	}
}
