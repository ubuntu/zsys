package testutils

import "time"

// FixedTime is the fixed "now" used for testing
type FixedTime struct{}

// Now returns fixed time for testing
func (FixedTime) Now() time.Time {
	t, err := time.Parse(time.RFC3339, "2020-01-01T12:00:00+00:00")
	if err != nil {
		panic(err)
	}
	return t
}
