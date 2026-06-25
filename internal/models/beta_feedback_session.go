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
	}
}

func (s *BetaFeedbackSession) SanitizeForAdmin() BetaFeedbackSession {
	sanitized := *s
	return sanitized
}
