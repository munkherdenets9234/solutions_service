package private

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/gin-gonic/gin"
)

// The cap is checked while the body is read, before the service or the tenant
// is touched, so a controller with a nil service proves it: reaching the
// service would panic.
func TestSaveRefusesABodyOverTheCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"entries":[{"path":"a","values":{"en":"` + strings.Repeat("x", translationBodyLimit) + `"}}]}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/translations/hero", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	err := (&translationsController{}).Save(c)

	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != http.StatusRequestEntityTooLarge {
		t.Fatalf("want a 413 apierr, got %v", err)
	}
	if !strings.Contains(ae.Message, "512 KB") {
		t.Fatalf("message should name the cap, got %q", ae.Message)
	}
}

func TestSaveRefusesAMalformedBodyAs400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPut, "/admin/translations/hero", strings.NewReader(`{"entries":`))
	err := (&translationsController{}).Save(c)
	var ae *apierr.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("want a 400 apierr, got %v", err)
	}
}
