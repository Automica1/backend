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
const betaFeedbackRollingWindowDays = 30

type BetaFeedbackService interface {
	CreateSession(ctx context.Context, req *models.CreateBetaFeedbackSessionRequest) (*models.BetaFeedbackSession, error)
	GetPendingSession(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSessionSummary, error)
	HasPendingSession(ctx context.Context, userID, serviceName string) (bool, error)
	SubmitFeedback(ctx context.Context, userID, email, sessionID string, expected *models.BetaFeedbackExpectedResult) (*models.SubmitBetaFeedbackResponse, error)
	ListSessions(ctx context.Context, serviceName string, limit int) ([]models.BetaFeedbackSession, error)
	GetSessionByID(ctx context.Context, sessionID string) (*models.BetaFeedbackSessionAdminDetail, error)
	GetUserRefundBudget(ctx context.Context, userID string) (*models.BetaFeedbackRefundBudget, error)
	SetUserRefundCapOverride(ctx context.Context, userID string, cap *int) (*models.BetaFeedbackRefundBudget, error)
	ResetUserRefundBudget(ctx context.Context, userID, confirm string) (*models.BetaFeedbackRefundBudget, error)
}

type betaFeedbackService struct {
	repo           repository.BetaFeedbackRepository
	userRepo       repository.UserRepository
	creditsService CreditsService
}

func NewBetaFeedbackService(
	repo repository.BetaFeedbackRepository,
	userRepo repository.UserRepository,
	creditsService CreditsService,
) BetaFeedbackService {
	return &betaFeedbackService{
		repo:           repo,
		userRepo:       userRepo,
		creditsService: creditsService,
	}
}

func (s *betaFeedbackService) CreateSession(ctx context.Context, req *models.CreateBetaFeedbackSessionRequest) (*models.BetaFeedbackSession, error) {
	if req == nil || req.UserID == "" || req.ServiceName == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid beta feedback session request")
	}

	if err := s.repo.SupersedePendingByUserAndService(ctx, req.UserID, req.ServiceName); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to supersede prior beta feedback sessions")
	}

	now := time.Now()
	session := &models.BetaFeedbackSession{
		UserID:         req.UserID,
		Email:          strings.ToLower(strings.TrimSpace(req.Email)),
		ServiceName:    req.ServiceName,
		BetaKeyPrefix:  req.BetaKeyPrefix,
		BetaServiceTag: req.BetaServiceTag,
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

func (s *betaFeedbackService) GetUserRefundBudget(ctx context.Context, userID string) (*models.BetaFeedbackRefundBudget, error) {
	user, err := s.userRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.buildRefundBudget(ctx, user)
}

func (s *betaFeedbackService) SetUserRefundCapOverride(ctx context.Context, userID string, cap *int) (*models.BetaFeedbackRefundBudget, error) {
	if cap != nil && *cap <= 0 {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "cap must be a positive integer")
	}
	if err := s.userRepo.UpdateBetaFeedbackRefundCapOverride(ctx, userID, cap); err != nil {
		return nil, err
	}
	return s.GetUserRefundBudget(ctx, userID)
}

func (s *betaFeedbackService) ResetUserRefundBudget(ctx context.Context, userID, confirm string) (*models.BetaFeedbackRefundBudget, error) {
	if strings.TrimSpace(confirm) != "RESET" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, `confirmation must be "RESET"`)
	}
	if err := s.userRepo.SetBetaFeedbackRefundBudgetResetAt(ctx, userID, time.Now()); err != nil {
		return nil, err
	}
	return s.GetUserRefundBudget(ctx, userID)
}

func (s *betaFeedbackService) refundableAmount(ctx context.Context, userID string, requested int) (int, error) {
	user, err := s.userRepo.GetByUserID(ctx, userID)
	if err != nil {
		return 0, err
	}

	cap := effectiveRefundCap(user)
	since := refundWindowStart(user)
	usage, err := s.repo.GetRefundUsageSince(ctx, userID, since)
	if err != nil {
		return 0, err
	}

	remaining := cap - usage.TotalCredits
	if remaining <= 0 {
		return 0, nil
	}
	if requested < remaining {
		return requested, nil
	}
	return remaining, nil
}

func (s *betaFeedbackService) buildRefundBudget(ctx context.Context, user *models.User) (*models.BetaFeedbackRefundBudget, error) {
	globalCap := monthlyRefundCap()
	effectiveCap := effectiveRefundCap(user)
	since := refundWindowStart(user)

	usage, err := s.repo.GetRefundUsageSince(ctx, user.UserID, since)
	if err != nil {
		return nil, err
	}

	remaining := effectiveCap - usage.TotalCredits
	if remaining < 0 {
		remaining = 0
	}

	var capOverride *int
	if user.BetaFeedbackMonthlyRefundCapOverride != nil {
		value := *user.BetaFeedbackMonthlyRefundCapOverride
		capOverride = &value
	}

	return &models.BetaFeedbackRefundBudget{
		UserID:              user.UserID,
		Email:               user.Email,
		GlobalCap:           globalCap,
		CapOverride:         capOverride,
		EffectiveCap:        effectiveCap,
		RollingWindowDays:   betaFeedbackRollingWindowDays,
		WindowStart:         since,
		CreditsUsed:         usage.TotalCredits,
		CreditsRemaining:    remaining,
		CapExhausted:        remaining <= 0,
		RefundSessionsCount: usage.SessionCount,
		BudgetResetAt:       user.BetaFeedbackRefundBudgetResetAt,
	}, nil
}

func effectiveRefundCap(user *models.User) int {
	if user != nil && user.BetaFeedbackMonthlyRefundCapOverride != nil && *user.BetaFeedbackMonthlyRefundCapOverride > 0 {
		return *user.BetaFeedbackMonthlyRefundCapOverride
	}
	return monthlyRefundCap()
}

func refundWindowStart(user *models.User) time.Time {
	rollingStart := time.Now().AddDate(0, 0, -betaFeedbackRollingWindowDays)
	if user == nil || user.BetaFeedbackRefundBudgetResetAt == nil {
		return rollingStart
	}
	resetAt := user.BetaFeedbackRefundBudgetResetAt.UTC()
	if resetAt.After(rollingStart) {
		return resetAt
	}
	return rollingStart
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
