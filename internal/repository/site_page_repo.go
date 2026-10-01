package repository

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type SitePageRepo struct {
	col *mongo.Collection
}

func NewSitePageRepo(db *mongo.Database) *SitePageRepo {
	return &SitePageRepo{col: db.Collection("site_pages")}
}

// sitePageFilter selects one page of one tenant. The tenant is part of every
// filter in this file: it is what keeps one tenant's wording from another's.
func sitePageFilter(tenantID primitive.ObjectID, page string) bson.M {
	return bson.M{"tenant_id": tenantID, "page": page}
}

// sitePageListFilter selects every page of one tenant.
func sitePageListFilter(tenantID primitive.ObjectID) bson.M {
	return bson.M{"tenant_id": tenantID}
}

// normalizeBSON turns the driver's own types into plain Go ones. An entry value
// is stored as an arbitrary document, and the driver decodes nested documents
// into primitive.D, which encoding/json renders as an array of {Key, Value}
// pairs instead of an object.
func normalizeBSON(v any) any {
	switch t := v.(type) {
	case primitive.D:
		m := make(map[string]any, len(t))
		for _, e := range t {
			m[e.Key] = normalizeBSON(e.Value)
		}
		return m
	case primitive.M:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = normalizeBSON(e)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = normalizeBSON(e)
		}
		return m
	case primitive.A:
		a := make([]any, len(t))
		for i, e := range t {
			a[i] = normalizeBSON(e)
		}
		return a
	case []any:
		a := make([]any, len(t))
		for i, e := range t {
			a[i] = normalizeBSON(e)
		}
		return a
	default:
		return v
	}
}

func normalizePage(p *models.SitePage) *models.SitePage {
	if p.Entries == nil {
		p.Entries = []models.ContentEntry{}
	}
	for i := range p.Entries {
		for k, v := range p.Entries[i].Values {
			p.Entries[i].Values[k] = normalizeBSON(v)
		}
	}
	return p
}

// List returns the tenant's pages, sorted by page.
func (r *SitePageRepo) List(ctx context.Context, tenantID primitive.ObjectID) ([]*models.SitePage, error) {
	cur, err := r.col.Find(ctx, sitePageListFilter(tenantID),
		options.Find().SetSort(bson.D{{Key: "page", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := []*models.SitePage{}
	for cur.Next(ctx) {
		var p models.SitePage
		if err := cur.Decode(&p); err != nil {
			return nil, err
		}
		out = append(out, normalizePage(&p))
	}
	return out, cur.Err()
}

// Find returns one page, or mongo.ErrNoDocuments.
func (r *SitePageRepo) Find(ctx context.Context, tenantID primitive.ObjectID, page string) (*models.SitePage, error) {
	var p models.SitePage
	if err := r.col.FindOne(ctx, sitePageFilter(tenantID, page)).Decode(&p); err != nil {
		return nil, err
	}
	return normalizePage(&p), nil
}

// Replace upserts the page's entries wholesale. Entries are stored as [] rather
// than null when empty.
func (r *SitePageRepo) Replace(ctx context.Context, tenantID primitive.ObjectID, page string, entries []models.ContentEntry, userID *primitive.ObjectID) error {
	if entries == nil {
		entries = []models.ContentEntry{}
	}
	set := bson.M{"entries": entries, "updated_at": time.Now()}
	update := bson.M{"$set": set}
	if userID != nil {
		set["user_id"] = *userID
	} else {
		update["$unset"] = bson.M{"user_id": ""}
	}
	_, err := r.col.UpdateOne(ctx, sitePageFilter(tenantID, page), update,
		options.Update().SetUpsert(true))
	return err
}
