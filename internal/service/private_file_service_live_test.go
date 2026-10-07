//go:build live

package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// validPDF builds a real minimal one-page PDF with a correct xref table.
func validPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objs := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 72 72]>>",
	}
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// errClass describes an error without provider text, URLs or secrets: the
// apierr code when there is one, otherwise just the Go type.
func errClass(err error) string {
	var ae *apierr.APIError
	if errors.As(err, &ae) {
		return fmt.Sprintf("apierr status %d code %v", ae.HTTPStatus, ae.Code)
	}
	return fmt.Sprintf("%T", err)
}

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
		t.Fatalf("NewPrivateFileService failed: %s", errClass(err))
	}
	ctx := context.Background()

	stored, err := svc.Upload(ctx, bytes.NewReader(validPDF()), primitive.NewObjectID())
	if err != nil {
		t.Fatalf("upload failed: %s", errClass(err))
	}
	// Always remove the object, even when an assertion below fails.
	t.Cleanup(func() {
		if err := svc.Delete(context.Background(), stored.PublicID, stored.Mime); err != nil {
			t.Errorf("cleanup delete failed: %s", errClass(err))
		}
	})
	if stored.Mime != "application/pdf" {
		t.Fatalf("stored mime = %q, want application/pdf", stored.Mime)
	}

	u, _, err := svc.DownloadURL(stored.PublicID, stored.Mime, 5*time.Minute, service.DispositionAttachment)
	if err != nil {
		t.Fatalf("DownloadURL failed: %s", errClass(err))
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
	expired, _, err := svc.DownloadURL(stored.PublicID, stored.Mime, -1*time.Minute, service.DispositionAttachment)
	if err != nil {
		t.Fatalf("DownloadURL (expired) failed: %s", errClass(err))
	}
	expCode, _ := fetchStatus(t, expired)
	t.Logf("expired link status: %d", expCode)
	if expCode == http.StatusOK {
		t.Log("NOTE: expired link still served; expiry is not enforced by this endpoint")
	}

	// Delete now (the Cleanup delete afterwards is idempotent: not-found counts
	// as success) and confirm the earlier valid signed link no longer serves.
	if err := svc.Delete(ctx, stored.PublicID, stored.Mime); err != nil {
		t.Fatalf("explicit delete failed: %s", errClass(err))
	}
	goneCode, _ := fetchStatus(t, u)
	t.Logf("link status after delete: %d", goneCode)
	if goneCode == http.StatusOK {
		t.Fatal("signed link still serves the file after delete")
	}
}
