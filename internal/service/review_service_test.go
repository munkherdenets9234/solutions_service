package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/dto"
	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type fakeReviewStore struct {
	created []*models.Review
	byID    map[primitive.ObjectID]*models.Review
	updates []bson.M
}

func (f *fakeReviewStore) Create(_ context.Context, tenantID primitive.ObjectID, r *models.Review, _ *primitive.ObjectID) error {
	r.ID = primitive.NewObjectID()
	r.TenantID = tenantID
	f.created = append(f.created, r)
	return nil
}
func (f *fakeReviewStore) FindAll(context.Context, primitive.ObjectID, bson.M, int, int) ([]*models.Review, int64, error) {
	return nil, 0, nil
}
func (f *fakeReviewStore) FindByID(_ context.Context, _ primitive.ObjectID, id primitive.ObjectID) (*models.Review, error) {
	if r, ok := f.byID[id]; ok {
		return r, nil
	}
	return nil, mongo.ErrNoDocuments
}
func (f *fakeReviewStore) Update(_ context.Context, _ primitive.ObjectID, _ primitive.ObjectID, u bson.M, _ *primitive.ObjectID) error {
	f.updates = append(f.updates, u)
	return nil
}
func (f *fakeReviewStore) Delete(context.Context, primitive.ObjectID, primitive.ObjectID) (int64, error) {
	return 0, nil
}

// fakeCustomers is tenant-aware like the real repo: a customer is only
// visible under the tenant it belongs to.
type fakeCustomers struct {
	rows         []*models.Customer
	findCalls    int
	batchCalls   int
	batchTenant  primitive.ObjectID
	batchIDsSeen []primitive.ObjectID
}

