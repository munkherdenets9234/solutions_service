package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ── fake ─────────────────────────────────────────────────────────────────

// fakeSiteStore keys pages by tenant, as the real collection does: the unique
// index is (tenant_id, page).
type fakeSiteStore struct {
	pages map[string]*models.SitePage
}

func newFakeSiteStore() *fakeSiteStore { return &fakeSiteStore{pages: map[string]*models.SitePage{}} }

func siteKey(t primitive.ObjectID, page string) string { return t.Hex() + "/" + page }

func (f *fakeSiteStore) List(_ context.Context, t primitive.ObjectID) ([]*models.SitePage, error) {
	var out []*models.SitePage
	for _, p := range f.pages {
		if p.TenantID == t {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeSiteStore) Find(_ context.Context, t primitive.ObjectID, page string) (*models.SitePage, error) {
	if p, ok := f.pages[siteKey(t, page)]; ok {
		return p, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeSiteStore) Replace(_ context.Context, t primitive.ObjectID, page string, entries []models.ContentEntry, userID *primitive.ObjectID) error {
	f.pages[siteKey(t, page)] = &models.SitePage{TenantID: t, Page: page, Entries: entries, UserID: userID}
	return nil
}

func entry(path string, kv ...string) models.ContentEntry {
	v := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		v[kv[i]] = kv[i+1]
	}
	return models.ContentEntry{Path: path, Values: v}
}

func wantBadRequest(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", what)
	}
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != 400 {
		t.Fatalf("%s: expected a 400 apierr, got %v", what, err)
	}
}

func newSiteSvc() (*SitePageService, *fakeSiteStore, primitive.ObjectID) {
	st := newFakeSiteStore()
	return NewSitePageService(st), st, primitive.NewObjectID()
}

// ── Save: validation ─────────────────────────────────────────────────────

func TestSaveRejectsABadPageName(t *testing.T) {
	svc, _, tn := newSiteSvc()
	for _, p := range []string{"", strings.Repeat("a", 65), "a b", "a.b", "a/b", "../x", "Сайн"} {
		_, err := svc.Save(context.Background(), tn, p, nil, nil)
		wantBadRequest(t, err, "page "+p)
	}
	for _, p := range []string{"hero", "tour-Detail_2", strings.Repeat("a", 64)} {
		if _, err := svc.Save(context.Background(), tn, p, nil, nil); err != nil {
			t.Fatalf("page %q should be accepted: %v", p, err)
		}
	}
}

func TestSaveRejectsABadPath(t *testing.T) {
	svc, _, tn := newSiteSvc()
	for _, p := range []string{"", strings.Repeat("a", 201), "a..b", "a b", ".a", "a.", "a$b"} {
		_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry(p, "en", "x")}, nil)
		wantBadRequest(t, err, "path "+p)
	}
}

func TestSaveRejectsDuplicatePaths(t *testing.T) {
	svc, _, tn := newSiteSvc()
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("title", "en", "a"), entry("title", "mn", "b")}, nil)
	wantBadRequest(t, err, "duplicate path")
}

func TestSaveRejectsMoreThan1000Entries(t *testing.T) {
	svc, _, tn := newSiteSvc()
	mk := func(n int) []models.ContentEntry {
		es := make([]models.ContentEntry, 0, n)
		for i := 0; i < n; i++ {
			es = append(es, entry("k"+strconv.Itoa(i), "en", "v"))
		}
		return es
	}
	if _, err := svc.Save(context.Background(), tn, "hero", mk(1000), nil); err != nil {
		t.Fatalf("1000 entries should pass: %v", err)
	}
	_, err := svc.Save(context.Background(), tn, "hero", mk(1001), nil)
	wantBadRequest(t, err, "1001 entries")
}

func TestSaveRejectsAnUnknownLanguage(t *testing.T) {
	svc, _, tn := newSiteSvc()
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("title", "fr", "Bonjour")}, nil)
	wantBadRequest(t, err, "fr")
}

