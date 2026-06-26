package repository

import (
	"context"
	"errors"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type BetaFeedbackRepository interface {
	Create(ctx context.Context, session *models.BetaFeedbackSession) error
	GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaFeedbackSession, error)
	GetPendingByUserAndService(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSession, error)
	SubmitFeedback(ctx context.Context, id primitive.ObjectID, expected *models.BetaFeedbackExpectedResult, refundedCredits int) error
	SumRefundedCreditsSince(ctx context.Context, userID string, since time.Time) (int, error)
	List(ctx context.Context, serviceName string, limit int) ([]*models.BetaFeedbackSession, error)
}

type betaFeedbackRepository struct {
	collection *mongo.Collection
}

func NewBetaFeedbackRepository(collection *mongo.Collection) BetaFeedbackRepository {
	return &betaFeedbackRepository{collection: collection}
}

func (r *betaFeedbackRepository) Create(ctx context.Context, session *models.BetaFeedbackSession) error {
	result, err := r.collection.InsertOne(ctx, session)
	if err != nil {
		return err
	}
	session.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *betaFeedbackRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaFeedbackSession, error) {
	var session models.BetaFeedbackSession
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&session)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta feedback session not found")
		}
		return nil, err
	}
	return &session, nil
}

func (r *betaFeedbackRepository) GetPendingByUserAndService(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSession, error) {
	filter := bson.M{
		"userId":      userID,
		"serviceName": serviceName,
		"status":      models.BetaFeedbackStatusPendingFeedback,
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})

	var session models.BetaFeedbackSession
	err := r.collection.FindOne(ctx, filter, opts).Decode(&session)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (r *betaFeedbackRepository) SubmitFeedback(ctx context.Context, id primitive.ObjectID, expected *models.BetaFeedbackExpectedResult, refundedCredits int) error {
	now := time.Now()
	update := bson.M{
		"expectedResult":      expected,
		"status":              models.BetaFeedbackStatusRefunded,
		"creditsRefunded":     refundedCredits,
		"feedbackSubmittedAt": now,
	}
	if refundedCredits > 0 {
		update["refundedAt"] = now
	}
	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{
			"_id":    id,
			"status": models.BetaFeedbackStatusPendingFeedback,
		},
		bson.M{"$set": update},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta feedback session not found or already submitted")
	}
	return nil
}

func (r *betaFeedbackRepository) SumRefundedCreditsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    userID,
			"status":    models.BetaFeedbackStatusRefunded,
			"refundedAt": bson.M{"$gte": since},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"total": bson.M{"$sum": "$creditsRefunded"},
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Total int `bson:"total"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0].Total, nil
}

func (r *betaFeedbackRepository) List(ctx context.Context, serviceName string, limit int) ([]*models.BetaFeedbackSession, error) {
	if limit <= 0 {
		limit = 50
	}
	filter := bson.M{}
	if serviceName != "" {
		filter["serviceName"] = serviceName
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var sessions []*models.BetaFeedbackSession
	if err := cursor.All(ctx, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}
