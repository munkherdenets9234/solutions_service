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

type RentalService struct {
	repo         *repository.RentalRepo
	customerRepo *repository.CustomerRepo
	carRepo      *repository.CarRepo
}

func NewRentalService(repo *repository.RentalRepo, customerRepo *repository.CustomerRepo, carRepo *repository.CarRepo) *RentalService {
	return &RentalService{repo: repo, customerRepo: customerRepo, carRepo: carRepo}
}

type CreateRentalInput struct {
	CarID    string
	Customer models.Customer
	Rental   models.Rental
}

func (s *RentalService) Create(ctx context.Context, tenantID primitive.ObjectID, input CreateRentalInput) (*models.Rental, error) {
	carID, err := primitive.ObjectIDFromHex(input.CarID)
	if err != nil {
		return nil, apierr.BadRequest("invalid car id")
	}

	if _, err := s.carRepo.FindByID(ctx, tenantID, carID); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("car not found")
		}
		return nil, apierr.Internal()
	}

	customer, err := s.customerRepo.Upsert(ctx, tenantID, &input.Customer)
	if err != nil {
		return nil, apierr.Internal()
	}

	rt := input.Rental
	rt.CarID = carID
	rt.CustomerID = customer.ID
	rt.ConfirmationID = uuid.NewString()

	if err := s.repo.Create(ctx, tenantID, &rt); err != nil {
		return nil, apierr.Internal()
	}
	return &rt, nil
}

func (s *RentalService) List(ctx context.Context, tenantID primitive.ObjectID, page, limit int) ([]*models.Rental, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.FindAll(ctx, tenantID, bson.M{}, page, limit)
}

func (s *RentalService) GetByID(ctx context.Context, tenantID primitive.ObjectID, idStr string) (*models.Rental, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, apierr.BadRequest("invalid id")
	}
	rt, err := s.repo.FindByID(ctx, tenantID, id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apierr.NotFound("rental not found")
		}
		return nil, apierr.Internal()
	}
	return rt, nil
}

func (s *RentalService) UpdateStatus(ctx context.Context, tenantID primitive.ObjectID, idStr string, status models.RentalStatus) error {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return apierr.BadRequest("invalid id")
	}
	return s.repo.UpdateStatus(ctx, tenantID, id, status)
}
