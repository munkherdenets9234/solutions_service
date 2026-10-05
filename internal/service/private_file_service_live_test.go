//go:build live

package service_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/service"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// minimalPDF is a tiny structurally valid one-page PDF (a few hundred bytes).
const minimalPDF = "%PDF-1.1\n" +
	"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
	"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 72 72]>>endobj\n" +
	"trailer<</Root 1 0 R/Size 4>>\n%%EOF\n"

// fetchStatus GETs rawURL and returns the status and the first bytes of the
// body. Errors never include the URL: it is a credential.
func fetchStatus(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal("build request failed")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal("request failed") // err text embeds the URL; do not print it
	}
	defer res.Body.Close()
	head, _ := io.ReadAll(io.LimitReader(res.Body, 16))
	return res.StatusCode, string(head)
}

// TestPrivateFileServiceLive talks to the real Cloudinary account named by
// CLOUDINARY_URL. It is skipped unless that variable is set, and only built
// with -tags live. It never prints the variable or any signed URL.
func TestPrivateFileServiceLive(t *testing.T) {
	cldURL := os.Getenv("CLOUDINARY_URL")
	if cldURL == "" {
		t.Skip("CLOUDINARY_URL not set")
	}
	svc, err := service.NewPrivateFileService(cldURL, 10<<20)
	if err != nil {
		t.Fatal("NewPrivateFileService failed")
	}
	ctx := context.Background()

	stored, err := svc.Upload(ctx, bytes.NewReader([]byte(minimalPDF)), primitive.NewObjectID())
	if err != nil {
		t.Fatal("upload failed")
	}
	// Always remove the object, even when an assertion below fails.
	t.Cleanup(func() {
		if err := svc.Delete(context.Background(), stored.PublicID, stored.Mime); err != nil {
			t.Error("cleanup delete failed")
		}
	})
	if stored.Mime != "application/pdf" {
		t.Fatalf("stored mime = %q, want application/pdf", stored.Mime)
	}

	u, _, err := svc.DownloadURL(stored.PublicID, stored.Mime, 5*time.Minute)
	if err != nil {
		t.Fatal("DownloadURL failed")
	}
	code, head := fetchStatus(t, u)
	if code != http.StatusOK {
		t.Fatalf("signed download status = %d, want 200", code)
	}
	if !strings.HasPrefix(head, "%PDF") {
		t.Fatal("downloaded body does not start with %PDF")
	}

	// Expired link. Whether Cloudinary enforces expires_at on this endpoint is
	// not proven, so a 200 here must not fail the run: it is recorded so the
	// spec can say the 5-minute bound rests on the signature timestamp only.
	expired, _, err := svc.DownloadURL(stored.PublicID, stored.Mime, -1*time.Minute)
	if err != nil {
		t.Fatal("DownloadURL (expired) failed")
	}
	expCode, _ := fetchStatus(t, expired)
	t.Logf("expired link status: %d", expCode)
	if expCode == http.StatusOK {
		t.Log("NOTE: expired link still served; expiry is not enforced by this endpoint")
	}
}
