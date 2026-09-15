package parser

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Entry struct {
	Description, Key string
	AmountMinor      int64
}

func Normalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func ParseAmount(s string) (int64, error) {
	multiplier := int64(100)
	if strings.HasSuffix(s, "k") || strings.HasSuffix(s, "K") {
		multiplier = 100000
		s = s[:len(s)-1]
	}
	if s == "" {
		return 0, errors.New("missing amount")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("use digits with optional k")
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/multiplier {
		return 0, errors.New("amount out of range")
	}
	return n * multiplier, nil
}
func Parse(s string) (Entry, error) {
	if !utf8.ValidString(s) {
		return Entry{}, errors.New("invalid description")
	}
	s = strings.TrimSpace(s)
	end := strings.LastIndexFunc(s, unicode.IsSpace)
	if end < 0 {
		return Entry{}, errors.New("include description and amount")
	}
	description := strings.TrimSpace(s[:end])
	if description == "" || utf8.RuneCountInString(description) > 200 {
		return Entry{}, errors.New("description must be 1–200 characters")
	}
	for _, r := range description {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return Entry{}, errors.New("invalid description")
		}
	}
	_, separatorBytes := utf8.DecodeRuneInString(s[end:])
	n, err := ParseAmount(s[end+separatorBytes:])
	if err != nil {
		return Entry{}, err
	}
	return Entry{description, Normalize(description), n}, nil
}
func FormatAmount(minor int64) string {
	s := strconv.FormatInt(minor/100, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "Rp" + s
}
