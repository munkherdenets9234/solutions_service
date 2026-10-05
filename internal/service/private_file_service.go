package service

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // Cloudinary's signature scheme mandates SHA-1
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// StoredFile describes a file held in private storage.
type StoredFile struct {
	PublicID string
	Mime     string
	Size     int64
}

// PrivateFiles stores applicant documents that must never have a public URL.
type PrivateFiles interface {
	Upload(ctx context.Context, r io.Reader, tenantID primitive.ObjectID) (*StoredFile, error)
	Delete(ctx context.Context, publicID, mime string) error
	DownloadURL(publicID, mime string, ttl time.Duration) (string, time.Time, error)
	Available() bool
}

// allowedPrivateTypes is the closed set of media accepted for applicant files.
var allowedPrivateTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"application/pdf": true,
}

// SniffPrivateType decides the type from the bytes alone, never from a
// client-declared Content-Type.
func SniffPrivateType(head []byte) (string, bool) {
	if len(head) == 0 {
		return "", false
	}
	mime := stripCharset(http.DetectContentType(head))
	if !allowedPrivateTypes[mime] {
		return "", false
	}
	return mime, true
}

func resourceTypeFor(mime string) (string, bool) {
	switch mime {
	case "application/pdf":
		return "raw", true
	case "image/jpeg", "image/png":
		return "image", true
	}
	return "", false
}

type PrivateFileService struct {
	cld      *cloudinary.Cloudinary
	maxBytes int64

	// Injected in tests so no network call is ever made.
	uploadFunc  func(ctx context.Context, r io.Reader, p uploader.UploadParams) (string, error)
	destroyFunc func(ctx context.Context, publicID, resourceType string) error
}

// NewPrivateFileService returns nil, nil when cloudinaryURL is blank, the
// configured-off state, mirroring NewUploadService.
func NewPrivateFileService(cloudinaryURL string, maxBytes int64) (*PrivateFileService, error) {
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
	s := &PrivateFileService{cld: cld, maxBytes: maxBytes}
	s.uploadFunc = func(ctx context.Context, r io.Reader, p uploader.UploadParams) (string, error) {
		res, err := cld.Upload.Upload(ctx, r, p)
		if err != nil {
			return "", err
		}
		return res.PublicID, nil
	}
	s.destroyFunc = func(ctx context.Context, publicID, resourceType string) error {
		_, err := cld.Upload.Destroy(ctx, uploader.DestroyParams{
			PublicID:     publicID,
			Type:         "authenticated",
			ResourceType: resourceType,
		})
		return err
	}
	return s, nil
}

// Available is safe on a nil receiver.
func (s *PrivateFileService) Available() bool { return s != nil }

// Upload stores r as an authenticated (non-public) object under the tenant's
// guide-applications folder. The destination derives only from tenantID and a
// random name; nothing in the request chooses it.
func (s *PrivateFileService) Upload(ctx context.Context, r io.Reader, tenantID primitive.ObjectID) (*StoredFile, error) {
	if !s.Available() {
		return nil, apierr.FeatureUnavailable("document uploads")
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, apierr.BadRequest("could not read file")
	}
	head = head[:n]
	if n == 0 {
		return nil, apierr.BadRequest("file is empty")
	}
	mime, ok := SniffPrivateType(head)
	if !ok {
		return nil, apierr.ValidationFailed("unsupported file type: only jpeg, png and pdf are accepted")
	}
	rt, _ := resourceTypeFor(mime)

	// One byte over the limit is read on purpose so "at the limit" and "over
	// it" are distinguishable.
	body := io.MultiReader(bytes.NewReader(head), io.LimitReader(r, s.maxBytes+1-int64(n)))
	limited := &countingReader{r: body}

	publicID, err := s.uploadFunc(ctx, limited, uploader.UploadParams{
		Type:         "authenticated",
		ResourceType: rt,
		Folder:       "tenants/" + tenantID.Hex() + "/guide-applications",
		PublicID:     uuid.NewString(),
	})
	if err != nil {
		return nil, apierr.Upstream(apierr.DomainUpload, err)
	}
	if limited.n > s.maxBytes {
		_ = s.destroyFunc(ctx, publicID, rt)
		return nil, apierr.ValidationFailed(fmt.Sprintf("file exceeds the %d byte limit", s.maxBytes))
	}
	return &StoredFile{PublicID: publicID, Mime: mime, Size: limited.n}, nil
}

// Delete removes a stored object, used to clean up when a submit fails.
func (s *PrivateFileService) Delete(ctx context.Context, publicID, mime string) error {
	if !s.Available() {
		return apierr.FeatureUnavailable("document uploads")
	}
	rt, ok := resourceTypeFor(mime)
	if !ok {
		return fmt.Errorf("unsupported mime %q", mime)
	}
	return s.destroyFunc(ctx, publicID, rt)
}

// DownloadURL returns a short-lived signed URL for Cloudinary's private
// download endpoint (CDN delivery of authenticated PDFs is refused by the
// account, this endpoint is not). The secret signs but is never in the URL.
func (s *PrivateFileService) DownloadURL(publicID, mime string, ttl time.Duration) (string, time.Time, error) {
	if !s.Available() {
		return "", time.Time{}, apierr.FeatureUnavailable("document uploads")
	}
	rt, ok := resourceTypeFor(mime)
	if !ok {
		return "", time.Time{}, fmt.Errorf("unsupported mime %q", mime)
	}
	cfg := s.cld.Config.Cloud
	now := time.Now()
	exp := now.Add(ttl)
	p := map[string]string{
		"public_id":  publicID,
		"type":       "authenticated",
		"timestamp":  strconv.FormatInt(now.Unix(), 10),
		"expires_at": strconv.FormatInt(exp.Unix(), 10),
		"attachment": "true",
	}
	sig := signDownloadParams(p, cfg.APISecret)

	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	q.Set("api_key", cfg.APIKey)
	q.Set("signature", sig)
	u := "https://api.cloudinary.com/v1_1/" + url.PathEscape(cfg.CloudName) + "/" + rt + "/download?" + q.Encode()
	return u, exp, nil
}

// signDownloadParams is Cloudinary's API signature: SHA-1 hex of the params
// sorted by key and joined as k=v&k=v, with the secret appended.
func signDownloadParams(p map[string]string, secret string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + p[k]
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "&") + secret)) //nolint:gosec
	return hex.EncodeToString(sum[:])
}
