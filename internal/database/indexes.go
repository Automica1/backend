// internal/database/indexes.go
package database

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func (m *MongoDB) CreateIndexes(ctx context.Context) error {
	log.Println("Creating database indexes...")

	// Users collection indexes
	usersCollection := m.GetCollection("users")
	if err := m.createUsersIndexes(ctx, usersCollection); err != nil {
		return err
	}

	// Credits collection indexes
	creditsCollection := m.GetCollection("credits")
	if err := m.createCreditsIndexes(ctx, creditsCollection); err != nil {
		return err
	}

	paymentEventsCollection := m.GetCollection("payment_events")
	if err := m.createPaymentEventsIndexes(ctx, paymentEventsCollection); err != nil {
		return err
	}

	betaServicesCollection := m.GetCollection("beta_services")
	if err := m.createBetaServicesIndexes(ctx, betaServicesCollection); err != nil {
		return err
	}

	betaFeedbackCollection := m.GetCollection("beta_feedback_sessions")
	if err := m.createBetaFeedbackSessionsIndexes(ctx, betaFeedbackCollection); err != nil {
		return err
	}

	jobsCollection := m.GetCollection("jobs")
	if err := m.createJobsIndexes(ctx, jobsCollection); err != nil {
		return err
	}

	gpuPoolsCollection := m.GetCollection("gpu_pools")
	if err := m.createGPUPoolsIndexes(ctx, gpuPoolsCollection); err != nil {
		return err
	}

	log.Println("✅ Database indexes created successfully")
	return nil
}

func (m *MongoDB) createUsersIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{"userId", 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{"email", 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ Users collection indexes created")
	return nil
}

func (m *MongoDB) createCreditsIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{"userId", 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ Credits collection indexes created")
	return nil
}

func (m *MongoDB) createPaymentEventsIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{"paymentId", 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ Payment events collection indexes created")
	return nil
}

func (m *MongoDB) createBetaServicesIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "tag", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "serviceName", Value: 1}, {Key: "isActive", Value: 1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ Beta services collection indexes created")
	return nil
}

func (m *MongoDB) createBetaFeedbackSessionsIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "createdAt", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "serviceName", Value: 1}, {Key: "createdAt", Value: -1}},
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "serviceName", Value: 1},
				{Key: "status", Value: 1},
				{Key: "createdAt", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "userId", Value: 1},
				{Key: "status", Value: 1},
				{Key: "refundedAt", Value: -1},
			},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ Beta feedback sessions collection indexes created")
	return nil
}

func (m *MongoDB) createJobsIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
				{Key: "runAfter", Value: 1},
			},
		},
		{
			Keys:    bson.D{{Key: "type", Value: 1}},
			Options: options.Index(),
		},
		{
			Keys:    bson.D{{Key: "idempotencyKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ Jobs collection indexes created")
	return nil
}

func (m *MongoDB) createGPUPoolsIndexes(ctx context.Context, collection *mongo.Collection) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "serviceTag", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "state", Value: 1}, {Key: "updatedAt", Value: -1}},
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return err
	}

	log.Println("✅ GPU pools collection indexes created")
	return nil
}