func TestSaveRejectsABadValueType(t *testing.T) {
	svc, _, tn := newSiteSvc()
	over := make([]any, 101)
	for i := range over {
		over[i] = "x"
	}
	bad := map[string]any{
		"number":         float64(3),
		"bool":           true,
		"nested object":  map[string]any{"a": "b"},
		"array of nums":  []any{float64(1), float64(2)},
		"obj non-string": []any{map[string]any{"a": float64(1)}},
		"obj nested":     []any{map[string]any{"a": map[string]any{"b": "c"}}},
		"5001 chars":     strings.Repeat("a", 5001),
		"101 items":      over,
		"mixed array":    []any{"a", map[string]any{"a": "b"}},
		"null":           nil,
	}
	for name, v := range bad {
		_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: map[string]any{"en": v}}}, nil)
		wantBadRequest(t, err, name)
	}
}

func TestSaveAcceptsStringsArraysAndFlatObjectArrays(t *testing.T) {
	svc, st, tn := newSiteSvc()
	max := make([]any, 100)
	for i := range max {
		max[i] = "x"
	}
	es := []models.ContentEntry{
		{Path: "a", Values: map[string]any{"en": strings.Repeat("a", 5000)}},
		{Path: "b", Values: map[string]any{"en": []any{"one", "two"}}},
		{Path: "c", Values: map[string]any{"en": []any{map[string]any{"q": "Q?", "a": "A."}}}},
		{Path: "d", Values: map[string]any{"en": max}},
	}
	n, err := svc.Save(context.Background(), tn, "faq", es, nil)
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := st.pages[siteKey(tn, "faq")]; got == nil || len(got.Entries) != 4 {
		t.Fatalf("not stored: %+v", got)
	}
}

// ── Save: semantics ──────────────────────────────────────────────────────

// Review Focus 1: a language left blank means "use the shipped wording", so it
// must not be stored as an empty override that would blank the site.
func TestSaveDropsEntriesWhoseLanguagesAreAllBlank(t *testing.T) {
	svc, st, tn := newSiteSvc()
	es := []models.ContentEntry{
		entry("all.blank", "en", "", "mn", "   \n\t", "ko", ""),
		{Path: "empty.array", Values: map[string]any{"en": []any{}}},
		entry("one.filled", "en", "Hello", "mn", "  ", "ko", ""),
		entry("none"),
	}
	n, err := svc.Save(context.Background(), tn, "hero", es, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("stored count = %d, want 1", n)
	}
	got := st.pages[siteKey(tn, "hero")].Entries
	if len(got) != 1 || got[0].Path != "one.filled" {
		t.Fatalf("entries = %+v", got)
	}
	if len(got[0].Values) != 1 || got[0].Values["en"] != "Hello" {
		t.Fatalf("blank languages must be dropped from Values: %+v", got[0].Values)
	}
}

// Review Focus 2: removing an override is saving without it.
func TestSaveReplacesTheWholePage(t *testing.T) {
	svc, st, tn := newSiteSvc()
	n, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("a", "en", "1"), entry("b", "en", "2")}, nil)
	if err != nil || n != 2 {
		t.Fatalf("first save n=%d err=%v", n, err)
	}
	n, err = svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("b", "en", "3")}, nil)
	if err != nil || n != 1 {
		t.Fatalf("second save n=%d err=%v", n, err)
	}
	got := st.pages[siteKey(tn, "hero")].Entries
	if len(got) != 1 || got[0].Path != "b" || got[0].Values["en"] != "3" {
		t.Fatalf("page not replaced: %+v", got)
	}
	// Saving nothing clears the page.
	if n, err = svc.Save(context.Background(), tn, "hero", nil, nil); err != nil || n != 0 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
	if len(st.pages[siteKey(tn, "hero")].Entries) != 0 {
		t.Fatal("page should be empty")
	}
}

// Review Focus 3: text is data. Nothing is escaped, trimmed or normalised on
// the way in; the site renders it as text.
func TestSaveStoresRiskyTextVerbatim(t *testing.T) {
	svc, st, tn := newSiteSvc()
	vals := map[string]any{
		"en": `<script>alert("x")</script> 'q' "dq" & ` + "\U0001F600",
		"mn": "Сайн байна уу",
		"ko": "  padded  ",
	}
	if _, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: vals}}, nil); err != nil {
		t.Fatal(err)
	}
	got := st.pages[siteKey(tn, "hero")].Entries[0].Values
	for k, v := range vals {
		if got[k] != v {
			t.Fatalf("%s changed: %q -> %q", k, v, got[k])
		}
	}
}

