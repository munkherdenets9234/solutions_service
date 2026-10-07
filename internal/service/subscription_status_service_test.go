package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/entitlement"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func providerReturning(e entitlement.Entitlement) entitlement.Provider {
	return entitlement.ProviderFunc(func(_ context.Context, id primitive.ObjectID) (entitlement.Entitlement, error) {
		e.TenantID = id
		return e, nil
	})
}

func TestActiveAndTrialingAreActive(t *testing.T) {
	for _, st := range []entitlement.Status{entitlement.StatusActive, entitlement.StatusTrialing} {
		s := NewSubscriptionStatusService(providerReturning(entitlement.Entitlement{Status: st}), zap.NewNop())
		if got := s.State(context.Background(), primitive.NewObjectID()); got != SubscriptionActive {
			t.Errorf("%s: got %q, want active", st, got)
		}
	}
}

func TestPastDueAndCanceledAreExpired(t *testing.T) {
	for _, st := range []entitlement.Status{entitlement.StatusPastDue, entitlement.StatusCanceled} {
		s := NewSubscriptionStatusService(providerReturning(entitlement.Entitlement{Status: st}), zap.NewNop())
		if got := s.State(context.Background(), primitive.NewObjectID()); got != SubscriptionExpired {
			t.Errorf("%s: got %q, want expired", st, got)
		}
	}
}

func TestActivePastPeriodEndIsExpired(t *testing.T) {
	s := NewSubscriptionStatusService(providerReturning(entitlement.Entitlement{
		Status: entitlement.StatusActive, PeriodEnd: time.Now().Add(-time.Hour),
	}), zap.NewNop())
	if got := s.State(context.Background(), primitive.NewObjectID()); got != SubscriptionExpired {
		t.Fatalf("got %q, want expired", got)
	}
}

func TestUnknownStatusIsActive(t *testing.T) {
	s := NewSubscriptionStatusService(providerReturning(entitlement.Entitlement{Status: entitlement.StatusUnknown}), zap.NewNop())
	if got := s.State(context.Background(), primitive.NewObjectID()); got != SubscriptionActive {
		t.Fatalf("got %q, want active", got)
	}
}

func TestProviderErrorIsActiveAndLogsIdOnly(t *testing.T) {
	const secretText = "dial tcp 10.9.8.7:5432: connection refused"
	core, logs := observer.New(zap.DebugLevel)
	s := NewSubscriptionStatusService(entitlement.ProviderFunc(
		func(context.Context, primitive.ObjectID) (entitlement.Entitlement, error) {
			return entitlement.Entitlement{}, errors.New(secretText)
		}), zap.New(core))
	id := primitive.NewObjectID()

	if got := s.State(context.Background(), id); got != SubscriptionActive {
		t.Fatalf("got %q, want active", got)
	}
	entries := logs.All()
	if len(entries) == 0 {
		t.Fatal("provider failure was not logged")
	}
	for _, e := range entries {
		rendered := fmt.Sprintf("%s %v", e.Message, e.ContextMap())
		if !strings.Contains(rendered, id.Hex()) {
			t.Errorf("log %q does not carry the tenant id", rendered)
		}
		if strings.Contains(rendered, secretText) || strings.Contains(rendered, "10.9.8.7") {
			t.Errorf("log %q leaks the provider's error text", rendered)
		}
		if _, ok := e.ContextMap()["error"]; ok {
			t.Errorf("log carries an error field: %v", e.ContextMap())
		}
	}
}

func TestStaleAnswerIsActive(t *testing.T) {
	s := NewSubscriptionStatusService(providerReturning(entitlement.Entitlement{
		Status: entitlement.StatusCanceled, Stale: true,
	}), zap.NewNop())
	if got := s.State(context.Background(), primitive.NewObjectID()); got != SubscriptionActive {
		t.Fatalf("got %q, want active (a stale answer is not proof)", got)
	}
}

func TestNilReceiverAndNilProviderAreActive(t *testing.T) {
	id := primitive.NewObjectID()
	var nilSvc *SubscriptionStatusService
	if got := nilSvc.State(context.Background(), id); got != SubscriptionActive {
		t.Errorf("nil receiver: got %q, want active", got)
	}
	if got := NewSubscriptionStatusService(nil, zap.NewNop()).State(context.Background(), id); got != SubscriptionActive {
		t.Errorf("nil provider: got %q, want active", got)
	}
	// A nil logger must not panic on the error path.
	failing := entitlement.ProviderFunc(func(context.Context, primitive.ObjectID) (entitlement.Entitlement, error) {
		return entitlement.Entitlement{}, errors.New("x")
	})
	if got := NewSubscriptionStatusService(failing, nil).State(context.Background(), id); got != SubscriptionActive {
		t.Errorf("nil logger: got %q, want active", got)
	}
}
