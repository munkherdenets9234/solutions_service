package service

import (
	"errors"
	"bytes"
	"context"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const fakeCloudinaryURL = "cloudinary://testkey:testsecret@testcloud"

var (
	pdfBytes  = append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 64)...)
	pngBytes  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")
	jpegBytes = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
)

func TestSniffAcceptsJpegPngPdf(t *testing.T) {
	cases := map[string][]byte{
		"image/jpeg":      jpegBytes,
		"image/png":       pngBytes,
		"application/pdf": pdfBytes,
	}
	for want, b := range cases {
		got, ok := SniffPrivateType(b)
		if !ok || got != want {
			t.Errorf("want %s, got %q ok=%v", want, got, ok)
		}
	}
}

func TestSniffRejectsExeNamedPdf(t *testing.T) {
	exe := append([]byte("MZ\x90\x00\x03\x00\x00\x00"), bytes.Repeat([]byte{0}, 64)...)
	if mime, ok := SniffPrivateType(exe); ok {
		t.Fatalf("exe accepted as %s", mime)
	}
	if _, ok := SniffPrivateType([]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>")); ok {
		t.Fatal("svg accepted")
	}
	if _, ok := SniffPrivateType(nil); ok {
		t.Fatal("empty accepted")
	}
}

func TestSniffIgnoresClientDeclaredType(t *testing.T) {
	// The function takes bytes only, so a declared type cannot influence it.
	if mime, ok := SniffPrivateType(pdfBytes); !ok || mime != "application/pdf" {
		t.Fatalf("pdf bytes: %q %v", mime, ok)
	}
}

func TestSignDownloadParamsMatchesKnownVector(t *testing.T) {
	p := map[string]string{
		"public_id":  "tenants/abc/guide-applications/f1",
		"type":       "authenticated",
		"timestamp":  "1700000000",
		"expires_at": "1700000300",
		"attachment": "true",
	}
	// printf '%s' 'attachment=true&expires_at=1700000300&public_id=tenants/abc/guide-applications/f1&timestamp=1700000000&type=authenticatedtestsecret' | sha1sum
	const want = "cb343cea60cadb353304d962e7e3235b11805741"
	if got := signDownloadParams(p, "testsecret"); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func newTestSvc(t *testing.T, max int64) *PrivateFileService {
	t.Helper()
	s, err := NewPrivateFileService(fakeCloudinaryURL, max)
	if err != nil || s == nil {
		t.Fatalf("new: %v %v", s, err)
	}
	return s
}

func TestDownloadURLShape(t *testing.T) {
	s := newTestSvc(t, 1024)
	for mime, rt := range map[string]string{"application/pdf": "raw", "image/jpeg": "image", "image/png": "image"} {
		raw, exp, err := s.DownloadURL("tenants/abc/guide-applications/f1", mime, 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Host != "api.cloudinary.com" || u.Path != "/v1_1/testcloud/"+rt+"/download" {
			t.Errorf("%s: bad url %s", mime, raw)
		}
		q := u.Query()
		if q.Get("type") != "authenticated" || q.Get("api_key") != "testkey" ||
			q.Get("expires_at") == "" || q.Get("signature") == "" || q.Get("public_id") == "" {
			t.Errorf("%s: missing params in %s", mime, raw)
		}
		if strings.Contains(raw, "testsecret") {
			t.Errorf("secret leaked in url")
		}
		if !exp.After(time.Now()) {
			t.Errorf("expiry not in future")
		}
	}
	if _, _, err := s.DownloadURL("x", "text/plain", time.Minute); err == nil {
		t.Error("unsupported mime should error")
	}
}

func TestAvailableNilReceiver(t *testing.T) {
	var s *PrivateFileService
	if s.Available() {
		t.Fatal("nil should be unavailable")
	}
	s2, err := NewPrivateFileService("", 10)
	if s2 != nil || err != nil {
		t.Fatalf("blank url: %v %v", s2, err)
	}
	if !newTestSvc(t, 10).Available() {
		t.Fatal("configured should be available")
	}
}

func TestUploadRejectsOverLimitStream(t *testing.T) {
	s := newTestSvc(t, 100)
	var uploads, destroys int
	var gotParams uploader.UploadParams
	var destroyed string
	s.uploadFunc = func(_ context.Context, r io.Reader, p uploader.UploadParams) (string, error) {
		uploads++
		gotParams = p
		_, _ = io.Copy(io.Discard, r)
		return "tenants/x/guide-applications/id1", nil
	}
	s.destroyFunc = func(_ context.Context, publicID, resourceType string) error {
		destroys++
		destroyed = publicID
		return nil
	}
	tid := primitive.NewObjectID()

	big := append(append([]byte{}, pdfBytes...), bytes.Repeat([]byte("y"), 200)...)
	if _, err := s.Upload(context.Background(), bytes.NewReader(big), tid); err == nil {
		t.Fatal("over-limit accepted")
	}
	if uploads != 1 || destroys != 1 || destroyed != "tenants/x/guide-applications/id1" {
		t.Fatalf("uploads=%d destroys=%d destroyed=%q", uploads, destroys, destroyed)
	}

	ok := append(append([]byte{}, pdfBytes...), bytes.Repeat([]byte("y"), 100-len(pdfBytes))...)
	f, err := s.Upload(context.Background(), bytes.NewReader(ok), tid)
	if err != nil {
		t.Fatalf("exactly at limit rejected: %v", err)
	}
	if f.Mime != "application/pdf" || f.Size != 100 || f.PublicID == "" {
		t.Fatalf("bad result %+v", f)
	}
	if gotParams.Type != "authenticated" || gotParams.ResourceType != "raw" ||
		gotParams.Folder != "tenants/"+tid.Hex()+"/guide-applications" || gotParams.PublicID == "" {
		t.Fatalf("bad params %+v", gotParams)
	}

	uploads = 0
	exe := append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{0}, 50)...)
	if _, err := s.Upload(context.Background(), bytes.NewReader(exe), tid); err == nil || uploads != 0 {
		t.Fatal("exe should be rejected before upload")
	}
	if _, err := s.Upload(context.Background(), bytes.NewReader(nil), tid); err == nil {
		t.Fatal("empty should be rejected")
	}
}

