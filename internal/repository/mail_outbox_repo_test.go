package repository

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The repository methods need MongoDB, so they are exercised live. What can be
// pinned without one is the filters, updates and index definitions, where the
// rules live. That a claim is exclusive comes from ClaimDue being one atomic
// FindOneAndUpdate; that is not provable here.

var outboxNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// A claim takes only pending rows that are due, so a sent or failed row, or one
// still waiting for its retry (or under another worker's lease), is skipped.
func TestClaimFilterSkipsFutureAndNonPending(t *testing.T) {
	want := bson.M{
		"status":          models.MailPending,
		"next_attempt_at": bson.M{"$lte": outboxNow},
	}
	if got := claimFilter(outboxNow); !reflect.DeepEqual(got, want) {
		t.Fatalf("claimFilter = %#v\nwant %#v", got, want)
	}
}

// The claim is a lease: next_attempt_at moves out, nothing else changes, so a
// crashed worker's row becomes due again on its own.
func TestClaimUpdateMovesNextAttemptByLease(t *testing.T) {
	lease := 2 * time.Minute
	want := bson.M{"$set": bson.M{
		"next_attempt_at": outboxNow.Add(lease),
		"updated_at":      outboxNow,
	}}
	if got := claimUpdate(outboxNow, lease); !reflect.DeepEqual(got, want) {
		t.Fatalf("claimUpdate = %#v\nwant %#v", got, want)
	}
}

func TestClaimSortsOldestDueFirst(t *testing.T) {
	want := bson.D{{Key: "next_attempt_at", Value: 1}}
	if got := claimSort(); !reflect.DeepEqual(got, want) {
		t.Fatalf("claimSort = %#v\nwant %#v", got, want)
	}
}

func TestResetFailedIsTenantScopedAndOnlyFailed(t *testing.T) {
	tenant, id := primitive.NewObjectID(), primitive.NewObjectID()
	wantF := bson.M{"_id": id, "tenant_id": tenant, "status": models.MailFailed}
	if got := resetFailedFilter(tenant, id); !reflect.DeepEqual(got, wantF) {
		t.Fatalf("resetFailedFilter = %#v\nwant %#v", got, wantF)
	}
	wantU := bson.M{"$set": bson.M{
		"status":          models.MailPending,
		"attempts":        0,
		"next_attempt_at": outboxNow,
		"updated_at":      outboxNow,
	}}
	if got := resetFailedUpdate(outboxNow); !reflect.DeepEqual(got, wantU) {
		t.Fatalf("resetFailedUpdate = %#v\nwant %#v", got, wantU)
	}
}

func TestCancelPendingOnlyTouchesPending(t *testing.T) {
	tenant, user := primitive.NewObjectID(), primitive.NewObjectID()
	want := bson.M{"tenant_id": tenant, "user_id": user, "status": models.MailPending}
	if got := cancelPendingFilter(tenant, user); !reflect.DeepEqual(got, want) {
		t.Fatalf("cancelPendingFilter = %#v\nwant %#v", got, want)
	}
}

func TestListFilterIsTenantScopedAndStatusOptional(t *testing.T) {
	tenant := primitive.NewObjectID()
	if got, want := listFilter(tenant, ""), (bson.M{"tenant_id": tenant}); !reflect.DeepEqual(got, want) {
		t.Fatalf("listFilter(no status) = %#v\nwant %#v", got, want)
	}
	want := bson.M{"tenant_id": tenant, "status": models.MailFailed}
	if got := listFilter(tenant, models.MailFailed); !reflect.DeepEqual(got, want) {
		t.Fatalf("listFilter(failed) = %#v\nwant %#v", got, want)
	}
}

// A failure keeps only a short code; provider text never reaches the row.
func TestAttemptFailedUpdateBoundsTheErrorCode(t *testing.T) {
	next := outboxNow.Add(time.Minute)
	long := strings.Repeat("x", 500)

	got := attemptFailedUpdate(outboxNow, long, 2, next, false)
	set := got["$set"].(bson.M)
	if code := set["last_error"].(string); len(code) != 64 {
		t.Fatalf("last_error length = %d, want 64", len(code))
	}
	want := bson.M{
		"status":          models.MailPending,
		"attempts":        2,
		"next_attempt_at": next,
		"last_error":      strings.Repeat("x", 64),
		"updated_at":      outboxNow,
	}
	if !reflect.DeepEqual(set, want) {
		t.Fatalf("retry set = %#v\nwant %#v", set, want)
	}

	final := attemptFailedUpdate(outboxNow, "upstream", 5, next, true)["$set"].(bson.M)
	if final["status"] != models.MailFailed || final["last_error"] != "upstream" {
		t.Fatalf("final set = %#v", final)
	}
}

func TestSentUpdateClearsErrorAndStampsSentAt(t *testing.T) {
	want := bson.M{"$set": bson.M{
		"status":     models.MailSent,
		"sent_at":    outboxNow,
		"last_error": "",
		"updated_at": outboxNow,
	}}
	if got := sentUpdate(outboxNow); !reflect.DeepEqual(got, want) {
		t.Fatalf("sentUpdate = %#v\nwant %#v", got, want)
	}
}

func TestTruncateErrCodeKeepsRunes(t *testing.T) {
	if got := truncateErrCode(strings.Repeat("é", 100)); len([]rune(got)) != 64 {
		t.Fatalf("rune length = %d, want 64", len([]rune(got)))
	}
	if got := truncateErrCode("rate_limited"); got != "rate_limited" {
		t.Fatalf("short code changed: %q", got)
	}
}

func TestMailOutboxIndexes(t *testing.T) {
	idx := mailOutboxIndexes()
	if len(idx) != 3 {
		t.Fatalf("got %d indexes, want 3", len(idx))
	}
	wantKeys := []bson.D{
		{{Key: "status", Value: 1}, {Key: "next_attempt_at", Value: 1}},
		{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}, {Key: "created_at", Value: -1}},
		{{Key: "created_at", Value: 1}},
	}
	for i, w := range wantKeys {
		if !reflect.DeepEqual(idx[i].Keys, w) {
			t.Errorf("index %d keys = %#v\nwant %#v", i, idx[i].Keys, w)
		}
	}
	ttl := idx[2].Options
	if ttl == nil || ttl.ExpireAfterSeconds == nil || *ttl.ExpireAfterSeconds != 2592000 {
		t.Fatalf("TTL index must expire after 2592000s (30 days), got %#v", ttl)
	}
}

// A worker that overruns its lease must not overwrite a row another worker
// already finished: the outcome is written only while the row is still pending.
func TestMarkFilterOnlyMatchesPending(t *testing.T) {
	id := primitive.NewObjectID()
	want := bson.M{"_id": id, "status": models.MailPending}
	if got := markFilter(id); !reflect.DeepEqual(got, want) {
		t.Fatalf("markFilter = %#v\nwant %#v", got, want)
	}
}
