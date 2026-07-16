package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolve(t *testing.T) {
	m := map[string]string{"en": "Hello", "mn": "Сайн байна уу"}

	if got := Resolve(m, "mn"); got != "Сайн байна уу" {
		t.Errorf("Resolve(mn) = %q, want mn value", got)
	}
	if got := Resolve(m, "en"); got != "Hello" {
		t.Errorf("Resolve(en) = %q, want en value", got)
	}
	// Unsupported/missing locale falls back to DefaultLocale.
	if got := Resolve(m, "fr"); got != "Hello" {
		t.Errorf("Resolve(fr) = %q, want fallback to en", got)
	}
	// Missing default too — falls back to any present value.
	onlyMN := map[string]string{"mn": "Сайн байна уу"}
	if got := Resolve(onlyMN, "en"); got != "Сайн байна уу" {
		t.Errorf("Resolve with only mn present = %q, want mn value as last-resort fallback", got)
	}
	if got := Resolve(nil, "en"); got != "" {
		t.Errorf("Resolve(nil) = %q, want empty string", got)
	}
	// A locale present but empty should fall through to the default, not
	// return the empty string.
	partiallyTranslated := map[string]string{"en": "Hello", "mn": ""}
	if got := Resolve(partiallyTranslated, "mn"); got != "Hello" {
		t.Errorf("Resolve with empty mn value = %q, want fallback to en", got)
	}
}

func TestResolveList(t *testing.T) {
	m := map[string][]string{"en": {"a", "b"}, "mn": {"а", "б"}}

	if got := ResolveList(m, "mn"); len(got) != 2 || got[0] != "а" {
		t.Errorf("ResolveList(mn) = %v, want mn list", got)
	}
	if got := ResolveList(m, "fr"); len(got) != 2 || got[0] != "a" {
		t.Errorf("ResolveList(fr) = %v, want fallback to en", got)
	}
	if got := ResolveList(nil, "en"); got != nil {
		t.Errorf("ResolveList(nil) = %v, want nil", got)
	}
}

func TestResolveFromRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newCtx := func(url, acceptLang string) *gin.Context {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		if acceptLang != "" {
			req.Header.Set("Accept-Language", acceptLang)
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		return c
	}

	cases := []struct {
		name string
		url  string
		al   string
		want string
	}{
		{"explicit supported query param", "/?lang=mn", "", "mn"},
		{"unsupported query param falls back", "/?lang=fr", "", DefaultLocale},
		{"no query param, Accept-Language wins", "/", "mn-MN,en;q=0.8", "mn"},
		{"no query param, unsupported Accept-Language falls back", "/", "fr-FR,de;q=0.8", DefaultLocale},
		{"nothing set falls back to default", "/", "", DefaultLocale},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveFromRequest(newCtx(tc.url, tc.al))
			if got != tc.want {
				t.Errorf("ResolveFromRequest() = %q, want %q", got, tc.want)
			}
		})
	}
}
