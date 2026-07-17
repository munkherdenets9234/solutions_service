package service

import (
	"context"

	"github.com/eandstravel/digitalservice/internal/models"
	"github.com/eandstravel/digitalservice/internal/repository"
	"github.com/eandstravel/digitalservice/pkg/apierr"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var validTransferTiers = map[models.TransferTier]bool{
	models.TransferStandard: true,
	models.TransferPremium:  true,
	models.TransferVIP:      true,
}

type AirportTransferService struct {
	repo           *repository.AirportTransferRepo
	customerRepo   *repository.CustomerRepo
	tenantUserRepo *repository.TenantUserRepo
}

func NewAirportTransferService(repo *repository.AirportTransferRepo, customerRepo *repository.CustomerRepo, tenantUserRepo *repository.TenantUserRepo) *AirportTransferService {
	return &AirportTransferService{repo: repo, customerRepo: customerRepo, tenantUserRepo: tenantUserRepo}
}

type CreateTransferInput struct {
	Customer models.Customer
	Transfer models.AirportTransfer
}

// AirportTransferDetail is an airport transfer enriched with its customer's
// details for API responses.
type AirportTransferDetail struct {
	models.AirportTransfer
	Customer *models.Customer `json:"customer,omitempty"`
}

func (s *AirportTransferService) Create(ctx context.Context, tenantID primitive.ObjectID, input CreateTransferInput) (*AirportTransferDetail, error) {
	if !validTransferTiers[input.Transfer.Tier] {
		return nil, apierr.BadRequest("invalid tier")
	}

	customer, err := s.customerRepo.Upsert(ctx, tenantID, &input.Customer)
	if err != nil {
		return nil, apierr.Internal()
	}

	t := input.Transfer
	t.CustomerID = customer.ID
	t.ConfirmationID = uuid.NewString()

	if err := s.repo.Create(ctx, tenantID, &t); err != nil {
		return nil, apierr.Internal()
	}
	return &AirportTransferDetail{AirportTransfer: t, Customer: customer}, nil
}

func (s *AirportTransferService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*AirportTransferDetail, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	transfers, total, err := s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, transfers); err != nil {
		return nil, 0, apierr.Internal()
	}

	ids := make([]primitive.ObjectID, 0, len(transfers))
	seen := make(map[primitive.ObjectID]bool, len(transfers))
	for _, t := range transfers {
		if !seen[t.CustomerID] {
			seen[t.CustomerID] = true
			ids = append(ids, t.CustomerID)
		}
	}
	customers, err := s.customerRepo.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, 0, apierr.Internal()
	}
	byID := make(map[primitive.ObjectID]*models.Customer, len(customers))
	for _, c := range customers {
		byID[c.ID] = c
	}

	details := make([]*AirportTransferDetail, len(transfers))
	for i, t := range transfers {
		details[i] = &AirportTransferDetail{AirportTransfer: *t, Customer: byID[t.CustomerID]}
	}
	return details, total, nil
}

func (s *AirportTransferService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*AirportTransferDetail, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	t, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("airport transfer not found")
		}
		return nil, apierr.Internal()
	}

	customer, err := s.customerRepo.FindByID(ctx, tenantID, t.CustomerID)
	if err != nil && err != mongo.ErrNoDocuments {
		return nil, apierr.Internal()
	}
	if err := s.resolveLastEditedBy(ctx, tenantID, []*models.AirportTransfer{t}); err != nil {
		return nil, apierr.Internal()
	}
	return &AirportTransferDetail{AirportTransfer: *t, Customer: customer}, nil
}

// resolveLastEditedBy populates each transfer's LastEditedBy with the display
// name of the tenant user referenced by its UserID, for admin GET responses.
func (s *AirportTransferService) resolveLastEditedBy(ctx context.Context, tenantID primitive.ObjectID, transfers []*models.AirportTransfer) error {
	ids := make([]primitive.ObjectID, 0, len(transfers))
	seen := make(map[primitive.ObjectID]bool, len(transfers))
	for _, t := range transfers {
		if t.UserID != nil && !seen[*t.UserID] {
			seen[*t.UserID] = true
			ids = append(ids, *t.UserID)
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

	for _, t := range transfers {
		if t.UserID == nil {
			continue
		}
		if name, ok := names[*t.UserID]; ok {
			t.LastEditedBy = &name
		}
	}
	return nil
}

func (s *AirportTransferService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.TransferStatus, userID *primitive.ObjectID) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status, userID)
}