func TestSaveRecordsTheActingUser(t *testing.T) {
	svc, st, tn := newSiteSvc()
	u := primitive.NewObjectID()
	if _, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("a", "en", "1")}, &u); err != nil {
		t.Fatal(err)
	}
	if got := st.pages[siteKey(tn, "hero")].UserID; got == nil || *got != u {
		t.Fatalf("user = %v", got)
	}
}

// ── Get / List ───────────────────────────────────────────────────────────

func TestGetOfAnUnknownPageIsEmptyNotAnError(t *testing.T) {
	svc, _, tn := newSiteSvc()
	p, err := svc.Get(context.Background(), tn, "hero")
	if err != nil {
		t.Fatal(err)
	}
	if p.Page != "hero" || p.Entries == nil || len(p.Entries) != 0 {
		t.Fatalf("page = %+v", p)
	}
	_, err = svc.Get(context.Background(), tn, "bad page")
	wantBadRequest(t, err, "bad page name")
}

func TestListCountsEntriesPerPage(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{entry("a", "en", "1"), entry("b", "en", "2")}, nil)
	_, _ = svc.Save(ctx, tn, "footer", []models.ContentEntry{entry("a", "en", "1")}, nil)
	got, err := svc.List(ctx, tn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Page != "footer" || got[0].Entries != 1 || got[1].Page != "hero" || got[1].Entries != 2 {
		t.Fatalf("summaries = %+v", got)
	}
}

func TestListReturnsAnEmptySliceNotNil(t *testing.T) {
	svc, _, tn := newSiteSvc()
	got, err := svc.List(context.Background(), tn)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}

// ── Public ───────────────────────────────────────────────────────────────

func TestPublicReturnsOnlyTheRequestedLanguage(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		entry("title", "en", "Hello", "mn", "Сайн"),
		entry("sub", "en", "Only English"),
	}, nil)
	_, _ = svc.Save(ctx, tn, "footer", []models.ContentEntry{entry("c", "en", "(c)")}, nil)

	mn, err := svc.Public(ctx, tn, "mn")
	if err != nil {
		t.Fatal(err)
	}
	if len(mn) != 1 || len(mn["hero"]) != 1 || mn["hero"]["title"] != "Сайн" {
		t.Fatalf("mn = %+v", mn)
	}
	ko, err := svc.Public(ctx, tn, "ko")
	if err != nil || ko == nil || len(ko) != 0 {
		t.Fatalf("ko should be {} not nil: %#v err=%v", ko, err)
	}
}

func TestPublicRejectsALanguageOutsideEnMnKo(t *testing.T) {
	svc, _, tn := newSiteSvc()
	for _, l := range []string{"EN", "fr", ""} {
		_, err := svc.Public(context.Background(), tn, l)
		wantBadRequest(t, err, "lang "+l)
	}
}

// Review Focus 5.
func TestTenantsDoNotSeeEachOthersPages(t *testing.T) {
	svc, _, a := newSiteSvc()
	b := primitive.NewObjectID()
	ctx := context.Background()
	if _, err := svc.Save(ctx, a, "hero", []models.ContentEntry{entry("t", "en", "A only")}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Public(ctx, b, "en"); len(got) != 0 {
		t.Fatalf("public leaked: %+v", got)
	}
	if got, _ := svc.List(ctx, b); len(got) != 0 {
		t.Fatalf("list leaked: %+v", got)
	}
	if p, _ := svc.Get(ctx, b, "hero"); len(p.Entries) != 0 {
		t.Fatalf("get leaked: %+v", p)
	}
	if _, err := svc.Save(ctx, b, "hero", []models.ContentEntry{entry("t", "en", "B")}, nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.Get(ctx, a, "hero"); p.Entries[0].Values["en"] != "A only" {
		t.Fatalf("B's save overwrote A: %+v", p.Entries)
	}
}

// ── Save: hardening ──────────────────────────────────────────────────────

func TestSaveRejects1001EntriesEvenWhenAllAreBlank(t *testing.T) {
	svc, _, tn := newSiteSvc()
	es := make([]models.ContentEntry, 0, 1001)
	for i := 0; i < 1001; i++ {
		es = append(es, entry("k"+strconv.Itoa(i), "en", ""))
	}
	_, err := svc.Save(context.Background(), tn, "hero", es, nil)
	wantBadRequest(t, err, "1001 blank entries")
}

func TestSaveRejectsAMixedArrayObjectFirst(t *testing.T) {
	svc, _, tn := newSiteSvc()
	v := []any{map[string]any{"a": "b"}, "text"}
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: map[string]any{"en": v}}}, nil)
	wantBadRequest(t, err, "object then string")
}

