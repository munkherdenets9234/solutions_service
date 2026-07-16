// Package i18n resolves per-locale content stored as map[string]string /
// map[string][]string fields (see internal/models) down to a single value
// for a given request locale, with fallback to DefaultLocale.
package i18n

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const DefaultLocale = "en"

var SupportedLocales = []string{"en", "mn"}

func IsSupported(locale string) bool {
	for _, l := range SupportedLocales {
		if l == locale {
			return true
		}
	}
	return false
}

// Resolve returns m[locale] if present and non-empty, falling back to
// m[DefaultLocale], then to any value in m, then "".
func Resolve(m map[string]string, locale string) string {
	if v, ok := m[locale]; ok && v != "" {
		return v
	}
	if v, ok := m[DefaultLocale]; ok && v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

// ResolveList is Resolve for map[string][]string fields (e.g. Highlights, Activities).
func ResolveList(m map[string][]string, locale string) []string {
	if v, ok := m[locale]; ok && len(v) > 0 {
		return v
	}
	if v, ok := m[DefaultLocale]; ok && len(v) > 0 {
		return v
	}
	for _, v := range m {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

// ResolveFromRequest determines the request's locale: ?lang= query param,
// else the first supported language in Accept-Language, else DefaultLocale.
func ResolveFromRequest(c *gin.Context) string {
	if lang := c.Query("lang"); lang != "" && IsSupported(lang) {
		return lang
	}
	if accept := c.GetHeader("Accept-Language"); accept != "" {
		for _, part := range strings.Split(accept, ",") {
			tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
			// Accept-Language tags can be region-qualified (e.g. "en-US") — match on
			// the primary subtag.
			primary, _, _ := strings.Cut(tag, "-")
			if IsSupported(primary) {
				return primary
			}
		}
	}
	return DefaultLocale
}
