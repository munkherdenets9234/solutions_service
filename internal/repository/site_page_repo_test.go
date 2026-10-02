package repository

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eandstravel/digitalservice/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestNormalizeBSONTurnsDriverTypesIntoPlainJSONShapes(t *testing.T) {
	in := primitive.A{primitive.D{{Key: "q", Value: "a"}}}
	got := normalizeBSON(in)

	want := []any{map[string]any{"q": "a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[{"q":"a"}]` {
		t.Fatalf("json = %s", b)
	}

	nested := normalizeBSON(primitive.M{"a": primitive.A{primitive.M{"b": primitive.D{{Key: "c", Value: "d"}}}}})
	wantNested := map[string]any{"a": []any{map[string]any{"b": map[string]any{"c": "d"}}}}
	if !reflect.DeepEqual(nested, wantNested) {
		t.Fatalf("nested got %#v, want %#v", nested, wantNested)
	}
}

func TestNormalizeBSONLeavesStringsAlone(t *testing.T) {
	if got := normalizeBSON("hello"); got != "hello" {
		t.Fatalf("got %#v", got)
	}
}

func TestSitePageFiltersAlwaysCarryTheTenant(t *testing.T) {
	tenant := primitive.NewObjectID()

	f := sitePageFilter(tenant, "hero")
	if f["tenant_id"] != tenant || f["page"] != "hero" {
		t.Fatalf("page filter = %#v", f)
	}
	l := sitePageListFilter(tenant)
	if l["tenant_id"] != tenant || len(l) != 1 {
		t.Fatalf("list filter = %#v", l)
	}
}

func TestNormalizePageNormalizesBase(t *testing.T) {
	p := &models.SitePage{Entries: []models.ContentEntry{{
		Path:   "faq",
		Values: map[string]any{"en": primitive.A{primitive.D{{Key: "q", Value: "v"}}}},
		Base:   map[string]any{"en": primitive.A{primitive.D{{Key: "q", Value: "b"}}}},
	}}}
	normalizePage(p)
	b, err := json.Marshal(p.Entries[0].Base)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"en":[{"q":"b"}]}` {
		t.Fatalf("base json = %s", b)
	}
}
