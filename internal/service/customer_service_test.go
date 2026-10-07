package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type fakeCustomerCreator struct {
	created []*models.Customer
	err     error
}

func (f *fakeCustomerCreator) Create(_ context.Context, tenantID primitive.ObjectID, c *models.Customer) error {
	if f.err != nil {
		return f.err
	}
	c.ID = primitive.NewObjectID()
	c.TenantID = tenantID
	f.created = append(f.created, c)
	return nil
}

type fakeAvatarUploader struct {
	calls     int
	unavail   bool
	gotTenant primitive.ObjectID
	gotBytes  int
	url       string
}

func (f *fakeAvatarUploader) Available() bool { return !f.unavail }
func (f *fakeAvatarUploader) Upload(_ context.Context, r io.Reader, tenantID primitive.ObjectID) (*UploadResult, error) {
	f.calls++
	f.gotTenant = tenantID
	b, _ := io.ReadAll(r)
	f.gotBytes = len(b)
	return &UploadResult{URL: f.url, PublicID: "p"}, nil
}

var (
	avatarPNG  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 600)...)
	avatarJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{1}, 600)...)
	avatarGIF  = append([]byte("GIF89a"), bytes.Repeat([]byte{0}, 600)...)
	avatarWEBP = append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0}, 600)...)
	avatarPDF  = append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 600)...)
	avatarEXE  = append([]byte("MZ\x90\x00\x03"), bytes.Repeat([]byte{0}, 600)...)
)

func newManualSvc() (*CustomerService, *fakeCustomerCreator, *fakeAvatarUploader) {
	cr := &fakeCustomerCreator{}
	up := &fakeAvatarUploader{url: "https://img.example/a.png"}
	return &CustomerService{creator: cr, uploader: up}, cr, up
}

func validCustomer() *models.Customer {
	return &models.Customer{Name: "  Ana  ", Email: "ana@example.com", Phone: "123", Nationality: "MN"}
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != want {
		t.Fatalf("want HTTP %d, got %v", want, err)
	}
}

func TestCreateManualStoresAvatarURL(t *testing.T) {
	for name, b := range map[string][]byte{"png": avatarPNG, "jpeg": avatarJPEG} {
		t.Run(name, func(t *testing.T) {
			svc, cr, up := newManualSvc()
			tenant, admin := primitive.NewObjectID(), primitive.NewObjectID()
			got, err := svc.CreateManual(context.Background(), tenant, validCustomer(), bytes.NewReader(b), &admin)
			if err != nil {
				t.Fatal(err)
			}
			if got.AvatarURL != "https://img.example/a.png" || len(cr.created) != 1 || cr.created[0].AvatarURL != got.AvatarURL {
				t.Fatalf("avatar not stored: %+v", got)
			}
			if got.Name != "Ana" || got.TenantID != tenant || got.UserID == nil || *got.UserID != admin {
				t.Fatalf("bad customer: %+v", got)
			}
			if up.gotTenant != tenant {
				t.Fatal("upload not scoped to tenant")
			}
			if up.gotBytes != len(b) {
				t.Fatalf("uploader saw %d bytes, want the whole %d", up.gotBytes, len(b))
			}
		})
	}
}

func TestCreateManualRejectsNonImageAvatar(t *testing.T) {
	for name, b := range map[string][]byte{"pdf": avatarPDF, "exe": avatarEXE, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			svc, cr, up := newManualSvc()
			_, err := svc.CreateManual(context.Background(), primitive.NewObjectID(), validCustomer(), bytes.NewReader(b), nil)
			assertStatus(t, err, http.StatusUnprocessableEntity)
			if len(cr.created) != 0 || up.calls != 0 {
				t.Fatalf("created=%d uploads=%d, want none", len(cr.created), up.calls)
			}
		})
	}
}

func TestCreateManualRejectsGifAndWebp(t *testing.T) {
	for name, b := range map[string][]byte{"gif": avatarGIF, "webp": avatarWEBP} {
		t.Run(name, func(t *testing.T) {
			svc, cr, up := newManualSvc()
			_, err := svc.CreateManual(context.Background(), primitive.NewObjectID(), validCustomer(), bytes.NewReader(b), nil)
			assertStatus(t, err, http.StatusUnprocessableEntity)
			if len(cr.created) != 0 || up.calls != 0 {
				t.Fatal("must not create or upload")
			}
		})
	}
}

func TestCreateManualWorksWithoutAvatar(t *testing.T) {
	svc, cr, up := newManualSvc()
	up.unavail = true // uploads off must not matter without an avatar
	got, err := svc.CreateManual(context.Background(), primitive.NewObjectID(), validCustomer(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarURL != "" || len(cr.created) != 1 || up.calls != 0 {
		t.Fatalf("unexpected: %+v uploads=%d", got, up.calls)
	}
}

func TestCreateManualAvatarWhenUploadsUnavailable(t *testing.T) {
	svc, cr, up := newManualSvc()
	up.unavail = true
	_, err := svc.CreateManual(context.Background(), primitive.NewObjectID(), validCustomer(), bytes.NewReader(avatarPNG), nil)
	assertStatus(t, err, http.StatusServiceUnavailable)
	if len(cr.created) != 0 {
		t.Fatal("must not create")
	}
}

func TestCreateManualAlwaysCreatesNewCustomer(t *testing.T) {
	svc, cr, _ := newManualSvc()
	tenant := primitive.NewObjectID()
	a, err := svc.CreateManual(context.Background(), tenant, validCustomer(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateManual(context.Background(), tenant, validCustomer(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cr.created) != 2 || a.ID == b.ID {
		t.Fatal("each call must insert a fresh customer, never merge")
	}
}

func TestCreateManualDuplicateEmailIsConflict(t *testing.T) {
	svc, _, _ := newManualSvc()
	svc.creator = &fakeCustomerCreator{err: mongo.WriteException{WriteErrors: []mongo.WriteError{{Code: 11000}}}}
	_, err := svc.CreateManual(context.Background(), primitive.NewObjectID(), validCustomer(), nil, nil)
	assertStatus(t, err, http.StatusConflict)
}

func TestCreateManualValidation(t *testing.T) {
	cases := map[string]*models.Customer{
		"no name":      {Name: "   "},
		"long name":    {Name: strings.Repeat("a", 201)},
		"bad email":    {Name: "A", Email: "not-an-email"},
		"long phone":   {Name: "A", Phone: strings.Repeat("1", 65)},
		"long nation":  {Name: "A", Nationality: strings.Repeat("x", 101)},
		"display name": {Name: "A", Email: "Bob <bob@example.com>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			svc, cr, up := newManualSvc()
			_, err := svc.CreateManual(context.Background(), primitive.NewObjectID(), c, bytes.NewReader(avatarPNG), nil)
			assertStatus(t, err, http.StatusUnprocessableEntity)
			if len(cr.created) != 0 || up.calls != 0 {
				t.Fatal("validation must run before upload and insert")
			}
		})
	}
}
