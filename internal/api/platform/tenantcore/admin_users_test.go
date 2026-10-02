package tenantcore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/token"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// fakeStore stands in for the tenant_users collection. FindByID deliberately
// ignores the tenant, so the tests prove the service's own tenant check
// rather than leaning on the query that the real repository also applies.
type fakeStore struct {
	users []*models.TenantUser
}

func (f *fakeStore) Create(context.Context, *models.TenantUser) error { return nil }
func (f *fakeStore) FindAll(context.Context, primitive.ObjectID, int, int) ([]*models.TenantUser, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) FindByTenantAndEmail(context.Context, primitive.ObjectID, string) (*models.TenantUser, error) {
	return nil, mongo.ErrNoDocuments
}
func (f *fakeStore) UpdatePassword(context.Context, primitive.ObjectID, primitive.ObjectID, string) error {
	return nil
}
func (f *fakeStore) UpdateStatus(context.Context, primitive.ObjectID, primitive.ObjectID, models.TenantUserStatus) error {
	return nil
}
func (f *fakeStore) FindByID(_ context.Context, _ primitive.ObjectID, id primitive.ObjectID) (*models.TenantUser, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}

// FindAdmins returns every user of the tenant, staff included: the service
// must not trust the store to have filtered.
func (f *fakeStore) FindAdmins(_ context.Context, tenantID primitive.ObjectID) ([]*models.TenantUser, error) {
	var out []*models.TenantUser
	for _, u := range f.users {
		if u.TenantID == tenantID {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

type resetCall struct {
	tenant primitive.ObjectID
	email  string
}

type fakeReset struct {
	calls []resetCall
	err   error
}

func (f *fakeReset) Request(_ context.Context, tenantID primitive.ObjectID, email string) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, resetCall{tenantID, email})
	return nil
}

type harness struct {
	e      *gin.Engine
	store  *fakeStore
	reset  *fakeReset
	priv   ed25519.PrivateKey
	tenant primitive.ObjectID
	admin  *models.TenantUser
	staff  *models.TenantUser
	susp   *models.TenantUser
	other  *models.TenantUser
}

func newUser(tenant primitive.ObjectID, email string, role models.TenantUserRole, status models.TenantUserStatus) *models.TenantUser {
	return &models.TenantUser{
		ID: primitive.NewObjectID(), TenantID: tenant, Name: "N " + email, Email: email,
		PasswordHash: "HASH-MUST-NOT-LEAK", Role: role, Status: status,
	}
}

func newHarness(t *testing.T, withKey bool) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var v *token.TenantcoreVerifier
	if withKey {
		if v, err = token.NewVerifier(base64.StdEncoding.EncodeToString(pub)); err != nil {
			t.Fatal(err)
		}
	}

	h := &harness{priv: priv, tenant: primitive.NewObjectID(), reset: &fakeReset{}}
	otherTenant := primitive.NewObjectID()
	h.admin = newUser(h.tenant, "boss@example.test", models.TenantUserAdmin, models.TenantUserActive)
	h.staff = newUser(h.tenant, "staff@example.test", models.TenantUserStaff, models.TenantUserActive)
	h.susp = newUser(h.tenant, "gone@example.test", models.TenantUserAdmin, models.TenantUserSuspended)
	h.other = newUser(otherTenant, "elsewhere@example.test", models.TenantUserAdmin, models.TenantUserActive)
	h.store = &fakeStore{users: []*models.TenantUser{h.admin, h.staff, h.susp, h.other}}

	h.e = gin.New()
	h.e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	Register(h.e.Group("/api/v1/platform"), Deps{
		Auth:      middleware.NewTenantcoreAuth(v),
		Users:     service.NewTenantUserServiceFromStore(h.store, nil, 1),
		Reset:     h.reset,
		RateLimit: func(c *gin.Context) { c.Next() },
	})
	return h
}