func TestUploadResultError(t *testing.T) {
	const safe = "document could not be read; export it again as a valid PDF or image"
	cases := []struct {
		name, msg, id string
		wantNil       bool
		wantStatus    int
	}{
		{"ok", "", "abc", true, 0},
		{"empty both", "", "", false, 502},
		{"invalid pdf", "Invalid PDF file", "", false, 422},
		{"invalid image lower", "invalid image file", "x", false, 422},
		{"other", "Unknown api_key secret-ish", "", false, 502},
	}
	for _, c := range cases {
		err := uploadResultError(c.msg, c.id)
		if c.wantNil {
			if err != nil {
				t.Errorf("%s: want nil, got %v", c.name, err)
			}
			continue
		}
		var ae *apierr.APIError
		if !errors.As(err, &ae) {
			t.Fatalf("%s: not APIError: %v", c.name, err)
		}
		if ae.HTTPStatus != c.wantStatus {
			t.Errorf("%s: status %d want %d", c.name, ae.HTTPStatus, c.wantStatus)
		}
		if c.msg != "" && (strings.Contains(ae.Message, c.msg) || (ae.Err != nil && strings.Contains(ae.Err.Error(), c.msg))) {
			t.Errorf("%s: provider message leaked", c.name)
		}
		if c.wantStatus == 422 && ae.Message != safe {
			t.Errorf("%s: message %q", c.name, ae.Message)
		}
	}
}

func TestUploadEmptyPublicIDIsAnError(t *testing.T) {
	s := newTestSvc(t, 100)
	s.uploadFunc = func(_ context.Context, r io.Reader, _ uploader.UploadParams) (string, error) {
		_, _ = io.Copy(io.Discard, r)
		return "", nil
	}
	s.destroyFunc = func(context.Context, string, string) error {
		t.Error("destroy must not be called")
		return nil
	}
	if f, err := s.Upload(context.Background(), bytes.NewReader(pdfBytes), primitive.NewObjectID()); err == nil || f != nil {
		t.Fatalf("empty public id accepted: %+v %v", f, err)
	}
}

func TestDeleteEmptyPublicIDIsNoop(t *testing.T) {
	s := newTestSvc(t, 100)
	s.destroyFunc = func(context.Context, string, string) error {
		t.Error("destroy must not be called")
		return nil
	}
	if err := s.Delete(context.Background(), "", "application/pdf"); err != nil {
		t.Fatal(err)
	}
}
