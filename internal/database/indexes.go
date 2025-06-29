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