func (f *fakeCustomers) FindByID(_ context.Context, tenantID, id primitive.ObjectID) (*models.Customer, error) {
	f.findCalls++
	for _, c := range f.rows {
		if c.ID == id && c.TenantID == tenantID {
			return c, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}
func (f *fakeCustomers) FindByIDs(_ context.Context, tenantID primitive.ObjectID, ids []primitive.ObjectID) ([]*models.Customer, error) {
	f.batchCalls++
	f.batchTenant = tenantID
	f.batchIDsSeen = ids
	var out []*models.Customer
	for _, c := range f.rows {
		for _, id := range ids {
			if c.ID == id && c.TenantID == tenantID {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func reviewBody() map[string]string { return map[string]string{"en": "Great"} }

func TestReviewCreateRejectsCustomerFromOtherTenant(t *testing.T) {
	tenantA, tenantB := primitive.NewObjectID(), primitive.NewObjectID()
	other := &models.Customer{ID: primitive.NewObjectID(), TenantID: tenantB, Name: "Zed"}
	store := &fakeReviewStore{}
	svc := &ReviewService{repo: store}
	svc.WithCustomers(&fakeCustomers{rows: []*models.Customer{other}})

	err := svc.Create(context.Background(), tenantA, &models.Review{Star: 5, Review: reviewBody(), CustomerID: &other.ID}, nil)
	assertStatus(t, err, http.StatusNotFound)
	missing := primitive.NewObjectID()
	err2 := svc.Create(context.Background(), tenantA, &models.Review{Star: 5, Review: reviewBody(), CustomerID: &missing}, nil)
	assertStatus(t, err2, http.StatusNotFound)
	if err.Error() != err2.Error() {
		t.Fatal("cross-tenant and unknown ids must look the same")
	}
	if len(store.created) != 0 {
		t.Fatal("review must not be created")
	}
}

func TestReviewCreateLinksCustomerAndFillsRelatedCustomer(t *testing.T) {
	tenant := primitive.NewObjectID()
	c := &models.Customer{ID: primitive.NewObjectID(), TenantID: tenant, Name: "Ana"}
	store := &fakeReviewStore{}
	svc := &ReviewService{repo: store}
	svc.WithCustomers(&fakeCustomers{rows: []*models.Customer{c}})

	if err := svc.Create(context.Background(), tenant, &models.Review{Star: 4, Review: reviewBody(), CustomerID: &c.ID}, nil); err != nil {
		t.Fatal(err)
	}
	got := store.created[0]
	if got.CustomerID == nil || *got.CustomerID != c.ID || got.RelatedCustomer != "Ana" {
		t.Fatalf("not linked/filled: %+v", got)
	}
	// An explicit related_customer is kept.
	if err := svc.Create(context.Background(), tenant, &models.Review{Star: 4, Review: reviewBody(), CustomerID: &c.ID, RelatedCustomer: "Ann B."}, nil); err != nil {
		t.Fatal(err)
	}
	if store.created[1].RelatedCustomer != "Ann B." {
		t.Fatal("explicit related_customer overwritten")
	}
}

func TestReviewCreateWithoutCustomerNeedsNoFinder(t *testing.T) {
	store := &fakeReviewStore{}
	svc := &ReviewService{repo: store}
	if err := svc.Create(context.Background(), primitive.NewObjectID(), &models.Review{Star: 4, Review: reviewBody()}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestReviewUpdateVerifiesCustomerAndStoresObjectID(t *testing.T) {
	tenant := primitive.NewObjectID()
	c := &models.Customer{ID: primitive.NewObjectID(), TenantID: tenant, Name: "Ana"}
	foreign := &models.Customer{ID: primitive.NewObjectID(), TenantID: primitive.NewObjectID(), Name: "Zed"}
	rev := &models.Review{ID: primitive.NewObjectID(), TenantID: tenant}
	store := &fakeReviewStore{byID: map[primitive.ObjectID]*models.Review{rev.ID: rev}}
	svc := &ReviewService{repo: store}
	svc.WithCustomers(&fakeCustomers{rows: []*models.Customer{c, foreign}})

	err := svc.Update(context.Background(), tenant, rev.ID.Hex(), bson.M{"customer_id": foreign.ID.Hex()}, nil)
	assertStatus(t, err, http.StatusNotFound)
	if len(store.updates) != 0 {
		t.Fatal("must not update")
	}
	if err := svc.Update(context.Background(), tenant, rev.ID.Hex(), bson.M{"customer_id": c.ID.Hex()}, nil); err != nil {
		t.Fatal(err)
	}
	u := store.updates[0]
	if id, ok := u["customer_id"].(primitive.ObjectID); !ok || id != c.ID {
		t.Fatalf("customer_id not stored as ObjectID: %#v", u["customer_id"])
	}
	if u["related_customer"] != "Ana" {
		t.Fatalf("related_customer not filled: %#v", u["related_customer"])
	}
}

func TestPublicReviewIncludesCustomerAvatar(t *testing.T) {
	tenant := primitive.NewObjectID()
	c := &models.Customer{ID: primitive.NewObjectID(), TenantID: tenant, Name: "Ana", Email: "ana@example.com", Phone: "999", Nationality: "MN", AvatarURL: "https://img.example/a.png"}
	svc := &ReviewService{repo: &fakeReviewStore{}}
	svc.WithCustomers(&fakeCustomers{rows: []*models.Customer{c}})
	linked := &models.Review{ID: primitive.NewObjectID(), Star: 5, Review: reviewBody(), CustomerID: &c.ID}
	plain := &models.Review{ID: primitive.NewObjectID(), Star: 5, Review: reviewBody()}

	avatars, err := svc.AvatarsFor(context.Background(), tenant, []*models.Review{linked, plain})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(dto.ToReviewResponsesWithAvatars([]*models.Review{linked, plain}, "en", avatars))
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out[0]["customer_avatar"] != "https://img.example/a.png" {
		t.Fatalf("avatar missing: %s", raw)
	}
	if _, has := out[1]["customer_avatar"]; has {
		t.Fatal("unlinked review must omit customer_avatar")
	}
	for _, banned := range []string{"email", "phone", "nationality", "ana@example.com", "999", "customer_id", "notes"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("public JSON leaks %q: %s", banned, raw)
		}
	}
}

func TestPublicReviewBatchesAvatarLookup(t *testing.T) {
	tenant := primitive.NewObjectID()
	fc := &fakeCustomers{}
	var reviews []*models.Review
	for i := 0; i < 5; i++ {
		c := &models.Customer{ID: primitive.NewObjectID(), TenantID: tenant, AvatarURL: "u"}
		fc.rows = append(fc.rows, c)
		id := c.ID
		reviews = append(reviews, &models.Review{CustomerID: &id})
	}
	reviews = append(reviews, &models.Review{}, &models.Review{CustomerID: reviews[0].CustomerID}) // unlinked + duplicate
	svc := &ReviewService{repo: &fakeReviewStore{}}
	svc.WithCustomers(fc)

	if _, err := svc.AvatarsFor(context.Background(), tenant, reviews); err != nil {
		t.Fatal(err)
	}
	if fc.batchCalls != 1 || fc.findCalls != 0 || fc.batchTenant != tenant || len(fc.batchIDsSeen) != 5 {
		t.Fatalf("batch=%d single=%d ids=%d", fc.batchCalls, fc.findCalls, len(fc.batchIDsSeen))
	}
	// No linked reviews: no query at all.
	fc2 := &fakeCustomers{}
	svc.WithCustomers(fc2)
	if _, err := svc.AvatarsFor(context.Background(), tenant, []*models.Review{{}}); err != nil || fc2.batchCalls != 0 {
		t.Fatal("must not query without linked reviews")
	}
}

func TestReviewUpdateRejectsDottedCustomerIDKey(t *testing.T) {
	tenant := primitive.NewObjectID()
	rev := &models.Review{ID: primitive.NewObjectID(), TenantID: tenant}
	for _, key := range []string{"customer_id.x", "customer_id.", "$set", "$unset", "$where"} {
		store := &fakeReviewStore{byID: map[primitive.ObjectID]*models.Review{rev.ID: rev}}
		svc := &ReviewService{repo: store}
		svc.WithCustomers(&fakeCustomers{})
		err := svc.Update(context.Background(), tenant, rev.ID.Hex(), bson.M{key: "x"}, nil)
		assertStatus(t, err, http.StatusBadRequest)
		if len(store.updates) != 0 {
			t.Fatalf("%q must not reach the store", key)
		}
	}
}

type failingCustomers struct{ fakeCustomers }

func (f *failingCustomers) FindByIDs(context.Context, primitive.ObjectID, []primitive.ObjectID) ([]*models.Customer, error) {
	return nil, mongo.ErrClientDisconnected
}

func TestPublicReviewListSurvivesAvatarLookupFailure(t *testing.T) {
	tenant := primitive.NewObjectID()
	id := primitive.NewObjectID()
	svc := &ReviewService{repo: &fakeReviewStore{}}
	svc.WithCustomers(&failingCustomers{})
	avatars := svc.PublicAvatars(context.Background(), tenant, []*models.Review{{CustomerID: &id}})
	if len(avatars) != 0 {
		t.Fatal("expected no avatars on failure")
	}
}
