package public

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"

	"github.com/eandstravel/digitalservice/internal/api/apictx"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// guideBodyHeadroom is added to 8 x the per-file ceiling for the JSON
	// part and multipart framing.
	guideBodyHeadroom = 1 << 20
	guideMaxMemory    = 8 << 20
	guideHoneypot     = "website"
)

// guideSubmitter is the one service method the public endpoint needs. A small
// local interface so handler tests can fake it.
type guideSubmitter interface {
	Submit(ctx context.Context, tenantID primitive.ObjectID, a *models.GuideApplication, uploads []service.GuideUpload) (*service.SubmitResult, error)
}

// guideApplicationsController takes guide applications from the public site.
// It is mounted in the lead group: rate limited, outside the subscription gate.
type guideApplicationsController struct {
	svc      guideSubmitter
	maxBytes int64 // per-file upload ceiling; the body ceiling derives from it
}

// guideFileKindForPart maps a multipart file part name to its kind.
func guideFileKindForPart(name string) (models.GuideFileKind, bool) {
	switch name {
	case "file_guide_certificate_1", "file_guide_certificate_2", "file_guide_certificate_3":
		return models.GuideFileGuideCertificate, true
	}
	for _, k := range models.GuideFileKinds {
		if k == models.GuideFileGuideCertificate {
			continue
		}
		if name == "file_"+string(k) {
			return k, true
		}
	}
	return "", false
}

// Submit accepts multipart/form-data: part "data" is the JSON application,
// file parts are named file_<kind>. Nothing from the request is ever logged or
// echoed in an error.
func (h *guideApplicationsController) Submit(c *gin.Context) error {
	if h.svc == nil {
		return apierr.FeatureUnavailable("guide applications")
	}

	ceiling := 8*h.maxBytes + guideBodyHeadroom
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ceiling)
	if err := c.Request.ParseMultipartForm(guideMaxMemory); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return apierr.ValidationFailed("request too large")
		}
		return apierr.BadRequest("invalid multipart body")
	}
	form := c.Request.MultipartForm
	defer func() { _ = form.RemoveAll() }()

	// Honeypot: a real visitor never sees this field. Answer exactly like a
	// success so a bot learns nothing, and store nothing.
	if v := form.Value[guideHoneypot]; len(v) > 0 && strings.TrimSpace(strings.Join(v, "")) != "" {
		id := primitive.NewObjectID().Hex()
		response.Created(c, gin.H{"id": id, "confirmation_id": "GA-" + strings.ToUpper(id[len(id)-6:])})
		return nil
	}

	raw := form.Value["data"]
	if len(raw) == 0 || strings.TrimSpace(raw[0]) == "" {
		return apierr.BadRequest("missing application data")
	}
	var app models.GuideApplication
	if err := json.Unmarshal([]byte(raw[0]), &app); err != nil {
		return apierr.BadRequest("invalid application data")
	}

	names := make([]string, 0, len(form.File))
	for name := range form.File {
		names = append(names, name)
	}
	sort.Strings(names)

	var uploads []service.GuideUpload
	for _, name := range names {
		kind, ok := guideFileKindForPart(name)
		if !ok {
			return apierr.BadRequest("unexpected file part")
		}
		for _, fh := range form.File[name] {
			uploads = append(uploads, service.GuideUpload{
				Kind:         kind,
				OriginalName: fh.Filename,
				Open:         openHeader(fh),
			})
		}
	}

	res, err := h.svc.Submit(c.Request.Context(), apictx.TenantID(c), &app, uploads)
	if err != nil {
		return err
	}
	response.Created(c, gin.H{"id": res.ID, "confirmation_id": res.ConfirmationID})
	return nil
}

func openHeader(fh *multipart.FileHeader) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return fh.Open() }
}
