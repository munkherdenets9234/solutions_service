package models

import (
	"testing"
	"time"
)

func TestTenantPasswordResetSpent(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	used := now.Add(-time.Minute)

	cases := []struct {
		name string
		p    TenantPasswordReset
		want bool
	}{
		{"fresh", TenantPasswordReset{ExpiresAt: now.Add(5 * time.Minute)}, false},
		{"one guess short of the cap", TenantPasswordReset{ExpiresAt: now.Add(5 * time.Minute), Attempts: MaxResetAttempts - 1}, false},
		{"already used", TenantPasswordReset{ExpiresAt: now.Add(5 * time.Minute), UsedAt: &used}, true},
		{"expired", TenantPasswordReset{ExpiresAt: now.Add(-time.Second)}, true},
		// Six digits is only safe because guessing is capped: at the cap the
		// code is dead even if it has not expired and has not been used.
		{"at the attempt cap", TenantPasswordReset{ExpiresAt: now.Add(5 * time.Minute), Attempts: MaxResetAttempts}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Spent(now); got != tc.want {
				t.Fatalf("Spent = %v, want %v", got, tc.want)
			}
		})
	}
}
