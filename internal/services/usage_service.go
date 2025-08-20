// 3. Create the usage service
// internal/services/usage_service.go
package services

import (
	"context"
	// "net/http"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
)

type UsageService interface {
	TrackUsage(ctx context.Context, req *models.UsageTrackingRequest) error
	GetGlobalStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error)
	GetUserStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UserUsageStats, error)
	GetServiceUserStats(ctx context.Context, serviceName string, startDate, endDate *time.Time) ([]models.ServiceUserStats, error)
	GetUserUsageHistory(ctx context.Context, userID string, limit, skip int) ([]models.ServiceUsage, error)
	GetServiceUsageHistory(ctx context.Context, serviceName string, limit, skip int) ([]models.ServiceUsage, error)
}

type usageService struct {
	usageRepo repository.UsageRepository
}

func NewUsageService(usageRepo repository.UsageRepository) UsageService {
	return &usageService{
		usageRepo: usageRepo,
	}
}

func (s *usageService) TrackUsage(ctx context.Context, req *models.UsageTrackingRequest) error {
	usage := &models.ServiceUsage{
		UserID:      req.UserID,
		Email:       req.Email,
		ServiceName: req.ServiceName,
		Endpoint:    req.Endpoint,
		Method:      req.Method,
		Success:     req.Success,
		ErrorMsg:    req.ErrorMsg,
		CreditsUsed: req.CreditsUsed,
		RequestID:   req.RequestID,
		IPAddress:   req.IPAddress,
		UserAgent:   req.UserAgent,
		AuthMethod:  req.AuthMethod,
		ProcessTime: req.ProcessTime,
	}

	return s.usageRepo.CreateUsage(ctx, usage)
}

func (s *usageService) GetGlobalStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error) {
	return s.usageRepo.GetGlobalStats(ctx, startDate, endDate)
}

func (s *usageService) GetUserStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UserUsageStats, error) {
	return s.usageRepo.GetUserStats(ctx, startDate, endDate)
}

func (s *usageService) GetServiceUserStats(ctx context.Context, serviceName string, startDate, endDate *time.Time) ([]models.ServiceUserStats, error) {
	return s.usageRepo.GetServiceUserStats(ctx, serviceName, startDate, endDate)
}

func (s *usageService) GetUserUsageHistory(ctx context.Context, userID string, limit, skip int) ([]models.ServiceUsage, error) {
	return s.usageRepo.GetUserUsageHistory(ctx, userID, limit, skip)
}

func (s *usageService) GetServiceUsageHistory(ctx context.Context, serviceName string, limit, skip int) ([]models.ServiceUsage, error) {
	return s.usageRepo.GetServiceUsageHistory(ctx, serviceName, limit, skip)
}