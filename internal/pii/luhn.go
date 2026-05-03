package pii

// Luhn returns true if s passes the Luhn (mod-10) checksum used by
// every major credit-card issuer. Non-digits in s cause Luhn to return
// false; callers that want to validate a candidate range with internal
// spaces or dashes should strip them first via stripNonDigits.
//
// Empty input returns false: the empty product is 0, which is technically
// "valid" mod 10, but treating "" as a card number is never useful.
func Luhn(s string) bool {
	if s == "" {
		return false
	}
	sum := 0
	double := false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// stripNonDigits returns s with every non-digit byte removed. Used by
// the credit-card detector to apply Luhn to candidates that include
// formatting characters (spaces, dashes).
func stripNonDigits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
