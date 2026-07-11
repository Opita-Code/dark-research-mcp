package research

import "time"

// parseRFC3339OrZero parses an RFC 3339 timestamp. Returns zero time
// if input is empty or malformed.
func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseRFC3339DateOrZero parses a date-only string ("2024-03-29").
// Falls back to empty time on parse error.
func parseRFC3339DateOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}