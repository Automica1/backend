package services

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const defaultMonthlyRefundCap = 50

type BetaFeedbackService interface {
	CreateSession(ctx context.Context, req *models.CreateBetaFeedbackSessionRequest) (*models.BetaFeedbackSession, error)
	GetPendingSession(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSessionSummary, error)
	HasPendingSession(ctx context.Context, userID, serviceName string) (bool, error)
	SubmitFeedback(ctx context.Context, userID, email, sessionID string, expected *models.BetaFeedbackExpectedResult) (*models.SubmitBetaFeedbackResponse, error)
	ListSessions(ctx context.Context, serviceName string, limit int) ([]models.BetaFeedbackSession, error)
}

type betaFeedbackService struct {
	repo           repository.BetaFeedbackRepository
	creditsService CreditsService
}

func NewBetaFeedbackService(repo repository.BetaFeedbackRepository, creditsService CreditsService) BetaFeedbackService {
	return &betaFeedbackService{
		repo:           repo,
		creditsService: creditsService,
	}
}

func (s *betaFeedbackService) CreateSession(ctx context.Context, req *models.CreateBetaFeedbackSessionRequest) (*models.BetaFeedbackSession, error) {
	if req == nil || req.UserID == "" || req.ServiceName == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid beta feedback session request")
	}

	now := time.Now()
	session := &models.BetaFeedbackSession{
		UserID:         req.UserID,
		Email:          strings.ToLower(strings.TrimSpace(req.Email)),
		ServiceName:    req.ServiceName,
		BetaKeyPrefix:  req.BetaKeyPrefix,
		ReqID:          req.ReqID,
		Inputs:         req.Inputs,
		ActualResult:   req.ActualResult,
		CreditsCharged: req.CreditsCharged,
		Status:         models.BetaFeedbackStatusPendingFeedback,
		CreatedAt:      now,
	}

	if err := s.repo.Create(ctx, session); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to store beta feedback session")
	}

	return session, nil
}

func (s *betaFeedbackService) GetPendingSession(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSessionSummary, error) {
	session, err := s.repo.GetPendingByUserAndService(ctx, userID, serviceName)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}
	summary := session.ToSummary()
	return &summary, nil
}

func (s *betaFeedbackService) HasPendingSession(ctx context.Context, userID, serviceName string) (bool, error) {
	session, err := s.repo.GetPendingByUserAndService(ctx, userID, serviceName)
	if err != nil {
		return false, err
	}
	return session != nil, nil
}

func (s *betaFeedbackService) SubmitFeedback(ctx context.Context, userID, email, sessionID string, expected *models.BetaFeedbackExpectedResult) (*models.SubmitBetaFeedbackResponse, error) {
	if expected == nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "expectedResult is required")
	}
	if err := expected.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	objectID, err := primitive.ObjectIDFromHex(sessionID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid session id")
	}

	session, err := s.repo.GetByID(ctx, objectID)
	if err != nil {
		return nil, err
	}
	if session.UserID != userID {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "beta feedback session does not belong to this user")
	}
	if strings.ToLower(strings.TrimSpace(email)) != session.Email {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "beta feedback session does not belong to this user")
	}
	if session.Status != models.BetaFeedbackStatusPendingFeedback {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "feedback already submitted for this session")
	}
	if session.CreditsCharged <= 0 {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "session is not eligible for refund")
	}

	if err := s.checkMonthlyRefundCap(ctx, userID, session.CreditsCharged); err != nil {
		return nil, err
	}

	refundAmount := session.CreditsCharged
	if err := s.repo.SubmitFeedback(ctx, objectID, expected, refundAmount); err != nil {
		return nil, err
	}

	balance, err := s.creditsService.AddCredits(ctx, &models.AddCreditsRequest{
		UserID: userID,
		Amount: refundAmount,
	})
	if err != nil {
		return nil, err
	}

	return &models.SubmitBetaFeedbackResponse{
		Message:          "Feedback submitted and credits refunded successfully",
		SessionID:        sessionID,
		CreditsRefunded:  refundAmount,
		RemainingCredits: balance.Credits,
	}, nil
}

func (s *betaFeedbackService) ListSessions(ctx context.Context, serviceName string, limit int) ([]models.BetaFeedbackSession, error) {
	sessions, err := s.repo.List(ctx, serviceName, limit)
	if err != nil {
		return nil, err
	}

	result := make([]models.BetaFeedbackSession, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		result = append(result, session.SanitizeForAdmin())
	}
	return result, nil
}

func (s *betaFeedbackService) checkMonthlyRefundCap(ctx context.Context, userID string, refundAmount int) error {
	cap := monthlyRefundCap()
	since := time.Now().AddDate(0, -1, 0)
	total, err := s.repo.SumRefundedCreditsSince(ctx, userID, since)
	if err != nil {
		return err
	}
	if total+refundAmount > cap {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "monthly beta feedback refund limit reached")
	}
	return nil
}

func monthlyRefundCap() int {
	raw := strings.TrimSpace(os.Getenv("BETA_FEEDBACK_MONTHLY_REFUND_CAP"))
	if raw == "" {
		return defaultMonthlyRefundCap
	}
	cap, err := strconv.Atoi(raw)
	if err != nil || cap <= 0 {
		return defaultMonthlyRefundCap
	}
	return cap
}
