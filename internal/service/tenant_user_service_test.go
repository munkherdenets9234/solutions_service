package service

import (
	"context"
	"testing"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// recvStore is a minimal in-memory TenantUserStore for the receive_emails rules.
type recvStore struct {
	users map[primitive.ObjectID]*models.TenantUser
}

func newRecvStore() *recvStore { return &recvStore{users: map[primitive.ObjectID]*models.TenantUser{}} }

func (f *recvStore) Create(_ context.Context, u *models.TenantUser) error {
	u.ID = primitive.NewObjectID()
	u.Status = models.TenantUserActive
	f.users[u.ID] = u
	return nil
}
func (f *recvStore) FindAll(context.Context, primitive.ObjectID, int, int) ([]*models.TenantUser, int64, error) {
	return nil, 0, nil
}
func (f *recvStore) FindByTenantAndEmail(context.Context, primitive.ObjectID, string) (*models.TenantUser, error) {
	return nil, mongo.ErrNoDocuments
}
func (f *recvStore) FindByID(_ context.Context, t, id primitive.ObjectID) (*models.TenantUser, error) {
	if u, ok := f.users[id]; ok && u.TenantID == t {
		return u, nil
	}
	return nil, mongo.ErrNoDocuments
}
func (f *recvStore) FindAdmins(context.Context, primitive.ObjectID) ([]*models.TenantUser, error) {
	return nil, nil
}
func (f *recvStore) UpdatePassword(context.Context, primitive.ObjectID, primitive.ObjectID, string) error {
	return nil
}
func (f *recvStore) UpdateStatus(context.Context, primitive.ObjectID, primitive.ObjectID, models.TenantUserStatus) error {
	return nil
}
func (f *recvStore) FindEmailRecipients(context.Context, primitive.ObjectID) ([]*models.TenantUser, error) {
	return nil, nil
}
func (f *recvStore) SetReceiveEmails(_ context.Context, t, id primitive.ObjectID, v bool) error {
	u, ok := f.users[id]
	if !ok || u.TenantID != t {
		return mongo.ErrNoDocuments
	}
	u.ReceiveEmails = v
	return nil
}

func boolPtr(b bool) *bool { return &b }

func TestCreateAndUpdatePersistReceiveEmails(t *testing.T) {
	ctx := context.Background()
	tenant := primitive.NewObjectID()
	st := newRecvStore()
	svc := NewTenantUserServiceFromStore(st, nil, 1)

	// Default is false.
	def, _, err := svc.Create(ctx, tenant, "A", "a@example.test", "", models.TenantUserStaff, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.users[def.ID].ReceiveEmails {
		t.Fatal("default must be false")
	}

	// Create persists true.
	u, _, err := svc.Create(ctx, tenant, "B", "b@example.test", "", models.TenantUserStaff, true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.users[u.ID].ReceiveEmails {
		t.Fatal("create did not persist receive_emails=true")
	}

	// Update with a missing field must not reset the stored flag.
	if err := svc.UpdateReceiveEmails(ctx, tenant, u.ID.Hex(), nil); err != nil {
		t.Fatal(err)
	}
	if !st.users[u.ID].ReceiveEmails {
		t.Fatal("nil receive_emails reset the stored flag")
	}

	// Explicit false and true are applied.
	if err := svc.UpdateReceiveEmails(ctx, tenant, u.ID.Hex(), boolPtr(false)); err != nil {
		t.Fatal(err)
	}
	if st.users[u.ID].ReceiveEmails {
		t.Fatal("explicit false not applied")
	}
	if err := svc.UpdateReceiveEmails(ctx, tenant, u.ID.Hex(), boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	if !st.users[u.ID].ReceiveEmails {
		t.Fatal("explicit true not applied")
	}

	// Other tenant cannot touch the user; bad id rejected.
	if err := svc.UpdateReceiveEmails(ctx, primitive.NewObjectID(), u.ID.Hex(), boolPtr(false)); err == nil {
		t.Fatal("cross-tenant update must fail")
	}
	if err := svc.UpdateReceiveEmails(ctx, tenant, "nope", boolPtr(true)); err == nil {
		t.Fatal("invalid id must fail")
	}
}
