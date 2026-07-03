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
	repo         *repository.AirportTransferRepo
	customerRepo *repository.CustomerRepo
}

func NewAirportTransferService(repo *repository.AirportTransferRepo, customerRepo *repository.CustomerRepo) *AirportTransferService {
	return &AirportTransferService{repo: repo, customerRepo: customerRepo}
}

type CreateTransferInput struct {
	Customer models.Customer
	Transfer models.AirportTransfer
}

func (s *AirportTransferService) Create(ctx context.Context, tenantID primitive.ObjectID, input CreateTransferInput) (*models.AirportTransfer, error) {
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
	return &t, nil
}

func (s *AirportTransferService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.AirportTransfer, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
}

func (s *AirportTransferService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.AirportTransfer, error) {
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
	return t, nil
}

func (s *AirportTransferService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.TransferStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status)
}