func TestSaveRejectsBadObjectKeys(t *testing.T) {
	svc, _, tn := newSiteSvc()
	many := map[string]any{}
	for i := 0; i < 21; i++ {
		many["k"+strconv.Itoa(i)] = "v"
	}
	bad := map[string]map[string]any{
		"dot":       {"a.b": "v"},
		"dollar":    {"$where": "v"},
		"empty":     {"": "v"},
		"space":     {"a b": "v"},
		"65 chars":  {strings.Repeat("k", 65): "v"},
		"21 keys":   many,
		"non-ascii": {"ключ": "v"},
	}
	for name, obj := range bad {
		v := []any{obj}
		_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: map[string]any{"en": v}}}, nil)
		wantBadRequest(t, err, name)
	}
	ok := map[string]any{"q-1_A": "x", strings.Repeat("k", 64): "y"}
	if _, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: map[string]any{"en": []any{ok}}}}, nil); err != nil {
		t.Fatalf("valid keys rejected: %v", err)
	}
}

func TestSaveTreatsArraysWithNoTextAsBlank(t *testing.T) {
	svc, st, tn := newSiteSvc()
	blanks := map[string]any{
		"empty":         []any{},
		"empty string":  []any{""},
		"spaces":        []any{"  ", "\t"},
		"blank objects": []any{map[string]any{"q": "", "a": ""}},
		"empty object":  []any{map[string]any{}},
	}
	for name, v := range blanks {
		n, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: map[string]any{"en": v}}}, nil)
		if err != nil || n != 0 {
			t.Fatalf("%s: want dropped (0, nil), got (%d, %v)", name, n, err)
		}
		if got := st.pages[siteKey(tn, "hero")]; len(got.Entries) != 0 {
			t.Fatalf("%s: stored %v", name, got.Entries)
		}
	}
	// one non-blank field keeps the array
	n, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{{Path: "t", Values: map[string]any{"en": []any{map[string]any{"q": "", "a": "yes"}}}}}, nil)
	if err != nil || n != 1 {
		t.Fatalf("want kept, got (%d, %v)", n, err)
	}
}

func TestUnknownLanguageErrorDoesNotEchoTheKey(t *testing.T) {
	svc, _, tn := newSiteSvc()
	secret := "zz<script>secret"
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("title", secret, "x")}, nil)
	wantBadRequest(t, err, "unknown lang")
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "zz") {
		t.Fatalf("error echoes the submitted key: %v", err)
	}
}

func TestValidationMessagesDoNotEchoSubmittedPaths(t *testing.T) {
	const secret = "Zq9distinctivepath"
	svc, _, tn := newSiteSvc()
	cases := map[string][]models.ContentEntry{
		"bad char":  {entry(secret+"$x", "en", "v")},
		"empty seg": {entry(secret+"..x", "en", "v")},
		"too long":  {entry(secret+strings.Repeat("a", 201), "en", "v")},
		"duplicate": {entry(secret, "en", "a"), entry(secret, "mn", "b")},
		"language":  {entry(secret, "fr", "v")},
		"value":     {entry(secret, "en", strings.Repeat("a", 5001))},
	}
	for name, es := range cases {
		_, err := svc.Save(context.Background(), tn, "hero", es, nil)
		wantBadRequest(t, err, name)
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: message echoes the submitted path: %q", name, err.Error())
		}
		if !strings.Contains(err.Error(), "entry ") {
			t.Errorf("%s: message should name the entry: %q", name, err.Error())
		}
	}
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{entry("ok", "en", "v"), entry(secret+strings.Repeat("a", 201), "en", "v")}, nil)
	if err == nil || !strings.Contains(err.Error(), "entry 2: the path is too long (max 200 characters)") {
		t.Errorf("unexpected message: %v", err)
	}
}

