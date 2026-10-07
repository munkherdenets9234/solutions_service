package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type fakeMailLogStore struct {
	rows       []*models.MailOutbox
	listTenant primitive.ObjectID
	listStatus models.MailStatus
	listCalls  int
	resetErr   error
	resetTen   primitive.ObjectID
	resetID    primitive.ObjectID
	resetCalls int
}

func (f *fakeMailLogStore) List(_ context.Context, tenantID primitive.ObjectID, status models.MailStatus, _, _ int) ([]*models.MailOutbox, int64, error) {
	f.listCalls++
	f.listTenant, f.listStatus = tenantID, status
	return f.rows, int64(len(f.rows)), nil
}

func (f *fakeMailLogStore) ResetFailed(_ context.Context, tenantID, id primitive.ObjectID) error {
	f.resetCalls++
	f.resetTen, f.resetID = tenantID, id
	return f.resetErr
}

func apiStatus(t *testing.T, err error) int {
	t.Helper()
	var ae *apierr.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("error %v is not an APIError", err)
	}
	return ae.HTTPStatus
}

func TestListRejectsUnknownStatus(t *testing.T) {
	f := &fakeMailLogStore{}
	s := NewMailOutboxService(f)
	for _, st := range []string{"queued", "SENT", "sent ", "$ne"} {
		_, _, err := s.List(context.Background(), primitive.NewObjectID(), st, 1, 20)
		if err == nil || apiStatus(t, err) != http.StatusBadRequest {
			t.Errorf("status %q: err = %v, want 400", st, err)
		}
	}
	if f.listCalls != 0 {
		t.Errorf("repo called %d times for a rejected status", f.listCalls)
	}
	for _, st := range []string{"", "pending", "sent", "failed"} {
		if _, _, err := s.List(context.Background(), primitive.NewObjectID(), st, 1, 20); err != nil {
			t.Errorf("status %q: unexpected error %v", st, err)
		}
	}
}

func TestListIsTenantScoped(t *testing.T) {
	f := &fakeMailLogStore{}
	s := NewMailOutboxService(f)
	tenant := primitive.NewObjectID()
	if _, _, err := s.List(context.Background(), tenant, "failed", 1, 20); err != nil {
		t.Fatal(err)
	}
	if f.listTenant != tenant {
		t.Errorf("repo got tenant %s, want the caller's %s", f.listTenant.Hex(), tenant.Hex())
	}
	if f.listStatus != models.MailFailed {
		t.Errorf("repo got status %q", f.listStatus)
	}
}

func TestRetryResetsOnlyFailedRows(t *testing.T) {
	tenant, id := primitive.NewObjectID(), primitive.NewObjectID()

	f := &fakeMailLogStore{}
	if err := NewMailOutboxService(f).Retry(context.Background(), tenant, id.Hex()); err != nil {
		t.Fatal(err)
	}
	if f.resetTen != tenant || f.resetID != id {
		t.Errorf("reset got (%s,%s)", f.resetTen.Hex(), f.resetID.Hex())
	}

	// Not failed, other tenant, missing: the repo says ErrNoDocuments, all 404.
	f = &fakeMailLogStore{resetErr: mongo.ErrNoDocuments}
	err := NewMailOutboxService(f).Retry(context.Background(), tenant, id.Hex())
	if err == nil || apiStatus(t, err) != http.StatusNotFound {
		t.Errorf("ErrNoDocuments: err = %v, want 404", err)
	}

	// A malformed id is a 400 and never reaches the repo.
	f = &fakeMailLogStore{}
	err = NewMailOutboxService(f).Retry(context.Background(), tenant, "not-an-id")
	if err == nil || apiStatus(t, err) != http.StatusBadRequest {
		t.Errorf("bad id: err = %v, want 400", err)
	}
	if f.resetCalls != 0 {
		t.Error("repo called for a bad id")
	}

	// Any other repo failure is a generic 500.
	f = &fakeMailLogStore{resetErr: errors.New("boom secret detail")}
	err = NewMailOutboxService(f).Retry(context.Background(), tenant, id.Hex())
	if err == nil || apiStatus(t, err) != http.StatusInternalServerError {
		t.Errorf("repo failure: err = %v, want 500", err)
	}
}

func TestListMasksRecipient(t *testing.T) {
	cases := []struct{ in, want string }{
		{"alice@example.com", "a***@example.com"},
		{"a@x.io", "a***@x.io"},
		{"no-at-sign", "***"},
		{"", "***"},
		{"@example.com", "***@example.com"},
		{"élan@example.com", "é***@example.com"},
	}
	for _, c := range cases {
		f := &fakeMailLogStore{rows: []*models.MailOutbox{{To: c.in}}}
		items, _, err := NewMailOutboxService(f).List(context.Background(), primitive.NewObjectID(), "", 1, 20)
		if err != nil || len(items) != 1 {
			t.Fatalf("%q: %v %d", c.in, err, len(items))
		}
		if items[0].To != c.want {
			t.Errorf("mask(%q) = %q, want %q", c.in, items[0].To, c.want)
		}
	}
}

func TestListDoesNotExposeData(t *testing.T) {
	sent := time.Now()
	row := &models.MailOutbox{
		ID: primitive.NewObjectID(), To: "alice@example.com", Kind: "booking", RecordID: primitive.NewObjectID(),
		Status: models.MailSent, Attempts: 1, SentAt: &sent, CreatedAt: sent,
		Data: map[string]string{"token": "SECRET", "unsubscribe_url": "https://x/unsub?t=SECRET"},
	}
	f := &fakeMailLogStore{rows: []*models.MailOutbox{row}}
	items, _, err := NewMailOutboxService(f).List(context.Background(), primitive.NewObjectID(), "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(items[0])
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"id": true, "kind": true, "record_id": true, "to": true, "status": true,
		"attempts": true, "last_error": true, "created_at": true, "sent_at": true}
	for k := range m {
		if !want[k] {
			t.Errorf("unexpected key %q in the log item", k)
		}
	}
	for k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	for _, bad := range []string{"data", "token", "unsubscribe", "alice@example.com", "SECRET", "user_id", "tenant_id"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("JSON leaks %q: %s", bad, b)
		}
	}
}
