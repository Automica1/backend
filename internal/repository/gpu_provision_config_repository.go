package repository

import (
	"context"
	"errors"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type GPUProvisionConfigRepository interface {
	GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUProvisionConfig, error)
	Upsert(ctx context.Context, cfg *models.GPUProvisionConfig) (*models.GPUProvisionConfig, error)
}

type gpuProvisionConfigRepository struct {
	collection *mongo.Collection
}

func NewGPUProvisionConfigRepository(collection *mongo.Collection) GPUProvisionConfigRepository {
	return &gpuProvisionConfigRepository{collection: collection}
}

func (r *gpuProvisionConfigRepository) GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUProvisionConfig, error) {
	var cfg models.GPUProvisionConfig
	err := r.collection.FindOne(ctx, bson.M{"serviceTag": serviceTag}).Decode(&cfg)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	cfg.Normalize()
	return &cfg, nil
}

func (r *gpuProvisionConfigRepository) Upsert(ctx context.Context, cfg *models.GPUProvisionConfig) (*models.GPUProvisionConfig, error) {
	now := time.Now().UTC()
	cfg.UpdatedAt = now
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.Normalize()
	filter := bson.M{"serviceTag": cfg.ServiceTag}
	update := bson.M{
		"$set": bson.M{
			"serviceTag":     cfg.ServiceTag,
			"serviceName":    cfg.ServiceName,
			"infrastructure": cfg.Infrastructure,
			"timeouts":       cfg.Timeouts,
			"retries":        cfg.Retries,
			"lifecycle":      cfg.Lifecycle,
			"flags":          cfg.Flags,
			"updatedAt":      cfg.UpdatedAt,
			"updatedBy":      cfg.UpdatedBy,
			// Legacy mirrors for backward compat readers.
			"primaryProvider":            cfg.Infrastructure.PrimaryProvider,
			"fallbackProvider":           cfg.Infrastructure.FallbackProvider,
			"sshReadyTimeoutSec":         cfg.Timeouts.SSHReadyPrimarySec,
			"fallbackSshReadyTimeoutSec": cfg.Timeouts.SSHReadyFallbackSec,
			"e2e":                        cfg.Infrastructure.E2E,
			"aws":                        cfg.Infrastructure.AWS,
			"gcp":                        cfg.Infrastructure.GCP,
		},
		"$setOnInsert": bson.M{
			"createdAt": cfg.CreatedAt,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var out models.GPUProvisionConfig
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&out)
	if err != nil {
		return nil, err
	}
	out.Normalize()
	return &out, nil
}
