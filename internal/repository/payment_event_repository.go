package repository

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type PaymentEventRepository interface {
	TryRecord(ctx context.Context, paymentID, eventType, userID, subscriptionID string, creditsAdded int) (alreadyProcessed bool, err error)
	IsProcessed(ctx context.Context, paymentID string) (bool, error)
}

type paymentEventRepository struct {
	collection *mongo.Collection
}

func NewPaymentEventRepository(collection *mongo.Collection) PaymentEventRepository {
	return &paymentEventRepository{collection: collection}
}

func (r *paymentEventRepository) TryRecord(ctx context.Context, paymentID, eventType, userID, subscriptionID string, creditsAdded int) (bool, error) {
	if paymentID == "" {
		return false, apperrors.NewAppError(apperrors.ErrValidation, 400, "payment id is required", "")
	}

	_, err := r.collection.InsertOne(ctx, &models.PaymentEvent{
		PaymentID:      paymentID,
		EventType:      eventType,
		UserID:         userID,
		SubscriptionID: subscriptionID,
		CreditsAdded:   creditsAdded,
		ProcessedAt:    time.Now(),
	})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return true, nil
		}
		return false, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to record payment event", err.Error())
	}
	return false, nil
}

func (r *paymentEventRepository) IsProcessed(ctx context.Context, paymentID string) (bool, error) {
	if paymentID == "" {
		return false, nil
	}
	err := r.collection.FindOne(ctx, bson.M{"paymentId": paymentID}).Err()
	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to check payment event", err.Error())
	}
	return true, nil
}
