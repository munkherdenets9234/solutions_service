package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/testutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const guideCol = "guide_applications"

// guideStaff creates a named staff user in tenant and returns its bearer token.
func guideStaff(t *testing.T, app *testutil.App, tn testutil.Tenant, name string) string {
	t.Helper()
	email := fmt.Sprintf("guide-staff-%d@tenant.test", testutil.Unique())
	password := fmt.Sprintf("pw-%d-%d", testutil.Unique(), testutil.Unique())
	resp := testutil.Do(t, app, http.MethodPost, "/api/v1/admin/users", testutil.ReqOpts{
		Token: tn.AdminToken, APIKey: tn.APIKey,
		Body: map[string]string{"name": name, "email": email, "password": password, "role": "staff"},
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("create staff: %d %s", resp.Status, resp.Raw)
	}
	login := testutil.Do(t, app, http.MethodPost, "/api/v1/login", testutil.ReqOpts{
		APIKey: tn.APIKey,
		Body:   map[string]string{"email": email, "password": password},
	})
	tok, _ := login.Data()["token"].(string)
	if login.Status != http.StatusOK || tok == "" {
		t.Fatalf("staff login: %d", login.Status)
	}
	return tok
}

// seedGuide inserts an application straight into the collection.
func seedGuide(t *testing.T, db *mongo.Database, tenantID string, name string, files []models.GuideFile) string {
	t.Helper()
	tid, err := primitive.ObjectIDFromHex(tenantID)
	if err != nil {
		t.Fatalf("tenant id: %v", err)
	}
	now := time.Now().UTC()
	a := models.GuideApplication{
		TenantID: tid,
		Season:   "2027",
		Locale:   "en",
		Personal: models.GuidePersonal{
			FullName: name, Phone: "+97699110000", Email: "seed@applicant.test",
			BirthDate: time.Date(1990, 1, 2, 0, 0, 0, 0, time.UTC), Gender: "female",
		},
		Languages: []models.GuideLanguage{{Language: "en", Level: "fluent"}},
		Regions:   []string{"gobi"},
		Files:     files,
		Status:    models.GuideNew,
		Events:    []models.GuideEvent{},
		ConsentAt: now, CreatedAt: now, UpdatedAt: now,
	}
	res, err := db.Collection(guideCol).InsertOne(context.Background(), a)
	if err != nil {
		t.Fatalf("seed application: %v", err)
	}
	return res.InsertedID.(primitive.ObjectID).Hex()
}

func guideCount(t *testing.T, db *mongo.Database) int64 {
	t.Helper()
	n, err := db.Collection(guideCol).CountDocuments(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func guideEvents(t *testing.T, app *testutil.App, tn testutil.Tenant, tok, id string) []map[string]interface{} {
	t.Helper()
	resp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/guide-applications/"+id, testutil.ReqOpts{Token: tok, APIKey: tn.APIKey})
	if resp.Status != http.StatusOK {
		t.Fatalf("get: %d %s", resp.Status, resp.Raw)
	}
	var out []map[string]interface{}
	raw, _ := resp.Data()["events"].([]interface{})
	for _, e := range raw {
		m, _ := e.(map[string]interface{})
		out = append(out, m)
	}
	return out
}

func listIDs(t *testing.T, app *testutil.App, tn testutil.Tenant, tok, query string) []string {
	t.Helper()
	resp := testutil.Do(t, app, http.MethodGet, "/api/v1/admin/guide-applications?"+query, testutil.ReqOpts{Token: tok, APIKey: tn.APIKey})
	if resp.Status != http.StatusOK {
		t.Fatalf("list %q: %d %s", query, resp.Status, resp.Raw)
	}
	var ids []string
	items, _ := resp.Body["data"].([]interface{})
	for _, it := range items {
		m, _ := it.(map[string]interface{})
		id, _ := m["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestGuideApplications(t *testing.T) {
	db := testutil.StartMongo(t)

	// Private storage is switched off: the shared test config carries a fake
	// Cloudinary URL, which would make a submit attempt a real network call.
	cfg := testutil.TestConfig()
	cfg.CloudinaryURL = ""
	cfg.CloudinaryPrivateURL = ""
	app := testutil.NewAppWithConfig(t, db, cfg)

	superadmin := testutil.SuperadminToken(t, app)
	tenantA := testutil.NewTenant(t, app, superadmin, "guidea")
	tenantB := testutil.NewTenant(t, app, superadmin, "guideb")
	staffA := guideStaff(t, app, tenantA, "Alice Staff")
	staffB := guideStaff(t, app, tenantB, "Bob Other")

	const base = "/api/v1/admin/guide-applications"

	t.Run("submit without private storage answers 503 and stores nothing", func(t *testing.T) {
		before := guideCount(t, db)
		data, _ := json.Marshal(map[string]interface{}{
			"personal": map[string]string{"full_name": "Submit Person", "email": "s@applicant.test", "phone": "+97699112233"},
		})
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if err := w.WriteField("data", string(data)); err != nil {
			t.Fatal(err)
		}
		fw, _ := w.CreateFormFile("file_cv", "cv.pdf")
		_, _ = fw.Write([]byte("%PDF-1.1\n%%EOF\n"))
		_ = w.Close()

		req, _ := http.NewRequest(http.MethodPost, app.Server.URL+"/api/v1/guide-applications", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("X-API-Key", tenantA.APIKey)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer res.Body.Close()
		var body map[string]interface{}
		_ = json.NewDecoder(res.Body).Decode(&body)
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d: %v", res.StatusCode, body)
		}
		if code, _ := body["code"].(string); code != "FEATURE_UNAVAILABLE" {
			t.Errorf("want code FEATURE_UNAVAILABLE, got %q (%v)", code, body)
		}
		if after := guideCount(t, db); after != before {
			t.Errorf("a document was created: %d -> %d", before, after)
		}
	})

	idA := seedGuide(t, db, tenantA.ID, "Alice Applicant", []models.GuideFile{
		{ID: "f1", Kind: models.GuideFileCV, PublicID: "tenants/x/guide-applications/fake", Mime: "application/pdf", Size: 10, OriginalName: "cv.pdf"},
	})
	idA2 := seedGuide(t, db, tenantA.ID, "abc Literal", nil)
	_ = seedGuide(t, db, tenantA.ID, "Plain Name", nil)

	t.Run("list shows tenant A's applications", func(t *testing.T) {
		ids := listIDs(t, app, tenantA, staffA, "page=1&limit=50")
		if !containsID(ids, idA) || !containsID(ids, idA2) {
			t.Errorf("seeded applications missing from list: %v", ids)
		}
	})

	t.Run("counts", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, base+"/counts", testutil.ReqOpts{Token: staffA, APIKey: tenantA.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("counts: %d %s", resp.Status, resp.Raw)
		}
		if n, _ := resp.Data()["new"].(float64); n != 3 {
			t.Errorf("want 3 new, got %v (%s)", resp.Data()["new"], resp.Raw)
		}
	})

	t.Run("get by id hides the storage public id", func(t *testing.T) {
		resp := testutil.Do(t, app, http.MethodGet, base+"/"+idA, testutil.ReqOpts{Token: staffA, APIKey: tenantA.APIKey})
		if resp.Status != http.StatusOK {
			t.Fatalf("get: %d %s", resp.Status, resp.Raw)
		}
		if bytes.Contains(resp.Raw, []byte("tenants/x/guide-applications/fake")) || bytes.Contains(resp.Raw, []byte("public_id")) {
			t.Error("response leaks the storage public id")
		}
	})

	t.Run("status change records the staff name, repeat adds nothing", func(t *testing.T) {
		set := func() int {
			return testutil.Do(t, app, http.MethodPatch, base+"/"+idA+"/status", testutil.ReqOpts{
				Token: staffA, APIKey: tenantA.APIKey, Body: map[string]string{"status": "reviewing"},
			}).Status
		}
		if s := set(); s != http.StatusOK {
			t.Fatalf("status: %d", s)
		}
		evs := guideEvents(t, app, tenantA, staffA, idA)
		if len(evs) != 1 {
			t.Fatalf("want 1 event, got %d", len(evs))
		}
		if evs[0]["user_name"] != "Alice Staff" || evs[0]["type"] != "status" || evs[0]["to"] != "reviewing" {
			t.Errorf("unexpected event: %v", evs[0])
		}
		if s := set(); s != http.StatusOK {
			t.Fatalf("repeat status: %d", s)
		}
		if n := len(guideEvents(t, app, tenantA, staffA, idA)); n != 1 {
			t.Errorf("same status added an event: %d events", n)
		}
		bad := testutil.Do(t, app, http.MethodPatch, base+"/"+idA+"/status", testutil.ReqOpts{
			Token: staffA, APIKey: tenantA.APIKey, Body: map[string]string{"status": "bogus"},
		})
		if bad.Status < 400 || bad.Status >= 500 {
			t.Errorf("invalid status: want 4xx, got %d", bad.Status)
		}
	})

	t.Run("notes are append-only", func(t *testing.T) {
		for _, text := range []string{"first note", "second note"} {
			resp := testutil.Do(t, app, http.MethodPost, base+"/"+idA+"/notes", testutil.ReqOpts{
				Token: staffA, APIKey: tenantA.APIKey, Body: map[string]string{"text": text},
			})
			if resp.Status != http.StatusOK {
				t.Fatalf("note: %d %s", resp.Status, resp.Raw)
			}
		}
		evs := guideEvents(t, app, tenantA, staffA, idA)
		if len(evs) != 3 {
			t.Fatalf("want status + 2 notes = 3 events, got %d", len(evs))
		}
		if evs[0]["type"] != "status" || evs[1]["text"] != "first note" || evs[2]["text"] != "second note" {
			t.Errorf("events not appended in order: %v", evs)
		}
	})

	t.Run("tenant B cannot reach tenant A's application", func(t *testing.T) {
		o := testutil.ReqOpts{Token: staffB, APIKey: tenantB.APIKey}
		if r := testutil.Do(t, app, http.MethodGet, base+"/"+idA, o); r.Status != http.StatusNotFound {
			t.Errorf("get: want 404, got %d", r.Status)
		}
		o.Body = map[string]string{"status": "hired"}
		if r := testutil.Do(t, app, http.MethodPatch, base+"/"+idA+"/status", o); r.Status != http.StatusNotFound {
			t.Errorf("status: want 404, got %d", r.Status)
		}
		o.Body = map[string]string{"text": "intruder"}
		if r := testutil.Do(t, app, http.MethodPost, base+"/"+idA+"/notes", o); r.Status != http.StatusNotFound {
			t.Errorf("note: want 404, got %d", r.Status)
		}
		o.Body = nil
		if r := testutil.Do(t, app, http.MethodGet, base+"/"+idA+"/files/f1", o); r.Status != http.StatusNotFound {
			t.Errorf("file link: want 404, got %d", r.Status)
		}
		if ids := listIDs(t, app, tenantB, staffB, "page=1&limit=50"); len(ids) != 0 {
			t.Errorf("tenant B list not empty: %v", ids)
		}
		if n := len(guideEvents(t, app, tenantA, staffA, idA)); n != 3 {
			t.Errorf("tenant A events changed by tenant B: %d", n)
		}
	})

	t.Run("admin routes need a bearer even with a valid key", func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, base},
			{http.MethodGet, base + "/counts"},
			{http.MethodGet, base + "/" + idA},
			{http.MethodPatch, base + "/" + idA + "/status"},
			{http.MethodPost, base + "/" + idA + "/notes"},
			{http.MethodGet, base + "/" + idA + "/files/f1"},
		} {
			resp := testutil.Do(t, app, c.method, c.path, testutil.ReqOpts{APIKey: tenantA.APIKey})
			if resp.Status != http.StatusUnauthorized {
				t.Errorf("%s %s: want 401, got %d", c.method, c.path, resp.Status)
			}
		}
	})

	t.Run("regex metacharacters in q match literally", func(t *testing.T) {
		// "a.c" as a regex would match "abc Literal"; literally it matches nothing.
		if ids := listIDs(t, app, tenantA, staffA, "q="+url.QueryEscape("a.c")); len(ids) != 0 {
			t.Errorf("a.c matched as a regex: %v", ids)
		}
		if ids := listIDs(t, app, tenantA, staffA, "q="+url.QueryEscape("(")); len(ids) != 0 {
			t.Errorf("( matched something: %v", ids)
		}
		// A literal substring still works, case-insensitively.
		if ids := listIDs(t, app, tenantA, staffA, "q="+url.QueryEscape("ABC lit")); !containsID(ids, idA2) {
			t.Errorf("literal search missed the seeded application: %v", ids)
		}
	})

	t.Run("file link for an unknown file id is 404", func(t *testing.T) {
		for _, id := range []string{idA2, idA} {
			resp := testutil.Do(t, app, http.MethodGet, base+"/"+id+"/files/nope", testutil.ReqOpts{Token: staffA, APIKey: tenantA.APIKey})
			if resp.Status != http.StatusNotFound {
				t.Errorf("want 404, got %d: %s", resp.Status, resp.Raw)
			}
		}
	})
}
