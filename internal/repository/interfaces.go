// internal/repository/interfaces.go
package repository

import (
	"context"

	"chi-mongo-backend/internal/models"
)

type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByUserID(ctx context.Context, userID string) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	Delete(ctx context.Context, userID string) error
}

type CreditsRepository interface {
	Create(ctx context.Context, credits *models.Credits) error
	GetByUserID(ctx context.Context, userID string) (*models.Credits, error)
	UpdateCredits(ctx context.Context, userID string, amount int) error
	DeductCredits(ctx context.Context, userID string, amount int) error
}