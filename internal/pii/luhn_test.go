package pii

import "testing"

func TestLuhn(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"4111111111111111", true},  // canonical Visa test number
		{"4111111111111112", false}, // single digit flipped
		{"5555555555554444", true},  // canonical Mastercard test
		{"378282246310005", true},   // Amex test
		{"6011111111111117", true},  // Discover test
		{"", false},
		{"abc", false},
		{"0", false},
		{"1234567890123", false}, // unlikely to pass — random
	}
	for _, tc := range cases {
		got := Luhn(tc.in)
		if got != tc.want {
			t.Errorf("Luhn(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