// ── base snapshot ────────────────────────────────────────────────────────

func withBase(e models.ContentEntry, kv ...any) models.ContentEntry {
	b := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		b[kv[i].(string)] = kv[i+1]
	}
	e.Base = b
	return e
}

func TestSiteValuesEqualTreatsDifferentTypesAsDifferent(t *testing.T) {
	cases := []struct{ a, b any }{
		{"1", []any{"1"}},
		{"a", map[string]any{"a": "a"}},
		{[]any{"a"}, []any{map[string]any{"a": "x"}}},
		{[]any{"a"}, "a"},
		{nil, "a"},
	}
	for i, c := range cases {
		if siteValuesEqual(c.a, c.b) || siteValuesEqual(c.b, c.a) {
			t.Fatalf("case %d: %#v and %#v must differ", i, c.a, c.b)
		}
	}
	if !siteValuesEqual("a", "a") {
		t.Fatal("equal strings must be equal")
	}
}

func TestSiteValuesEqualComparesArraysInOrder(t *testing.T) {
	if !siteValuesEqual([]any{"a", "b"}, []any{"a", "b"}) {
		t.Fatal("same order must be equal")
	}
	if siteValuesEqual([]any{"a", "b"}, []any{"b", "a"}) {
		t.Fatal("different order must differ")
	}
	if siteValuesEqual([]any{"a"}, []any{"a", "b"}) {
		t.Fatal("different length must differ")
	}
}

func TestSiteValuesEqualIgnoresObjectKeyOrder(t *testing.T) {
	a := []any{map[string]any{"q": "1", "a": "2"}}
	b := []any{map[string]any{"a": "2", "q": "1"}}
	if !siteValuesEqual(a, b) {
		t.Fatal("key order must not matter")
	}
	if siteValuesEqual(a, []any{map[string]any{"q": "1", "a": "3"}}) {
		t.Fatal("different field value must differ")
	}
	if siteValuesEqual(a, []any{map[string]any{"q": "1"}}) {
		t.Fatal("different key set must differ")
	}
}

func TestPublicOmitsAValueEqualToItsBase(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, err := svc.Save(ctx, tn, "faq", []models.ContentEntry{
		withBase(entry("s", "en", "Same"), "en", "Same"),
		{Path: "arr", Values: map[string]any{"en": []any{"x", "y"}}, Base: map[string]any{"en": []any{"x", "y"}}},
		{Path: "objs",
			Values: map[string]any{"en": []any{map[string]any{"q": "Q", "a": "A"}}},
			Base:   map[string]any{"en": []any{map[string]any{"a": "A", "q": "Q"}}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Public(ctx, tn, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("every value equals its base, want {}: %#v", got)
	}
}

func TestPublicKeepsAValueThatDiffersFromItsBase(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Edited"), "en", "Original"),
	}, nil)
	got, _ := svc.Public(ctx, tn, "en")
	if got["hero"]["title"] != "Edited" {
		t.Fatalf("got %#v", got)
	}
}

func TestPublicKeepsAValueWithNoBase(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		entry("title", "en", "Hello"),
		withBase(entry("sub", "en", "Hi", "mn", "Сайн"), "mn", "Сайн"),
	}, nil)
	got, _ := svc.Public(ctx, tn, "en")
	if got["hero"]["title"] != "Hello" || got["hero"]["sub"] != "Hi" {
		t.Fatalf("got %#v", got)
	}
}

