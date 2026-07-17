package repository

import "go.mongodb.org/mongo-driver/bson"

// protectedUpdateFields are keys a caller-supplied $set map must never touch —
// they're owned by the tenant-scoping/write path, not the client. Without
// this, a client PUT body containing "tenant_id" would silently reassign a
// document to another tenant, since the update's match filter (_id +
// tenant_id) is checked before the $set is applied, not after. user_id is
// protected for the same reason: it's the audit link to tenant_users and
// must only ever be set from the authenticated caller's token, never from
// client-supplied JSON.
var protectedUpdateFields = []string{"_id", "tenant_id", "created_at", "user_id"}

func stripProtectedFields(update bson.M) {
	for _, k := range protectedUpdateFields {
		delete(update, k)
	}
}
