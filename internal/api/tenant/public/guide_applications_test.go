package public

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type fakeGuideSubmitter struct {
	calls   int
	app     *models.GuideApplication
	uploads []service.GuideUpload
}

func (f *fakeGuideSubmitter) Submit(_ context.Context, _ primitive.ObjectID, a *models.GuideApplication, u []service.GuideUpload) (*service.SubmitResult, error) {
	f.calls++
	f.app = a
	f.uploads = u
	return &service.SubmitResult{ID: "abc123", ConfirmationID: "GA-ABC123"}, nil
}

const testGuideMax = 1024

func guideEngine(svc guideSubmitter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	e.Use(func(c *gin.Context) { c.Set(middleware.CtxTenantID, primitive.NewObjectID()) })
	h := &guideApplicationsController{svc: svc, maxBytes: testGuideMax}
	httpx.Wrap(&e.RouterGroup).POST("/guide-applications", h.Submit)
	return e
}

type part struct {
	name, filename, content string
}

func multipartBody(t *testing.T, parts ...part) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.filename == "" {
			if err := w.WriteField(p.name, p.content); err != nil {
				t.Fatal(err)
			}
			continue
		}
		fw, err := w.CreateFormFile(p.name, p.filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(p.content))
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func post(e *gin.Engine, body io.Reader, ct string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/guide-applications", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	return w
}

