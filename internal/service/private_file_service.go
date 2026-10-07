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

// Disposition says whether a signed download is rendered by the browser
// (inline) or saved (attachment).
type Disposition string

const (
	DispositionInline     Disposition = "inline"
	DispositionAttachment Disposition = "attachment"
)

// PrivateFiles stores applicant documents that must never have a public URL.
type PrivateFiles interface {
	Upload(ctx context.Context, r io.Reader, tenantID primitive.ObjectID) (*StoredFile, error)
	Delete(ctx context.Context, publicID, mime string) error
	DownloadURL(publicID, mime string, ttl time.Duration, d Disposition) (string, time.Time, error)
	Available() bool
}

// Client-facing texts for file errors. Constants so no provider text can leak
// into them.
const (
	msgCouldNotReadFile = "could not read file"
	msgFileEmpty        = "file is empty"
	msgUnsupportedType  = "unsupported file type: only jpeg, png and pdf are accepted"
)

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

// resourceTypeFor returns the Cloudinary resource type objects are stored
// under. The SDK's Upload always posts to the auto-detect endpoint and ignores
// UploadParams.ResourceType, so Cloudinary stores PDFs as "image" too (the
// earlier "PDFs are raw" assumption was wrong and made downloads 404).
func resourceTypeFor(mime string) (string, bool) {
	switch mime {
	case "application/pdf", "image/jpeg", "image/png":
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
		// The SDK reports API-level failures in res.Error with err == nil.
		if e := uploadResultError(res.Error.Message, res.PublicID); e != nil {
			return "", e
		}
		if e := uploadResultKindError(res.ResourceType, res.Type); e != nil {
			// Stored somewhere we cannot download from; remove it best-effort.
			_, _ = cld.Upload.Destroy(context.WithoutCancel(ctx), uploader.DestroyParams{
				PublicID: res.PublicID, Type: res.Type, ResourceType: res.ResourceType,
			})
			return "", e
		}
		return res.PublicID, nil
	}
	s.destroyFunc = func(ctx context.Context, publicID, resourceType string) error {
		res, err := cld.Upload.Destroy(ctx, uploader.DestroyParams{
			PublicID:     publicID,
			Type:         "authenticated",
			ResourceType: resourceType,
		})
		if err != nil {
			return err
		}
		if res.Error.Message != "" {
			return errors.New("destroy rejected")
		}
		return nil // "ok" and "not found" both mean the object is gone
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
		return nil, apierr.BadRequest(msgCouldNotReadFile)
	}
	head = head[:n]
	if n == 0 {
		return nil, apierr.BadRequest(msgFileEmpty)
	}
	mime, ok := SniffPrivateType(head)
	if !ok {
		return nil, apierr.ValidationFailed(msgUnsupportedType)
	}
	rt, _ := resourceTypeFor(mime) // used for cleanup

	// One byte over the limit is read on purpose so "at the limit" and "over
	// it" are distinguishable.
	body := io.MultiReader(bytes.NewReader(head), io.LimitReader(r, s.maxBytes+1-int64(n)))
	limited := &countingReader{r: body}

	publicID, err := s.uploadFunc(ctx, limited, uploader.UploadParams{
		Type:     "authenticated",
		Folder:   "tenants/" + tenantID.Hex() + "/guide-applications",
		PublicID: uuid.NewString(),
	})
	if err != nil {
		var ae *apierr.APIError
		if errors.As(err, &ae) {
			return nil, ae
		}
		return nil, apierr.Upstream(apierr.DomainUpload, err)
	}
	if publicID == "" {
		return nil, apierr.Upstream(apierr.DomainUpload, errors.New("upload rejected"))
	}
	if limited.n > s.maxBytes {
		_ = s.destroyFunc(context.WithoutCancel(ctx), publicID, rt)
		return nil, apierr.ValidationFailed(fmt.Sprintf("file exceeds the %d byte limit", s.maxBytes))
	}
	return &StoredFile{PublicID: publicID, Mime: mime, Size: limited.n}, nil
}

// Delete removes a stored object, used to clean up when a submit fails.
func (s *PrivateFileService) Delete(ctx context.Context, publicID, mime string) error {
	if publicID == "" {
		return nil // nothing was stored; never send an empty id upstream
	}
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
//
// Attachment signs attachment=true; inline sends no attachment param at all
// (the signature covers exactly the params sent). Any other value is an error.
func (s *PrivateFileService) DownloadURL(publicID, mime string, ttl time.Duration, d Disposition) (string, time.Time, error) {
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
	p, err := downloadParams(publicID, now, exp, d)
	if err != nil {
		return "", time.Time{}, err
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

// downloadParams builds the exact param set that is signed and sent.
func downloadParams(publicID string, now, exp time.Time, d Disposition) (map[string]string, error) {
	p := map[string]string{
		"public_id":  publicID,
		"type":       "authenticated",
		"timestamp":  strconv.FormatInt(now.Unix(), 10),
		"expires_at": strconv.FormatInt(exp.Unix(), 10),
	}
	switch d {
	case DispositionAttachment:
		p["attachment"] = "true"
	case DispositionInline:
		// no attachment param
	default:
		return nil, fmt.Errorf("unknown disposition %q", string(d))
	}
	return p, nil
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

// uploadResultError converts the SDK's err-less failure shape into an error.
// Provider text is never carried into the returned error.
func uploadResultError(msg, publicID string) error {
	if msg != "" {
		lm := strings.ToLower(msg)
		if strings.Contains(lm, "invalid") && strings.Contains(lm, "file") {
			return apierr.ValidationFailed("document could not be read; export it again as a valid PDF or image")
		}
		return apierr.Upstream(apierr.DomainUpload, errors.New("upload rejected"))
	}
	if publicID == "" {
		return apierr.Upstream(apierr.DomainUpload, errors.New("upload rejected"))
	}
	return nil
}

// uploadResultKindError rejects an upload that did not land as an
// authenticated image-resource object, since downloads are signed for that.
func uploadResultKindError(resourceType, deliveryType string) error {
	if resourceType != "image" || deliveryType != "authenticated" {
		return apierr.Upstream(apierr.DomainUpload, errors.New("upload rejected"))
	}
	return nil
}
