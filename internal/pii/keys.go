package pii

import "strings"

// sensitiveKey is the value stored in sensitiveKeys: the kind code and
// tier that a key-context match should emit.
type sensitiveKey struct {
	Code string
	Tier Tier
}

// sensitiveKeys maps lowercase JSON key names to their PII kind. Lookup
// is case-insensitive (callers should pass the lowercase key, or use
// sensitiveKeyLookup which lowercases for them).
//
// The list is hard-coded for v1. A future config-driven extension can
// merge additional keys at startup without changing API shape.
var sensitiveKeys = map[string]sensitiveKey{
	// names
	"name":        {"name", TierIdentifying},
	"full_name":   {"name", TierIdentifying},
	"fullname":    {"name", TierIdentifying},
	"first_name":  {"name", TierIdentifying},
	"firstname":   {"name", TierIdentifying},
	"given_name":  {"name", TierIdentifying},
	"givenname":   {"name", TierIdentifying},
	"last_name":   {"name", TierIdentifying},
	"lastname":    {"name", TierIdentifying},
	"family_name": {"name", TierIdentifying},
	"familyname":  {"name", TierIdentifying},
	"surname":     {"name", TierIdentifying},

	// addresses
	"address":        {"address", TierIdentifying},
	"street":         {"address", TierIdentifying},
	"street_address": {"address", TierIdentifying},
	"city":           {"address", TierIdentifying},
	"postal_code":    {"address", TierIdentifying},
	"postcode":       {"address", TierIdentifying},
	"zip":            {"address", TierIdentifying},
	"zipcode":        {"address", TierIdentifying},

	// dates of birth
	"dob":           {"dob", TierSensitive},
	"birthday":      {"dob", TierSensitive},
	"date_of_birth": {"dob", TierSensitive},
	"dateofbirth":   {"dob", TierSensitive},
	"birth_date":    {"dob", TierSensitive},

	// phone
	"phone":        {"phone", TierIdentifying},
	"phone_number": {"phone", TierIdentifying},
	"phonenumber":  {"phone", TierIdentifying},
	"mobile":       {"phone", TierIdentifying},
	"cellphone":    {"phone", TierIdentifying},
	"tel":          {"phone", TierIdentifying},
	"telephone":    {"phone", TierIdentifying},

	// ssn
	"ssn":                    {"ssn", TierSensitive},
	"social_security_number": {"ssn", TierSensitive},

	// credit card
	"credit_card": {"credit_card", TierSensitive},
	"creditcard":  {"credit_card", TierSensitive},
	"card_number": {"credit_card", TierSensitive},
	"cardnumber":  {"credit_card", TierSensitive},
	"cc_number":   {"credit_card", TierSensitive},
	"pan":         {"credit_card", TierSensitive},
}

// sensitiveKeyLookup performs the case-insensitive lookup. Returns the
// matching sensitiveKey and true if found, zero-value and false otherwise.
//
// Callers also need to handle camelCase variants — Go's strings package
// has no built-in "camelCase to snake_case" so we add the most common
// variants explicitly above (firstName, lastName, etc.).
func sensitiveKeyLookup(key string) (sensitiveKey, bool) {
	lk := strings.ToLower(key)
	v, ok := sensitiveKeys[lk]
	return v, ok
}
