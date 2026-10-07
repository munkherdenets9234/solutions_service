package service

import (
	"testing"
	"time"
)

// Booking, rental and transfer services hold concrete repositories, so their
// Create paths cannot run without MongoDB; the summary they all build is
// covered here instead.
func TestNotifySummary(t *testing.T) {
	d := time.Date(2027, 3, 4, 23, 30, 0, 0, time.UTC)
	cases := []struct {
		name, label string
		when        time.Time
		want        string
	}{
		{"Ada  Lovelace\n", "from", d, "Ada Lovelace - from 2027-03-04"},
		{"Bat", "pickup", time.Time{}, "Bat"},
		{"  ", "arrival", d, "(no name) - arrival 2027-03-04"},
	}
	for _, c := range cases {
		if got := notifySummary(c.name, c.label, c.when); got != c.want {
			t.Errorf("notifySummary(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
