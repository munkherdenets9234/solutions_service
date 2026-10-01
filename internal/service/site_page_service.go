package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	sitePageMaxLen  = 64
	sitePathMaxLen  = 200
	siteMaxEntries  = 1000
	siteMaxText     = 5000 // characters in one string
	siteMaxArrayLen = 100
)

// sitePageStore is the persistence this service needs, narrow for the same
// reason as the password reset service: the rules below are what keep one
// tenant's wording from another's, and they should be testable without a
// database. Find returns mongo.ErrNoDocuments when the page does not exist.
type sitePageStore interface {
	List(ctx context.Context, tenantID primitive.ObjectID) ([]*models.SitePage, error)
	Find(ctx context.Context, tenantID primitive.ObjectID, page string) (*models.SitePage, error)
	Replace(ctx context.Context, tenantID primitive.ObjectID, page string, entries []models.ContentEntry, userID *primitive.ObjectID) error
}

// SitePageSummary is one row of the list of pages that have overrides.
type SitePageSummary struct {
	Page      string    `json:"page"`
	Entries   int       `json:"entries"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SitePageService stores the editable wording of a tenant's public site as
// overrides. It validates the shape of what is stored, not whether a key exists:
// it does not have the site's locale files. The site decides what a stored value
// means and ignores one that does not fit.
type SitePageService struct {
	store sitePageStore
}

func NewSitePageService(store sitePageStore) *SitePageService {
	return &SitePageService{store: store}
}

// List returns the pages that have entries, with counts, sorted by page. It is
// never nil, so it serialises as [].
func (s *SitePageService) List(ctx context.Context, tenantID primitive.ObjectID) ([]SitePageSummary, error) {
	pages, err := s.store.List(ctx, tenantID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]SitePageSummary, 0, len(pages))
	for _, p := range pages {
		out = append(out, SitePageSummary{Page: p.Page, Entries: len(p.Entries), UpdatedAt: p.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Page < out[j].Page })
	return out, nil
}

// Get returns one page. A page with nothing stored is an empty page, not an
// error: the editor opens on it.
func (s *SitePageService) Get(ctx context.Context, tenantID primitive.ObjectID, page string) (*models.SitePage, error) {
	if err := validateSitePage(page); err != nil {
		return nil, err
	}
	p, err := s.store.Find(ctx, tenantID, page)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return &models.SitePage{TenantID: tenantID, Page: page, Entries: []models.ContentEntry{}}, nil
		}
		return nil, apierr.Internal(err)
	}
	if p.Entries == nil {
		p.Entries = []models.ContentEntry{}
	}
	return p, nil
}

// Save replaces the whole page with the cleaned entries and returns how many
// were stored. Removing an override is saving without it.
func (s *SitePageService) Save(ctx context.Context, tenantID primitive.ObjectID, page string, entries []models.ContentEntry, userID *primitive.ObjectID) (int, error) {
	if err := validateSitePage(page); err != nil {
		return 0, err
	}
	cleaned, err := validateEntries(entries)
	if err != nil {
		return 0, err
	}
	if err := s.store.Replace(ctx, tenantID, page, cleaned, userID); err != nil {
		return 0, apierr.Internal(err)
	}
	return len(cleaned), nil
}

// Public returns {page: {path: value}} for one language. Pages and entries with
// nothing for that language are omitted; the result is {} rather than nil.
func (s *SitePageService) Public(ctx context.Context, tenantID primitive.ObjectID, lang string) (map[string]map[string]any, error) {
	if !validSiteLocale(lang) {
		return nil, apierr.BadRequest("lang must be one of en, mn, ko")
	}
	pages, err := s.store.List(ctx, tenantID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := map[string]map[string]any{}
	for _, p := range pages {
		for _, e := range p.Entries {
			v, ok := e.Values[lang]
			if !ok {
				continue
			}
			if out[p.Page] == nil {
				out[p.Page] = map[string]any{}
			}
			out[p.Page][e.Path] = v
		}
	}
	return out, nil
}

func validSiteLocale(l string) bool {
	for _, k := range models.SiteLocales {
		if l == k {
			return true
		}
	}
	return false
}

func validateSitePage(page string) error {
	if page == "" || len(page) > sitePageMaxLen {
		return apierr.BadRequest("page must be 1-64 characters")
	}
	for _, r := range page {
		if !isSiteNameChar(r) {
			return apierr.BadRequest("page may contain only letters, digits, - and _")
		}
	}
	return nil
}

func isSiteNameChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

func validateSitePath(path string) error {
	if path == "" || len(path) > sitePathMaxLen {
		return apierr.BadRequest("path must be 1-200 characters")
	}
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			return apierr.BadRequest("path " + path + " has an empty segment")
		}
		for _, r := range seg {
			if !isSiteNameChar(r) {
				return apierr.BadRequest("path " + path + " may contain only letters, digits, -, _ and .")
			}
		}
	}
	return nil
}

// validateEntries checks every rule on the submitted list and returns the
// cleaned one: a language whose value is blank is dropped (blank means "use the
// shipped wording", and storing it would blank the site), and so is an entry
// left with no language. Values are otherwise kept exactly as sent.
func validateEntries(entries []models.ContentEntry) ([]models.ContentEntry, error) {
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if err := validateSitePath(e.Path); err != nil {
			return nil, err
		}
		if _, dup := seen[e.Path]; dup {
			return nil, apierr.BadRequest("duplicate path " + e.Path)
		}
		seen[e.Path] = struct{}{}
	}
	if len(entries) > siteMaxEntries {
		return nil, apierr.BadRequest("a page may have at most 1000 entries")
	}

	cleaned := make([]models.ContentEntry, 0, len(entries))
	for _, e := range entries {
		values := make(map[string]any, len(e.Values))
		for lang, v := range e.Values {
			if !validSiteLocale(lang) {
				return nil, apierr.BadRequest("unknown language " + lang + " at " + e.Path)
			}
			blank, err := checkSiteValue(v)
			if err != nil {
				return nil, apierr.BadRequest(err.Error() + " at " + e.Path + " (" + lang + ")")
			}
			if !blank {
				values[lang] = v
			}
		}
		if len(values) > 0 {
			cleaned = append(cleaned, models.ContentEntry{Path: e.Path, Values: values})
		}
	}
	return cleaned, nil
}

// checkSiteValue validates one language's value and reports whether it is blank.
// JSON-decoded values arrive as string, []any and map[string]any.
func checkSiteValue(v any) (blank bool, err error) {
	switch x := v.(type) {
	case string:
		if utf8.RuneCountInString(x) > siteMaxText {
			return false, errors.New("text is longer than 5000 characters")
		}
		return strings.TrimSpace(x) == "", nil
	case []any:
		if len(x) > siteMaxArrayLen {
			return false, errors.New("an array may have at most 100 items")
		}
		if len(x) == 0 {
			return true, nil
		}
		_, firstIsText := x[0].(string)
		for _, item := range x {
			switch it := item.(type) {
			case string:
				if !firstIsText {
					return false, errors.New("an array must hold only strings or only objects")
				}
				if utf8.RuneCountInString(it) > siteMaxText {
					return false, errors.New("text is longer than 5000 characters")
				}
			case map[string]any:
				if firstIsText {
					return false, errors.New("an array must hold only strings or only objects")
				}
				for _, f := range it {
					fs, ok := f.(string)
					if !ok {
						return false, errors.New("object fields must be strings")
					}
					if utf8.RuneCountInString(fs) > siteMaxText {
						return false, errors.New("text is longer than 5000 characters")
					}
				}
			default:
				return false, errors.New("an array may hold only strings or flat objects of strings")
			}
		}
		return false, nil
	default:
		return false, errors.New("a value must be a string or an array")
	}
}
