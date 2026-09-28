package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// allowedUploadTypes is the closed set of media this service accepts.
//
// The type is decided by sniffing the bytes, never by the Content-Type the
// client sent. A client-declared type is a claim about a file, not a fact
// about it, and the two differ exactly when it matters.
// SVG is deliberately absent: it is a script-carrying document, and
// http.DetectContentType reports it as text/xml rather than an image type
// anyway, so accepting it would mean trusting the client's declared type for
// the one format where that is least safe.
var allowedUploadTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

type UploadResult struct {
	URL      string `json:"url"`
	PublicID string `json:"public_id"`
}

type UploadService struct {
	cld      *cloudinary.Cloudinary
	maxBytes int64
}

// NewUploadService returns nil, nil when cloudinaryURL is blank.
//
// A nil service is the configured-off state, not an error: uploads are one
// feature among many, and refusing to start the whole API because an image
// host was not configured would be a worse outage than the one it prevents.
// The route stays mounted and answers 503 FEATURE_UNAVAILABLE, so a client
// can tell a missing deployment setting from a bad request — see
// config.Features and /readyz.
func NewUploadService(cloudinaryURL string, maxBytes int64) (*UploadService, error) {
	if cloudinaryURL == "" {
		return nil, nil
	}
	cld, err := cloudinary.NewFromURL(cloudinaryURL)
	if err != nil {
		return nil, fmt.Errorf("cloudinary init: %w", err)
	}
	if maxBytes < 1 {
		maxBytes = 10 << 20
	}
	return &UploadService{cld: cld, maxBytes: maxBytes}, nil
}

// Available reports whether uploads are configured on this deployment.
// Safe on a nil receiver, so callers need no nil check of their own.
func (s *UploadService) Available() bool { return s != nil }

// Upload stores file under the given tenant's own folder and returns its
// public URL.
//
// SECURITY: the destination is derived entirely from the tenant id resolved
// by middleware — never from the request body. The previous version passed a
// client-supplied "folder" form field straight through, which let any caller
// holding a tenant's API key write into any other tenant's folder, or into
// the account root, by typing a different value. A caller choosing its own
// storage path is the same class of mistake as a caller choosing its own
// filename; the fix is the same, which is to stop asking it.
//
// The object name is a random UUID for the same reason: it cannot collide
// with an existing object and cannot be steered.
func (s *UploadService) Upload(ctx context.Context, file io.Reader, tenantID primitive.ObjectID) (*UploadResult, error) {
	if !s.Available() {
		return nil, apierr.FeatureUnavailable("image uploads")
	}

	// Sniff before streaming. http.DetectContentType reads at most 512 bytes,
	// which are then put back in front of the stream so the upload still sees
	// the whole file.
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, apierr.BadRequest("could not read file")
	}
	head = head[:n]
	if n == 0 {
		return nil, apierr.BadRequest("file is empty")
	}
	if _, ok := allowedUploadTypes[stripCharset(http.DetectContentType(head))]; !ok {
		return nil, apierr.ValidationFailed("unsupported file type: only jpeg, png, gif and webp images are accepted")
	}

	// Cap the stream rather than trusting Content-Length, which is a header
	// and therefore also a claim. One byte over the limit is read on purpose
	// so "exactly at the limit" and "over it" are distinguishable.
	body := io.MultiReader(bytes.NewReader(head), io.LimitReader(file, s.maxBytes+1-int64(n)))
	limited := &countingReader{r: body}

	res, err := s.cld.Upload.Upload(ctx, limited, uploader.UploadParams{
		Folder:   "tenants/" + tenantID.Hex(),
		PublicID: uuid.NewString(),
	})
	if err != nil {
		return nil, apierr.Upstream(apierr.DomainUpload, err)
	}
	if limited.n > s.maxBytes {
		// The object is already at the host; remove it rather than leaving a
		// file nobody has a row for.
		_, _ = s.cld.Upload.Destroy(ctx, uploader.DestroyParams{PublicID: res.PublicID})
		return nil, apierr.ValidationFailed(fmt.Sprintf("file exceeds the %d byte limit", s.maxBytes))
	}

	return &UploadResult{URL: res.SecureURL, PublicID: res.PublicID}, nil
}

// stripCharset reduces "text/plain; charset=utf-8" to "text/plain".
func stripCharset(ct string) string {
	for i := 0; i < len(ct); i++ {
		if ct[i] == ';' {
			return ct[:i]
		}
	}
	return ct
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