func TestHoneypotReturnsFakeSuccessAndStoresNothing(t *testing.T) {
	f := &fakeGuideSubmitter{}
	e := guideEngine(f)
	body, ct := multipartBody(t,
		part{name: "website", content: "http://spam.example"},
		part{name: "data", content: `{}`},
		part{name: "file_photo", filename: "p.png", content: "x"},
	)
	w := post(e, body, ct)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", w.Code, w.Body.String())
	}
	if f.calls != 0 {
		t.Fatalf("service called %d times, want 0", f.calls)
	}
	var env struct {
		Data struct {
			ID             string `json:"id"`
			ConfirmationID string `json:"confirmation_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if _, err := primitive.ObjectIDFromHex(env.Data.ID); err != nil {
		t.Errorf("id %q is not an object id", env.Data.ID)
	}
	if !strings.HasPrefix(env.Data.ConfirmationID, "GA-") || len(env.Data.ConfirmationID) != 9 {
		t.Errorf("confirmation id %q has the wrong shape", env.Data.ConfirmationID)
	}
}

func TestMissingDataPartIs400(t *testing.T) {
	f := &fakeGuideSubmitter{}
	e := guideEngine(f)
	body, ct := multipartBody(t, part{name: "file_photo", filename: "p.png", content: "x"})
	w := post(e, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}

	body, ct = multipartBody(t, part{name: "data", content: "{not json SECRETCONTENT"})
	w = post(e, body, ct)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "SECRETCONTENT") {
		t.Error("response echoes request content")
	}
	if f.calls != 0 {
		t.Errorf("service called %d times", f.calls)
	}
}

func TestMultipartMapsFilePartsToKinds(t *testing.T) {
	f := &fakeGuideSubmitter{}
	e := guideEngine(f)
	body, ct := multipartBody(t,
		part{name: "data", content: `{"personal":{"full_name":"Test Guide"}}`},
		part{name: "file_photo", filename: "me.png", content: "photo-bytes"},
		part{name: "file_id_card", filename: "id.pdf", content: "id-bytes"},
		part{name: "file_guide_certificate_1", filename: "c1.pdf", content: "c1"},
		part{name: "file_guide_certificate_3", filename: "c3.pdf", content: "c3"},
	)
	w := post(e, body, ct)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", w.Code, w.Body.String())
	}
	if f.calls != 1 || f.app == nil || f.app.Personal.FullName != "Test Guide" {
		t.Fatalf("service not called with the decoded data part: %+v", f.app)
	}
	got := map[models.GuideFileKind][]string{}
	for _, u := range f.uploads {
		rc, err := u.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[u.Kind] = append(got[u.Kind], u.OriginalName+":"+string(b))
	}
	if len(f.uploads) != 4 ||
		len(got[models.GuideFilePhoto]) != 1 || got[models.GuideFilePhoto][0] != "me.png:photo-bytes" ||
		len(got[models.GuideFileIDCard]) != 1 ||
		len(got[models.GuideFileGuideCertificate]) != 2 {
		t.Errorf("unexpected mapping: %v", got)
	}
}

func TestUnknownFilePartIs400(t *testing.T) {
	for _, name := range []string{"file_passport", "avatar", "file_guide_certificate_4", "file_guide_certificate"} {
		f := &fakeGuideSubmitter{}
		e := guideEngine(f)
		body, ct := multipartBody(t,
			part{name: "data", content: `{}`},
			part{name: name, filename: "SECRETNAME.pdf", content: "x"},
		)
		w := post(e, body, ct)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, w.Code)
		}
		if strings.Contains(w.Body.String(), "SECRETNAME") || strings.Contains(w.Body.String(), name) {
			t.Errorf("%s: response names submitted content: %s", name, w.Body.String())
		}
		if f.calls != 0 {
			t.Errorf("%s: service called", name)
		}
	}
}

func TestOverSizedBodyIsRejected(t *testing.T) {
	f := &fakeGuideSubmitter{}
	e := guideEngine(f)
	// The ceiling is 8*1024 + 1 MiB; go well over it.
	big := strings.Repeat("a", 8*testGuideMax+(1<<20)+4096)
	body, ct := multipartBody(t,
		part{name: "data", content: `{}`},
		part{name: "file_photo", filename: "p.png", content: big},
	)
	w := post(e, body, ct)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "request too large") {
		t.Errorf("body should say the request is too large: %s", w.Body.String())
	}
	if f.calls != 0 {
		t.Error("service called for an oversized body")
	}
}

func TestSubmitNilServiceIs503(t *testing.T) {
	e := guideEngine(nil)
	body, ct := multipartBody(t, part{name: "data", content: `{}`})
	w := post(e, body, ct)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "FEATURE_UNAVAILABLE") {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

func TestHoneypotWhitespaceOnlyIsNotHoneypot(t *testing.T) {
	f := &fakeGuideSubmitter{}
	e := guideEngine(f)
	body, ct := multipartBody(t, part{name: "website", content: "   \t"}, part{name: "data", content: `{}`})
	w := post(e, body, ct)
	if w.Code != http.StatusCreated || f.calls != 1 {
		t.Fatalf("got %d, calls %d: a blank website field must go to the service", w.Code, f.calls)
	}
}

func TestSameKindTwiceReachesService(t *testing.T) {
	f := &fakeGuideSubmitter{}
	e := guideEngine(f)
	body, ct := multipartBody(t,
		part{name: "data", content: `{}`},
		part{name: "file_cv", filename: "a.pdf", content: "a"},
		part{name: "file_cv", filename: "b.pdf", content: "b"},
	)
	w := post(e, body, ct)
	if w.Code != http.StatusCreated || len(f.uploads) != 2 ||
		f.uploads[0].Kind != models.GuideFileCV || f.uploads[1].Kind != models.GuideFileCV {
		t.Fatalf("got %d, uploads %d", w.Code, len(f.uploads))
	}
}

func TestStrictFormShape(t *testing.T) {
	cases := map[string][]part{
		"unknown value field": {{name: "data", content: `{}`}, {name: "extra", content: "x"}},
		"file as plain value": {{name: "data", content: `{}`}, {name: "file_photo", content: "x"}},
		"two data parts":      {{name: "data", content: `{}`}, {name: "data", content: `{}`}},
		"two website parts":   {{name: "data", content: `{}`}, {name: "website", content: ""}, {name: "website", content: ""}},
	}
	for name, parts := range cases {
		f := &fakeGuideSubmitter{}
		body, ct := multipartBody(t, parts...)
		w := post(guideEngine(f), body, ct)
		if w.Code != http.StatusBadRequest || f.calls != 0 {
			t.Errorf("%s: got %d, calls %d, want 400 and none", name, w.Code, f.calls)
		}
	}
}

func TestTooManyFilesAndOversizedDataAreRejected(t *testing.T) {
	f := &fakeGuideSubmitter{}
	parts := []part{{name: "data", content: `{}`}}
	for i := 0; i < 9; i++ {
		parts = append(parts, part{name: "file_cv", filename: "a.pdf", content: "a"})
	}
	body, ct := multipartBody(t, parts...)
	if w := post(guideEngine(f), body, ct); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("9 files: got %d, want 422", w.Code)
	}
	body, ct = multipartBody(t, part{name: "data", content: `{"x":"` + strings.Repeat("a", 256<<10) + `"}`})
	if w := post(guideEngine(f), body, ct); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("big data: got %d, want 422", w.Code)
	}
	if f.calls != 0 {
		t.Error("service called")
	}
}

func TestOversizedFileIsRejectedBeforeSubmit(t *testing.T) {
	f := &fakeGuideSubmitter{}
	body, ct := multipartBody(t,
		part{name: "data", content: `{}`},
		part{name: "file_cv", filename: "ok.pdf", content: "a"},
		part{name: "file_photo", filename: "big.png", content: strings.Repeat("a", testGuideMax+1)},
	)
	w := post(guideEngine(f), body, ct)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "files.photo") {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if f.calls != 0 {
		t.Error("service called with an oversized file")
	}
	// Exactly at the limit is accepted.
	body, ct = multipartBody(t,
		part{name: "data", content: `{}`},
		part{name: "file_photo", filename: "edge.png", content: strings.Repeat("a", testGuideMax)},
	)
	if w := post(guideEngine(f), body, ct); w.Code != http.StatusCreated || f.calls != 1 {
		t.Fatalf("at the limit: got %d, calls %d", w.Code, f.calls)
	}
}

func TestSubmitToleratesWriterWithoutDeadlines(t *testing.T) {
	// httptest.ResponseRecorder does not support deadlines; the handler must
	// still answer normally.
	f := &fakeGuideSubmitter{}
	body, ct := multipartBody(t, part{name: "data", content: `{}`})
	w := post(guideEngine(f), body, ct)
	if w.Code != http.StatusCreated || f.calls != 1 {
		t.Fatalf("got %d, calls %d", w.Code, f.calls)
	}
}

func TestCleanFileName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cv.pdf", "cv.pdf"},
		{"  spaced name.pdf  ", "spaced name.pdf"},
		{"a\x00b\x01c\x7f\u0085d.pdf", "abcd.pdf"},
		{"line\nbreak\r\t.pdf", "linebreak.pdf"},
		{"\x01\x02\n", "document"},
		{"   ", "document"},
		{"", "document"},
		{strings.Repeat("\u00e9", 300), strings.Repeat("\u00e9", 255)},
	}
	for _, c := range cases {
		if got := cleanFileName(c.in); got != c.want {
			t.Errorf("cleanFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOriginalNameIsCleanedBeforeSubmit(t *testing.T) {
	f := &fakeGuideSubmitter{}
	long := strings.Repeat("n", 400) + ".pdf"
	body, ct := multipartBody(t,
		part{name: "data", content: `{}`},
		part{name: "file_cv", filename: long, content: "a"},
	)
	if w := post(guideEngine(f), body, ct); w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if len(f.uploads) != 1 || len([]rune(f.uploads[0].OriginalName)) != 255 {
		t.Fatalf("name length = %d, want 255", len([]rune(f.uploads[0].OriginalName)))
	}
}
