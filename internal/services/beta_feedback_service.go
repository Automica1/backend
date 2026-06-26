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
	GetSessionByID(ctx context.Context, sessionID string) (*models.BetaFeedbackSessionAdminDetail, error)
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
		RunOutcome:     req.RunOutcome,
		FailureMessage: req.FailureMessage,
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

	refundAmount, err := s.refundableAmount(ctx, userID, session.CreditsCharged)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SubmitFeedback(ctx, objectID, expected, refundAmount); err != nil {
		return nil, err
	}

	var remainingCredits int
	var message string
	if refundAmount > 0 {
		balance, err := s.creditsService.AddCredits(ctx, &models.AddCreditsRequest{
			UserID: userID,
			Amount: refundAmount,
		})
		if err != nil {
			return nil, err
		}
		remainingCredits = balance.Credits
		message = "Feedback submitted and credits refunded successfully"
	} else {
		balance, err := s.creditsService.GetBalance(ctx, userID)
		if err != nil {
			return nil, err
		}
		remainingCredits = balance.Credits
		message = "Feedback submitted successfully"
	}

	return &models.SubmitBetaFeedbackResponse{
		Message:          message,
		SessionID:        sessionID,
		CreditsRefunded:  refundAmount,
		RemainingCredits: remainingCredits,
	}, nil
}

func (s *betaFeedbackService) GetSessionByID(ctx context.Context, sessionID string) (*models.BetaFeedbackSessionAdminDetail, error) {
	objectID, err := primitive.ObjectIDFromHex(sessionID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid session id")
	}

	session, err := s.repo.GetByID(ctx, objectID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta feedback session not found")
	}

	detail := session.ToAdminDetail()
	return &detail, nil
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

func (s *betaFeedbackService) refundableAmount(ctx context.Context, userID string, requested int) (int, error) {
	cap := monthlyRefundCap()
	since := time.Now().AddDate(0, -1, 0)
	total, err := s.repo.SumRefundedCreditsSince(ctx, userID, since)
	if err != nil {
		return 0, err
	}
	remaining := cap - total
	if remaining <= 0 {
		return 0, nil
	}
	if requested < remaining {
		return requested, nil
	}
	return remaining, nil
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
