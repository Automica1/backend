// internal/repository/interfaces.go
package repository

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
)

type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByUserID(ctx context.Context, userID string) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	IsSuspendedByEmail(ctx context.Context, email string) (bool, error)
	Delete(ctx context.Context, userID string) error
	UpdateActiveStatus(ctx context.Context, userID string, isActive bool) error
	UpdateBillingCurrency(ctx context.Context, userID string, currency string) error
	ClearBillingCurrency(ctx context.Context, userID string) error
	UpdateBetaFeedbackRefundCapOverride(ctx context.Context, userID string, cap *int) error
	SetBetaFeedbackRefundBudgetResetAt(ctx context.Context, userID string, at time.Time) error
	// Admin methods
	GetAll(ctx context.Context) ([]models.User, error)
	GetTotalCount(ctx context.Context) (int64, error)
}

type CreditsRepository interface {
	Create(ctx context.Context, credits *models.Credits) error
	GetByUserID(ctx context.Context, userID string) (*models.Credits, error)
	UpdateCredits(ctx context.Context, userID string, amount int) error
	UpsertCredits(ctx context.Context, userID string, amount int) (*models.Credits, error)
	DeductCredits(ctx context.Context, userID string, amount int) (*models.Credits, error)
	DeleteByUserID(ctx context.Context, userID string) error
	// Admin methods
	GetTotalCredits(ctx context.Context) (int64, error)
	GetAllWithUsers(ctx context.Context) ([]models.AdminUser, error)
}

type ActivityRepository interface {
	Create(ctx context.Context, activity *models.ActivityLog) error
	GetByUserID(ctx context.Context, userID string) ([]models.ActivityLog, error)
	DeleteByUserID(ctx context.Context, userID string) error
}

type AdminAuditRepository interface {
	Create(ctx context.Context, log *models.AdminAuditLog) error
	GetRecent(ctx context.Context, limit int) ([]models.AdminAuditLog, error)
	GetRecentPaged(ctx context.Context, limit, skip int) ([]models.AdminAuditLog, error)
	Count(ctx context.Context) (int64, error)
}
