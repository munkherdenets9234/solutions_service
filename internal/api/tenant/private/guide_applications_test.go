package private

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/middleware"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/internal/service"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/eandstravel/digitalservice/pkg/httpx"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type fakeGuideAdmin struct {
	statusActor, noteActor *primitive.ObjectID
	statusCalls, noteCalls int
	listPage, listLimit    int
	dlDisp                 service.Disposition
	dlCalls                int
	dlErr                  error
}

func (f *fakeGuideAdmin) List(_ context.Context, _ primitive.ObjectID, _ repository.GuideListFilter, page, limit int) ([]*models.GuideApplication, int64, error) {
	f.listPage, f.listLimit = page, limit
	return nil, 0, nil
}
func (f *fakeGuideAdmin) Counts(context.Context, primitive.ObjectID) (map[models.GuideStatus]int64, error) {
	return nil, nil
}
func (f *fakeGuideAdmin) Get(context.Context, primitive.ObjectID, string) (*models.GuideApplication, error) {
	return nil, nil
}
func (f *fakeGuideAdmin) SetStatus(_ context.Context, _ primitive.ObjectID, _ string, _ models.GuideStatus, actor *primitive.ObjectID) error {
	f.statusCalls++
	f.statusActor = actor
	return nil
}
func (f *fakeGuideAdmin) AddNote(_ context.Context, _ primitive.ObjectID, _, _ string, actor *primitive.ObjectID) error {
	f.noteCalls++
	f.noteActor = actor
	return nil
}
func (f *fakeGuideAdmin) FileDownload(_ context.Context, _ primitive.ObjectID, _, _ string, d service.Disposition) (string, time.Time, error) {
	f.dlCalls++
	f.dlDisp = d
	if f.dlErr != nil {
		return "", time.Time{}, f.dlErr
	}
	return "https://files.test/signed", time.Date(2027, 1, 1, 0, 5, 0, 0, time.UTC), nil
}

func TestActorComesFromTheTokenNotTheBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userX := primitive.NewObjectID()
	bogus := primitive.NewObjectID()
	f := &fakeGuideAdmin{}
	h := &guideApplicationsController{svc: f}

	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	e.Use(func(c *gin.Context) {
		c.Set(middleware.CtxTenantID, primitive.NewObjectID())
		c.Set(middleware.CtxUserID, userX.Hex())
	})
	g := httpx.Wrap(&e.RouterGroup)
	g.PATCH("/guide-applications/:id/status", h.SetStatus)
	g.POST("/guide-applications/:id/notes", h.AddNote)

	do := func(method, path, body string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: got %d: %s", method, path, w.Code, w.Body.String())
		}
	}
	do(http.MethodPatch, "/guide-applications/abc/status",
		`{"status":"reviewing","user_id":"`+bogus.Hex()+`","actor":"`+bogus.Hex()+`"}`)
	do(http.MethodPost, "/guide-applications/abc/notes",
		`{"text":"hello","user_id":"`+bogus.Hex()+`","actor":"`+bogus.Hex()+`"}`)

	if f.statusCalls != 1 || f.noteCalls != 1 {
		t.Fatalf("calls: status %d note %d", f.statusCalls, f.noteCalls)
	}
	for name, got := range map[string]*primitive.ObjectID{"status": f.statusActor, "note": f.noteActor} {
		if got == nil || *got != userX {
			t.Errorf("%s actor = %v, want the token's user %s", name, got, userX.Hex())
		}
		if got != nil && *got == bogus {
			t.Errorf("%s actor took the body's value", name)
		}
	}
}

func TestListMetaReportsEffectiveLimitAndPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		query               string
		wantPage, wantLimit int
	}{
		{"limit=500", 1, 20},
		{"limit=100&page=3", 3, 100},
		{"limit=0&page=-2", 1, 20},
	}
	for _, c := range cases {
		f := &fakeGuideAdmin{}
		h := &guideApplicationsController{svc: f}
		e := gin.New()
		e.Use(middleware.ErrorHandler(zap.NewNop(), false))
		e.Use(func(c *gin.Context) { c.Set(middleware.CtxTenantID, primitive.NewObjectID()) })
		httpx.Wrap(&e.RouterGroup).GET("/guide-applications", h.List)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/guide-applications?"+c.query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d: %s", c.query, w.Code, w.Body.String())
		}
		var env struct {
			Meta struct {
				Page  int `json:"page"`
				Limit int `json:"limit"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if env.Meta.Page != c.wantPage || env.Meta.Limit != c.wantLimit {
			t.Errorf("%s: meta = (%d,%d), want (%d,%d)", c.query, env.Meta.Page, env.Meta.Limit, c.wantPage, c.wantLimit)
		}
		if f.listPage != c.wantPage || f.listLimit != c.wantLimit {
			t.Errorf("%s: service got (%d,%d), want (%d,%d)", c.query, f.listPage, f.listLimit, c.wantPage, c.wantLimit)
		}
	}
}

func fileLinkEngine(f *fakeGuideAdmin) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &guideApplicationsController{svc: f}
	e := gin.New()
	e.Use(middleware.ErrorHandler(zap.NewNop(), false))
	e.Use(func(c *gin.Context) { c.Set(middleware.CtxTenantID, primitive.NewObjectID()) })
	httpx.Wrap(&e.RouterGroup).GET("/guide-applications/:id/files/:fileId", h.FileLink)
	return e
}

func TestFileLinkRejectsUnknownDisposition(t *testing.T) {
	for _, q := range []string{"disposition=", "disposition=INLINE", "disposition=Attachment", "disposition=inline,attachment", "disposition=download", "disposition=inline&disposition=attachment"} {
		f := &fakeGuideAdmin{}
		w := httptest.NewRecorder()
		fileLinkEngine(f).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/guide-applications/abc/files/f1?"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d: %s", q, w.Code, w.Body.String())
		}
		if f.dlCalls != 0 {
			t.Errorf("%s: service was called", q)
		}
	}
}

func TestFileLinkDefaultsToAttachment(t *testing.T) {
	cases := map[string]service.Disposition{
		"":                        service.DispositionAttachment,
		"?disposition=attachment": service.DispositionAttachment,
		"?disposition=inline":     service.DispositionInline,
	}
	for q, want := range cases {
		f := &fakeGuideAdmin{}
		w := httptest.NewRecorder()
		fileLinkEngine(f).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/guide-applications/abc/files/f1"+q, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%q: got %d: %s", q, w.Code, w.Body.String())
		}
		if f.dlDisp != want {
			t.Errorf("%q: disposition = %q, want %q", q, f.dlDisp, want)
		}
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Data["url"] == nil || env.Data["expires_at"] == nil || len(env.Data) != 2 {
			t.Errorf("%q: response shape changed: %s", q, w.Body.String())
		}
	}
}

func TestFileLinkOtherTenantFileIs404(t *testing.T) {
	f := &fakeGuideAdmin{dlErr: apierr.NotFound("file")}
	w := httptest.NewRecorder()
	fileLinkEngine(f).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/guide-applications/abc/files/other?disposition=inline", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "files.test") {
		t.Fatal("url leaked on 404")
	}
}
