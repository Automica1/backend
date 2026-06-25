package models

import (
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	BetaFeedbackStatusPendingFeedback = "pending_feedback"
	BetaFeedbackStatusRefunded        = "refunded"

	BetaFeedbackRunOutcomeCompleted = "completed"
	BetaFeedbackRunOutcomeFailed    = "failed"
)

var validExpectedClassifications = map[string]bool{
	"match":     true,
	"no_match":  true,
	"uncertain": true,
}

type BetaFeedbackExpectedResult struct {
	ExpectedClassification string  `json:"expectedClassification" bson:"expectedClassification"`
	ExpectedSimilarityMin  *float64 `json:"expectedSimilarityMin,omitempty" bson:"expectedSimilarityMin,omitempty"`
	ExpectedSimilarityMax  *float64 `json:"expectedSimilarityMax,omitempty" bson:"expectedSimilarityMax,omitempty"`
	Notes                  string  `json:"notes,omitempty" bson:"notes,omitempty"`
}

func (e *BetaFeedbackExpectedResult) Validate() error {
	e.ExpectedClassification = strings.TrimSpace(strings.ToLower(e.ExpectedClassification))
	e.Notes = strings.TrimSpace(e.Notes)

	if e.ExpectedClassification == "" {
		return errors.New("expectedClassification is required")
	}
	if !validExpectedClassifications[e.ExpectedClassification] {
		return errors.New("expectedClassification must be match, no_match, or uncertain")
	}
	if e.ExpectedSimilarityMin != nil && (*e.ExpectedSimilarityMin < 0 || *e.ExpectedSimilarityMin > 100) {
		return errors.New("expectedSimilarityMin must be between 0 and 100")
	}
	if e.ExpectedSimilarityMax != nil && (*e.ExpectedSimilarityMax < 0 || *e.ExpectedSimilarityMax > 100) {
		return errors.New("expectedSimilarityMax must be between 0 and 100")
	}
	if e.ExpectedSimilarityMin != nil && e.ExpectedSimilarityMax != nil && *e.ExpectedSimilarityMin > *e.ExpectedSimilarityMax {
		return errors.New("expectedSimilarityMin cannot exceed expectedSimilarityMax")
	}
	return nil
}

type BetaFeedbackActualResult struct {
	SimilarityPercentage float64 `json:"similarity_percentage,omitempty" bson:"similarity_percentage,omitempty"`
	Classification       string  `json:"classification,omitempty" bson:"classification,omitempty"`
}

type BetaFeedbackSession struct {
	ID                primitive.ObjectID          `bson:"_id,omitempty" json:"id,omitempty"`
	UserID            string                      `bson:"userId" json:"userId"`
	Email             string                      `bson:"email" json:"email"`
	ServiceName       string                      `bson:"serviceName" json:"serviceName"`
	BetaKeyPrefix     string                      `bson:"betaKeyPrefix,omitempty" json:"betaKeyPrefix,omitempty"`
	ReqID             string                      `bson:"reqId" json:"reqId"`
	Inputs            []string                    `bson:"inputs" json:"-"`
	ActualResult      *BetaFeedbackActualResult     `bson:"actualResult,omitempty" json:"actualResult,omitempty"`
	ExpectedResult    *BetaFeedbackExpectedResult `bson:"expectedResult,omitempty" json:"expectedResult,omitempty"`
	CreditsCharged    int                         `bson:"creditsCharged" json:"creditsCharged"`
	CreditsRefunded   int                         `bson:"creditsRefunded" json:"creditsRefunded"`
	Status            string                      `bson:"status" json:"status"`
	RunOutcome        string                      `bson:"runOutcome,omitempty" json:"runOutcome,omitempty"`
	FailureMessage    string                      `bson:"failureMessage,omitempty" json:"failureMessage,omitempty"`
	CreatedAt         time.Time                   `bson:"createdAt" json:"createdAt"`
	FeedbackSubmittedAt *time.Time                `bson:"feedbackSubmittedAt,omitempty" json:"feedbackSubmittedAt,omitempty"`
	RefundedAt        *time.Time                  `bson:"refundedAt,omitempty" json:"refundedAt,omitempty"`
}

type CreateBetaFeedbackSessionRequest struct {
	UserID         string
	Email          string
	ServiceName    string
	BetaKeyPrefix  string
	ReqID          string
	Inputs         []string
	ActualResult   *BetaFeedbackActualResult
	CreditsCharged int
	RunOutcome     string
	FailureMessage string
}

