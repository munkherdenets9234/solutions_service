package repository

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The repository methods need MongoDB, so they are exercised live. What can be
// pinned without one is the filters, where the rules live.

// Review Focus 1. The tenant is part of the lookup, so a code issued for one
// tenant's user can never be found, let alone used, for another tenant's user
// who happens to share the email address.
func TestActiveFilterIsScopedToTheTenant(t *testing.T) {
	tenant := primitive.NewObjectID()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	want := bson.M{
		"tenant_id":  tenant,
		"email":      "a@example.com",
		"used_at":    bson.M{"$exists": false},
		"expires_at": bson.M{"$gt": now},
	}
	if got := activeFilter(tenant, "a@example.com", now); !reflect.DeepEqual(got, want) {
		t.Fatalf("activeFilter = %#v\nwant %#v", got, want)
	}
}

func TestInvalidateFilterIsScopedToTheTenantAndUser(t *testing.T) {
	tenant, user := primitive.NewObjectID(), primitive.NewObjectID()

	want := bson.M{
		"tenant_id": tenant,
		"user_id":   user,
		"used_at":   bson.M{"$exists": false},
	}
	if got := invalidateFilter(tenant, user); !reflect.DeepEqual(got, want) {
		t.Fatalf("invalidateFilter = %#v\nwant %#v", got, want)
	}
}