func (h *harness) tcToken(t *testing.T, role string) string {
	t.Helper()
	claims := token.TenantcoreClaims{
		UserID: "op1", Role: token.TenantcoreRole(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    token.TenantcoreIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(h.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (h *harness) do(method, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.e.ServeHTTP(w, req)
	return w
}

func (h *harness) listPath() string {
	return "/api/v1/platform/tenants/" + h.tenant.Hex() + "/admin-users"
}

func (h *harness) resetPath(u *models.TenantUser) string {
	return h.listPath() + "/" + u.ID.Hex() + "/reset-password"
}

func TestListAdminUsers_ReturnsAdminsOnly(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodGet, h.listPath(), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("want 2 admins (active + suspended, no staff, no other tenant), got %d: %s", len(body.Data), w.Body.String())
	}
	if body.Data[0]["email"] != "boss@example.test" || body.Data[1]["email"] != "gone@example.test" {
		t.Fatalf("unexpected rows: %s", w.Body.String())
	}
	for _, row := range body.Data {
		if len(row) != 4 {
			t.Errorf("row should carry exactly id, email, name, status: %v", row)
		}
	}
	if strings.Contains(w.Body.String(), "HASH-MUST-NOT-LEAK") || strings.Contains(w.Body.String(), "staff@example.test") {
		t.Fatalf("leaked a hash or a staff row: %s", w.Body.String())
	}
}

func TestListAdminUsers_EmptyIsArrayNotNull(t *testing.T) {
	h := newHarness(t, true)
	h.store.users = nil
	w := h.do(http.MethodGet, h.listPath(), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("want data to be [], got %s", w.Body.String())
	}
}

func TestListAdminUsers_InvalidTenantIDIs400(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodGet, "/api/v1/platform/tenants/not-an-id/admin-users", h.tcToken(t, "superadmin"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestListAdminUsers_NoTokenIs401(t *testing.T) {
	h := newHarness(t, true)
	if w := h.do(http.MethodGet, h.listPath(), ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
}

func TestListAdminUsers_HMACPlatformTokenIs401(t *testing.T) {
	h := newHarness(t, true)
	maker, err := token.NewMaker(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := maker.CreateToken("op1", "superadmin", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if w := h.do(http.MethodGet, h.listPath(), tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
	if w := h.do(http.MethodPost, h.resetPath(h.admin), tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("reset: got %d, want 401", w.Code)
	}
	if len(h.reset.calls) != 0 {
		t.Fatal("a reset was queued for a refused caller")
	}
}

func TestListAdminUsers_GroupOffIs404WhenKeyUnset(t *testing.T) {
	h := newHarness(t, false)
	if w := h.do(http.MethodGet, h.listPath(), h.tcToken(t, "superadmin")); w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
	if w := h.do(http.MethodPost, h.resetPath(h.admin), h.tcToken(t, "superadmin")); w.Code != http.StatusNotFound {
		t.Fatalf("reset: got %d, want 404", w.Code)
	}
}

func TestResetPassword_QueuesCodeForAdmin(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodPost, h.resetPath(h.admin), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if len(h.reset.calls) != 1 {
		t.Fatalf("want one reset request, got %d", len(h.reset.calls))
	}
	got := h.reset.calls[0]
	if got.tenant != h.tenant || got.email != h.admin.Email {
		t.Fatalf("reset asked for %v/%q, want %v/%q (the stored email)", got.tenant, got.email, h.tenant, h.admin.Email)
	}
}

func TestResetPassword_StaffUserRefused(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodPost, h.resetPath(h.staff), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
	if len(h.reset.calls) != 0 {
		t.Fatal("a reset was queued for a staff user")
	}
}

func TestResetPassword_SuspendedUserConflict(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodPost, h.resetPath(h.susp), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409", w.Code)
	}
	if len(h.reset.calls) != 0 {
		t.Fatal("a reset was queued for a suspended user")
	}
}

func TestResetPassword_OtherTenantsUserNotFound(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodPost, h.resetPath(h.other), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
	if len(h.reset.calls) != 0 {
		t.Fatal("a reset was queued across tenants")
	}
}

func TestResetPassword_ResponseCarriesNoCode(t *testing.T) {
	h := newHarness(t, true)
	w := h.do(http.MethodPost, h.resetPath(h.admin), h.tcToken(t, "superadmin"))
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data["message"] != "A reset code was emailed." {
		t.Fatalf("data should be exactly the message, got %s", w.Body.String())
	}
	for _, bad := range []string{"password", "HASH-MUST-NOT-LEAK", h.admin.Email} {
		if strings.Contains(w.Body.String(), bad) {
			t.Errorf("response mentions %q: %s", bad, w.Body.String())
		}
	}
}

func TestResetPassword_MailUnavailableIs503(t *testing.T) {
	h := newHarness(t, true)
	h.reset.err = apierr.FeatureUnavailable("email")
	w := h.do(http.MethodPost, h.resetPath(h.admin), h.tcToken(t, "superadmin"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}