type SubmitBetaFeedbackRequest struct {
	ExpectedResult BetaFeedbackExpectedResult `json:"expectedResult"`
}

type BetaFeedbackPendingResponse struct {
	Message string               `json:"message"`
	Session *BetaFeedbackSessionSummary `json:"session,omitempty"`
}

type BetaFeedbackSessionSummary struct {
	ID             string                      `json:"id"`
	ServiceName    string                      `json:"serviceName"`
	ReqID          string                      `json:"reqId"`
	CreditsCharged int                         `json:"creditsCharged"`
	ActualResult   *BetaFeedbackActualResult   `json:"actualResult,omitempty"`
	CreatedAt      time.Time                   `json:"createdAt"`
	InputCount     int                         `json:"inputCount"`
	RunOutcome     string                      `json:"runOutcome,omitempty"`
	FailureMessage string                      `json:"failureMessage,omitempty"`
}

type BetaFeedbackSessionAdminDetail struct {
	ID                  string                      `json:"id"`
	UserID              string                      `json:"userId"`
	Email               string                      `json:"email"`
	ServiceName         string                      `json:"serviceName"`
	BetaKeyPrefix       string                      `json:"betaKeyPrefix,omitempty"`
	ReqID               string                      `json:"reqId"`
	Inputs              []string                    `json:"inputs"`
	ActualResult        *BetaFeedbackActualResult   `json:"actualResult,omitempty"`
	ExpectedResult      *BetaFeedbackExpectedResult `json:"expectedResult,omitempty"`
	CreditsCharged      int                         `json:"creditsCharged"`
	CreditsRefunded     int                         `json:"creditsRefunded"`
	Status              string                      `json:"status"`
	RunOutcome          string                      `json:"runOutcome,omitempty"`
	FailureMessage      string                      `json:"failureMessage,omitempty"`
	CreatedAt           time.Time                   `json:"createdAt"`
	FeedbackSubmittedAt *time.Time                  `json:"feedbackSubmittedAt,omitempty"`
	RefundedAt          *time.Time                  `json:"refundedAt,omitempty"`
}

type BetaFeedbackSessionDetailResponse struct {
	Message string                       `json:"message"`
	Session BetaFeedbackSessionAdminDetail `json:"session"`
}

type SubmitBetaFeedbackResponse struct {
	Message          string `json:"message"`
	SessionID        string `json:"sessionId"`
	CreditsRefunded  int    `json:"creditsRefunded"`
	RemainingCredits int    `json:"remainingCredits"`
}

type BetaFeedbackSessionListResponse struct {
	Message string                `json:"message"`
	Sessions []BetaFeedbackSession `json:"sessions"`
	Total   int                   `json:"total"`
}

func (s *BetaFeedbackSession) ToSummary() BetaFeedbackSessionSummary {
	return BetaFeedbackSessionSummary{
		ID:             s.ID.Hex(),
		ServiceName:    s.ServiceName,
		ReqID:          s.ReqID,
		CreditsCharged: s.CreditsCharged,
		ActualResult:   s.ActualResult,
		CreatedAt:      s.CreatedAt,
		InputCount:     len(s.Inputs),
		RunOutcome:     s.RunOutcome,
		FailureMessage: s.FailureMessage,
	}
}

func (s *BetaFeedbackSession) SanitizeForAdmin() BetaFeedbackSession {
	sanitized := *s
	sanitized.Inputs = nil
	return sanitized
}

func (s *BetaFeedbackSession) ToAdminDetail() BetaFeedbackSessionAdminDetail {
	return BetaFeedbackSessionAdminDetail{
		ID:                  s.ID.Hex(),
		UserID:              s.UserID,
		Email:               s.Email,
		ServiceName:         s.ServiceName,
		BetaKeyPrefix:       s.BetaKeyPrefix,
		ReqID:               s.ReqID,
		Inputs:              s.Inputs,
		ActualResult:        s.ActualResult,
		ExpectedResult:      s.ExpectedResult,
		CreditsCharged:      s.CreditsCharged,
		CreditsRefunded:     s.CreditsRefunded,
		Status:              s.Status,
		RunOutcome:          s.RunOutcome,
		FailureMessage:      s.FailureMessage,
		CreatedAt:           s.CreatedAt,
		FeedbackSubmittedAt: s.FeedbackSubmittedAt,
		RefundedAt:          s.RefundedAt,
	}
}