func TestPublicDecidesPerLanguage(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Hello", "mn", "Засвар"), "en", "Hello", "mn", "Сайн"),
	}, nil)
	en, _ := svc.Public(ctx, tn, "en")
	if len(en) != 0 {
		t.Fatalf("en equals base, want {}: %#v", en)
	}
	mn, _ := svc.Public(ctx, tn, "mn")
	if mn["hero"]["title"] != "Засвар" {
		t.Fatalf("mn = %#v", mn)
	}
}

func TestSaveStoresBase(t *testing.T) {
	svc, st, tn := newSiteSvc()
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Edited"), "en", "Original", "mn", "  "),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := st.pages[siteKey(tn, "hero")].Entries[0].Base
	if len(b) != 1 || b["en"] != "Original" {
		t.Fatalf("base = %#v (blank base language must be dropped)", b)
	}
}

func TestSaveRejectsAnUnknownBaseLanguage(t *testing.T) {
	svc, _, tn := newSiteSvc()
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "x"), "xx-SECRET", "v"),
	}, nil)
	wantBadRequest(t, err, "unknown base language")
	if !strings.Contains(err.Error(), "entry 1: unknown language") {
		t.Fatalf("message = %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("message echoes input: %v", err)
	}
}

func TestSaveRejectsABadBaseValue(t *testing.T) {
	items := make([]any, 101)
	for i := range items {
		items[i] = "ZZ"
	}
	cases := map[string]any{
		"number":  42.0,
		"nested":  []any{map[string]any{"a": map[string]any{"b": "ZZ"}}},
		"toolong": strings.Repeat("ZZ", 2501),
		"items":   items,
	}
	for name, v := range cases {
		svc, _, tn := newSiteSvc()
		_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{
			withBase(entry("title", "en", "x"), "en", v),
		}, nil)
		wantBadRequest(t, err, name)
		if !strings.Contains(err.Error(), "entry 1 (en) base: ") {
			t.Fatalf("%s: message = %v", name, err)
		}
		if strings.Contains(err.Error(), "ZZ") || strings.Contains(err.Error(), "42") {
			t.Fatalf("%s: message echoes input: %v", name, err)
		}
	}
}

func TestSaveKeepsBaseForALanguageWhoseValueIsBlank(t *testing.T) {
	svc, st, tn := newSiteSvc()
	_, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Edited", "mn", ""), "en", "Original", "mn", "Сайн"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := st.pages[siteKey(tn, "hero")].Entries[0]
	if _, ok := e.Values["mn"]; ok {
		t.Fatalf("blank mn value must be dropped: %#v", e.Values)
	}
	if e.Base["mn"] != "Сайн" || e.Base["en"] != "Original" {
		t.Fatalf("base = %#v", e.Base)
	}
}

func TestSaveDropsBaseWithAnEntryThatIsBlankInEveryLanguage(t *testing.T) {
	svc, st, tn := newSiteSvc()
	n, err := svc.Save(context.Background(), tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", " "), "en", "Original"),
	}, nil)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(st.pages[siteKey(tn, "hero")].Entries) != 0 {
		t.Fatal("entry and its base must be dropped")
	}
}

func TestGetReturnsBaseUnfiltered(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Same"), "en", "Same"),
	}, nil)
	p, err := svc.Get(ctx, tn, "hero")
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("p=%#v err=%v", p, err)
	}
	if p.Entries[0].Values["en"] != "Same" || p.Entries[0].Base["en"] != "Same" {
		t.Fatalf("entry = %#v", p.Entries[0])
	}
}

func TestEditedThenResetToBaseDropsOutOfThePublicRead(t *testing.T) {
	svc, _, tn := newSiteSvc()
	ctx := context.Background()
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Edited"), "en", "Original"),
	}, nil)
	got, _ := svc.Public(ctx, tn, "en")
	if got["hero"]["title"] != "Edited" {
		t.Fatalf("edited should show: %#v", got)
	}
	_, _ = svc.Save(ctx, tn, "hero", []models.ContentEntry{
		withBase(entry("title", "en", "Original"), "en", "Original"),
	}, nil)
	got, _ = svc.Public(ctx, tn, "en")
	if len(got) != 0 {
		t.Fatalf("reset to base should drop out: %#v", got)
	}
}
