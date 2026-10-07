package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eandstravel/digitalservice/internal/i18n"
	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// reviewStore is the slice of the review repository this service uses.
type reviewStore interface {
	Create(ctx context.Context, tenantID primitive.ObjectID, rev *models.Review, userID *primitive.ObjectID) error
	FindAll(ctx context.Context, tenantID primitive.ObjectID, filter bson.M, page, limit int) ([]*models.Review, int64, error)
	FindByID(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (*models.Review, error)
	Update(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID, update bson.M, userID *primitive.ObjectID) error
	Delete(ctx context.Context, tenantID primitive.ObjectID, id primitive.ObjectID) (int64, error)
}

// customerFinder looks one customer up inside one tenant.
type customerFinder interface {
	FindByID(ctx context.Context, tenantID, id primitive.ObjectID) (*models.Customer, error)
}

// customerLookup adds the batch read used for public avatars.
type customerLookup interface {
	customerFinder
	FindByIDs(ctx context.Context, tenantID primitive.ObjectID, ids []primitive.ObjectID) ([]*models.Customer, error)
}

type ReviewService struct {
	repo           reviewStore
	tenantUserRepo *repository.TenantUserRepo
	customers      customerLookup
}

// WithCustomers enables linking reviews to customers.
func (s *ReviewService) WithCustomers(c customerLookup) *ReviewService {
	s.customers = c
	return s
}

// resolveCustomer returns the customer in this tenant, or a 404 that is the
// same whether the id is unknown or belongs to another tenant.
func (s *ReviewService) resolveCustomer(ctx context.Context, tenantID, id primitive.ObjectID) (*models.Customer, error) {
	if s.customers == nil {
		return nil, apierr.NotFound("customer")
	}
	c, err := s.customers.FindByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apierr.NotFound("customer")
		}
		return nil, apierr.Internal(err)
	}
	return c, nil
}

// AvatarsFor maps customer id to avatar URL for the reviews that link a
// customer, with ONE tenant-scoped batch query. Customers without an avatar
// are omitted.
func (s *ReviewService) AvatarsFor(ctx context.Context, tenantID primitive.ObjectID, reviews []*models.Review) (map[primitive.ObjectID]string, error) {
	ids := make([]primitive.ObjectID, 0, len(reviews))
	seen := make(map[primitive.ObjectID]bool, len(reviews))
	for _, r := range reviews {
		if r.CustomerID != nil && !seen[*r.CustomerID] {
			seen[*r.CustomerID] = true
			ids = append(ids, *r.CustomerID)
		}
	}
	if len(ids) == 0 || s.customers == nil {
		return nil, nil
	}
	rows, err := s.customers.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make(map[primitive.ObjectID]string, len(rows))
	for _, c := range rows {
		if c.AvatarURL != "" {
			out[c.ID] = c.AvatarURL
		}
	}
	return out, nil
}

func NewReviewService(repo *repository.ReviewRepo, tenantUserRepo *repository.TenantUserRepo) *ReviewService {
	return &ReviewService{repo: repo, tenantUserRepo: tenantUserRepo}
}

type ListReviewsFilter struct {
	Tour    string
	Partner string
	Page    int
	Limit   int
}

func (s *ReviewService) List(ctx context.Context, tenantID primitive.ObjectID, f ListReviewsFilter) ([]*models.Review, int64, error) {
	filter := bson.M{}
	if f.Tour != "" {
		filter["related_tour"] = f.Tour
	}
	if f.Partner != "" {
		filter["related_partner"] = f.Partner
	}

	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	reviews, total, err := s.repo.FindAll(ctx, tenantID, filter, f.Page, f.Limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, reviews); err != nil {
		return nil, 0, apierr.Internal(err)
	}
	return reviews, total, nil
}

func (s *ReviewService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Review, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	rev, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("review not found")
		}
		return nil, apierr.Internal(err)
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.Review{rev}); err != nil {
		return nil, apierr.Internal(err)
	}
	return rev, nil
}

// resolveLastEditedBy populates each review's LastEditedBy with the display
// name of the tenant user referenced by its UserID, for admin GET responses.
func (s *ReviewService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, reviews []*models.Review) error {
	ids := make([]primitive.ObjectID, 0, len(reviews))
	seen := make(map[primitive.ObjectID]bool, len(reviews))
	for _, rev := range reviews {
		if rev.UserID != nil && !seen[*rev.UserID] {
			seen[*rev.UserID] = true
			ids = append(ids, *rev.UserID)
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

	for _, rev := range reviews {
		if rev.UserID == nil {
			continue
		}
		if name, ok := names[*rev.UserID]; ok {
			rev.LastEditedBy = &name
		}
	}
	return nil
}

func (s *ReviewService) Create(ctx context.Context, tenantID primitive.ObjectID, rev *models.Review, userID *primitive.ObjectID) error {
	if rev.Star < 1 || rev.Star > 5 {
		return apierr.BadRequest("star must be between 1 and 5")
	}
	if i18n.Resolve(rev.Review, i18n.DefaultLocale) == "" {
		return apierr.BadRequest("review is required")
	}
	if rev.CustomerID != nil {
		c, err := s.resolveCustomer(ctx, tenantID, *rev.CustomerID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(rev.RelatedCustomer) == "" {
			rev.RelatedCustomer = c.Name
		}
	}
	return s.repo.Create(ctx, tenantID, rev, userID)
}

func (s *ReviewService) Update(ctx context.Context, tenantID primitive.ObjectID, idStr string, update bson.M, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	// A JSON body decodes numbers into float64 in a bson.M.
	if v, ok := update["star"]; ok {
		f, isNum := v.(float64)
		if !isNum || f != float64(int(f)) || int(f) < 1 || int(f) > 5 {
			return apierr.BadRequest("star must be between 1 and 5")
		}
	}
	if _, err := s.repo.FindByID(ctx, tenantID, id); err != nil {
		return apierr.NotFound("review not found")
	}
	if v, ok := update["customer_id"]; ok {
		switch cv := v.(type) {
		case nil:
			// null clears the link
		case string:
			cid, perr := primitive.ObjectIDFromHex(cv)
			if perr != nil {
				return apierr.BadRequest("invalid customer_id")
			}
			c, err := s.resolveCustomer(ctx, tenantID, cid)
			if err != nil {
				return err
			}
			update["customer_id"] = cid
			if rc, has := update["related_customer"]; !has || strings.TrimSpace(fmt.Sprint(rc)) == "" {
				update["related_customer"] = c.Name
			}
		default:
			return apierr.BadRequest("invalid customer_id")
		}
	}
	return s.repo.Update(ctx, tenantID, id, update, userID)
}

func (s *ReviewService) Delete(ctx context.Context, tenantID primitive.ObjectID, idStr string) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	deleted, err := s.repo.Delete(ctx, tenantID, id)
	if err != nil {
		return apierr.Internal(err)
	}
	if deleted == 0 {
		return apierr.NotFound("review not found")
	}
	return nil
}
