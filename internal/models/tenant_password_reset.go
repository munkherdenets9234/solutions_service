package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TenantPasswordReset is one outstanding reset code for one tenant user.
//
// Three things here are security decisions, not storage details:
//
//  1. CodeHash, never the code. A reset code stands in for a password for as
//     long as it lives, so anyone who can read the database (a backup, a slow
//     query log) would otherwise take over every account with a pending reset.
//
//  2. Attempts, with a ceiling. Six digits is a million possibilities, which
//     sounds like a lot and is not: unlimited guesses in a ten-minute window is
//     a few minutes of scripted requests. The counter is what makes the code
//     short enough to type and still safe.
//
//  3. TenantID. Users are unique per tenant, not globally, so two tenants can
//     have a user with the same email. A code is only ever looked up together
//     with its tenant, so one tenant's code can never be used against the
//     other's user.
type TenantPasswordReset struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TenantID primitive.ObjectID `bson:"tenant_id" json:"tenant_id"`
	UserID   primitive.ObjectID `bson:"user_id" json:"user_id"`
	Email    string             `bson:"email" json:"email"`
	CodeHash string             `bson:"code_hash" json:"-"`

	Attempts int `bson:"attempts" json:"attempts"`

	ExpiresAt time.Time  `bson:"expires_at" json:"expires_at"`
	UsedAt    *time.Time `bson:"used_at,omitempty" json:"used_at,omitempty"`
	CreatedAt time.Time  `bson:"created_at" json:"created_at"`
}

// MaxResetAttempts is how many wrong codes one reset tolerates before it is
// burned. A person reading a code out of their inbox does not need more.
const MaxResetAttempts = 5

// Spent reports whether this code can no longer be used, for any reason. One
// predicate, so a caller cannot check expiry and forget replay.
func (p *TenantPasswordReset) Spent(now time.Time) bool {
	return p.UsedAt != nil || now.After(p.ExpiresAt) || p.Attempts >= MaxResetAttempts
}
