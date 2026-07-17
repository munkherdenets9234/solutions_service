package service

import (
	"context"
	"time"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type BlogService struct {
	repo           *repository.BlogRepo
	tenantUserRepo *repository.TenantUserRepo
}

func NewBlogService(repo *repository.BlogRepo, tenantUserRepo *repository.TenantUserRepo) *BlogService {
	return &BlogService{repo: repo, tenantUserRepo: tenantUserRepo}
}

func (s *BlogService) ListPublished(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Blog, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 10
	}
	blogs, total, err := s.repo.FindPublished(ctx, tenantID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveAuthors(ctx, tenantID, blogs); err != nil {
		return nil, 0, apierr.Internal()
	}
	return blogs, total, nil
}

// ListAll returns every blog for a tenant regardless of status, for admin
// management views where drafts need to be visible ahead of publishing. An
// empty status returns both drafts and published blogs.
func (s *BlogService) ListAll(ctx context.Context, tenantID primitive.ObjectID, page, limit int, status models.BlogStatus) ([]*models.Blog, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 10
	}
	if status != "" && status != models.BlogDraft && status != models.BlogPublished {
		return nil, 0, apierr.BadRequest("invalid status")
	}
	blogs, total, err := s.repo.FindAll(ctx, tenantID, status, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveAuthors(ctx, tenantID, blogs); err != nil {
		return nil, 0, apierr.Internal()
	}
	return blogs, total, nil
}

func (s *BlogService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Blog, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	b, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("blog not found")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveAuthors(ctx, tenantID, []*models.Blog{b}); err != nil {
		return nil, apierr.Internal()
	}
	return b, nil
}

func (s *BlogService) GetBySlug(ctx context.Context, tenantID primitive.ObjectID, slug string) (*models.Blog, error) {
	b, err := s.repo.FindBySlug(ctx, tenantID, slug)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("blog not found")
		}
		return nil, apierr.Internal()
	}
	if err := s.resolveAuthors(ctx, tenantID, []*models.Blog{b}); err != nil {
		return nil, apierr.Internal()
	}
	// increment views async — ignore error
	go func() {
		_ = s.repo.IncrementViews(context.Background(), tenantID, b.ID)
	}()
	return b, nil
}

// resolveAuthors overwrites each blog's Author.Name with the display name of
// the tenant user who last created/updated it (Blog.UserID), so the byline
// always reflects who's actually responsible for the current content rather
// than a byline typed in at some earlier edit. Blogs with no UserID (never
// touched by an authenticated tenant user) keep whatever Author.Name was
// already stored.
func (s *BlogService) resolveAuthors(ctx context.Context, tenantID primitive.ObjectID, blogs []*models.Blog) error {
	ids := make([]primitive.ObjectID, 0, len(blogs))
	seen := make(map[primitive.ObjectID]bool, len(blogs))
	for _, b := range blogs {
		if b.UserID != nil && !seen[*b.UserID] {
			seen[*b.UserID] = true
			ids = append(ids, *b.UserID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	users, err := s.tenantUserRepo.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	names := make(map[primitive.ObjectID]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}

	for _, b := range blogs {
		if b.UserID == nil {
			continue
		}
		if name, ok := names[*b.UserID]; ok {
			b.Author.Name = name
			b.LastEditedBy = &name
		}
	}
	return nil
}

func (s *BlogService) Create(ctx context.Context, tenantID primitive.ObjectID, b *models.Blog, userID *primitive.ObjectID) error {
	if b.Slug == "" {
		return apierr.BadRequest("slug is required")
	}
	return s.repo.Create(ctx, tenantID, b, userID)
}

func (s *BlogService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.Update(ctx, tenantID, id, update, userID)
}

func (s *BlogService) Publish(ctx context.Context, tenantID primitive.ObjectID, idStr string, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("blog not found")
	}
	now := time.Now()
	return s.repo.Update(ctx, tenantID, id, bson.M{
		"status":       models.BlogPublished,
		"published_at": now,
	}, userID)
}
