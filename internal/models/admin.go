// internal/models/admin.go
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Response models for admin endpoints
type AdminUserListResponse struct {
	Message string      `json:"message"`
	Users   []AdminUser `json:"users"`
	Total   int         `json:"total"`
}

type AdminUser struct {
	ID        primitive.ObjectID `json:"id"`
	UserID    string             `json:"userId"`
	Email     string             `json:"email"`
	IsActive  bool               `json:"isActive"`
	Credits   int                `json:"credits"`
	CreatedAt time.Time          `json:"createdAt,omitempty"`
	UpdatedAt time.Time          `json:"updatedAt,omitempty"`
}

type AdminUserDetailResponse struct {
	Message string    `json:"message"`
	User    AdminUser `json:"user"`
}

type UserStatsResponse struct {
	Message      string  `json:"message"`
	TotalUsers   int64   `json:"totalUsers"`
	TotalCredits int64   `json:"totalCredits"`
	AvgCredits   float64 `json:"avgCredits"`
}

type UserActivityResponse struct {
	Message    string        `json:"message"`
	UserID     string        `json:"userId"`
	Activities []ActivityLog `json:"activities"`
}

type ActivityLog struct {
	ID          primitive.ObjectID     `bson:"_id,omitempty" json:"id,omitempty"`
	UserID      string                 `bson:"userId" json:"userId"`
	Action      string                 `bson:"action" json:"action"`
	Description string                 `bson:"description" json:"description"`
	Timestamp   time.Time              `bson:"timestamp" json:"timestamp"`
	Metadata    map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
}

type UserCreditsResponse struct {
	Message string `json:"message"`
	UserID  string `json:"userId"`
	Credits int    `json:"credits"`
}

type AdminListQuery struct {
	Limit  int    `json:"limit"`
	Skip   int    `json:"skip"`
	Search string `json:"search"`
	Status string `json:"status,omitempty"`
}

type AdminSubscription struct {
	ID                 primitive.ObjectID `json:"id"`
	UserID             string             `json:"userId"`
	Email              string             `json:"email"`
	PlanID             string             `json:"planId"`
	PlanName           string             `json:"planName,omitempty"`
	PlanRazorpayID     string             `json:"planRazorpayId,omitempty"`
	SubscriptionID     string             `json:"subscriptionId"`
	Status             SubscriptionStatus `json:"status"`
	Amount             int                `json:"amount"`
	Currency           string             `json:"currency"`
	CurrentPeriodStart time.Time          `json:"currentPeriodStart"`
	CurrentPeriodEnd   time.Time          `json:"currentPeriodEnd"`
	GracePeriodEnd     *time.Time         `json:"gracePeriodEnd,omitempty"`
	CancelAtCycleEnd   bool               `json:"cancelAtCycleEnd,omitempty"`
	CancelScheduledAt  *time.Time         `json:"cancelScheduledAt,omitempty"`
	CancelledAt        *time.Time         `json:"cancelledAt,omitempty"`
	PendingPlanID      string             `json:"pendingPlanId,omitempty"`
	PlanChangeDate     *time.Time         `json:"planChangeDate,omitempty"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

type AdminSubscriptionListResponse struct {
	Message       string              `json:"message"`
	Subscriptions []AdminSubscription `json:"subscriptions"`
	Total         int64               `json:"total"`
	Limit         int                 `json:"limit"`
	Skip          int                 `json:"skip"`
}

type AdminSubscriptionDetailResponse struct {
	Message      string            `json:"message"`
	Subscription AdminSubscription `json:"subscription"`
}

type AdminSubscriptionTestResetCapabilitiesResponse struct {
	Message   string `json:"message"`
	Enabled   bool   `json:"enabled"`
	Reason    string `json:"reason,omitempty"`
}

type AdminSubscriptionTestResetRequest struct {
	Confirm string `json:"confirm"`
}

type AdminSubscriptionTestResetResponse struct {
	Message                string   `json:"message"`
	UserID                 string   `json:"userId"`
	Email                  string   `json:"email"`
	RazorpayCancelled      []string `json:"razorpayCancelled"`
	LocalRecordsDeleted    int64    `json:"localRecordsDeleted"`
	BillingCurrencyCleared bool     `json:"billingCurrencyCleared"`
}

type AdminSubscriptionQuery struct {
	Limit          int    `json:"limit"`
	Skip           int    `json:"skip"`
	Search         string `json:"search"`
	Status         string `json:"status,omitempty"`
	UserID         string `json:"userId,omitempty"`
	Email          string `json:"email,omitempty"`
	PlanID         string `json:"planId,omitempty"`
	SubscriptionID string `json:"subscriptionId,omitempty"`
}

type AdminAuditLog struct {
	ID         primitive.ObjectID     `bson:"_id,omitempty" json:"id,omitempty"`
	ActorEmail string                 `bson:"actorEmail" json:"actorEmail"`
	ActorID    string                 `bson:"actorId,omitempty" json:"actorId,omitempty"`
	Action     string                 `bson:"action" json:"action"`
	TargetType string                 `bson:"targetType" json:"targetType"`
	TargetID   string                 `bson:"targetId,omitempty" json:"targetId,omitempty"`
	Outcome    string                 `bson:"outcome" json:"outcome"`
	Reason     string                 `bson:"reason,omitempty" json:"reason,omitempty"`
	Metadata   map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
	Timestamp  time.Time              `bson:"timestamp" json:"timestamp"`
}

type AdminAuditLogListResponse struct {
	Message string          `json:"message"`
	Logs    []AdminAuditLog `json:"logs"`
	Count   int             `json:"count"`
}

type AdminLogEntry struct {
	ID            string                 `json:"id"`
	Category      string                 `json:"category"`
	Kind          string                 `json:"kind,omitempty"`
	Source        string                 `json:"source"`
	Timestamp     time.Time              `json:"timestamp"`
	Level         string                 `json:"level,omitempty"`
	Message       string                 `json:"message"`
	RequestID     string                 `json:"requestId,omitempty"`
	Method        string                 `json:"method,omitempty"`
	Path          string                 `json:"path,omitempty"`
	Route         string                 `json:"route,omitempty"`
	Status        int                    `json:"status,omitempty"`
	Bytes         int                    `json:"bytes,omitempty"`
	DurationMS    int64                  `json:"durationMs,omitempty"`
	RemoteIP      string                 `json:"remoteIp,omitempty"`
	ClientIP      string                 `json:"clientIp,omitempty"`
	Referer       string                 `json:"referer,omitempty"`
	Host          string                 `json:"host,omitempty"`
	Upstream      string                 `json:"upstream,omitempty"`
	UserAgent     string                 `json:"userAgent,omitempty"`
	Email         string                 `json:"email,omitempty"`
	IsAdmin       bool                   `json:"isAdmin,omitempty"`
	ActorEmail    string                 `json:"actorEmail,omitempty"`
	ActorID       string                 `json:"actorId,omitempty"`
	Action        string                 `json:"action,omitempty"`
	TargetType    string                 `json:"targetType,omitempty"`
	TargetID      string                 `json:"targetId,omitempty"`
	Outcome       string                 `json:"outcome,omitempty"`
	Reason        string                 `json:"reason,omitempty"`
	UserID        string                 `json:"userId,omitempty"`
	ServiceName   string                 `json:"serviceName,omitempty"`
	Endpoint      string                 `json:"endpoint,omitempty"`
	AuthMethod    string                 `json:"authMethod,omitempty"`
	CreditsUsed   int                    `json:"creditsUsed,omitempty"`
	Success       bool                   `json:"success,omitempty"`
	ProcessTimeMS int64                  `json:"processTimeMs,omitempty"`
	IPAddress     string                 `json:"ipAddress,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type AdminLogListResponse struct {
	Message string          `json:"message"`
	Source  string          `json:"source"`
	Logs    []AdminLogEntry `json:"logs"`
	Total   int64           `json:"total"`
	Limit   int             `json:"limit"`
	Skip    int             `json:"skip"`
}

type AdminLogQuery struct {
	Source    string
	Kind      string
	Search    string
	Level     string
	Email     string
	UserID    string
	Route     string
	RequestID string
	Status    string
	Target    string
	Limit     int
	Skip      int
	StartDate *time.Time
	EndDate   *time.Time
}

type AdminSearchResponse struct {
	Message       string              `json:"message"`
	Query         string              `json:"query"`
	Users         []AdminUser         `json:"users"`
	Tokens        []*CreditToken      `json:"tokens"`
	Plans         []Plan              `json:"plans"`
	Usage         []UsageStats        `json:"usage"`
	Subscriptions []AdminSubscription `json:"subscriptions"`
}

type AdminSummaryResponse struct {
	Message           string    `json:"message"`
	GeneratedAt       time.Time `json:"generatedAt"`
	TotalUsers        int       `json:"totalUsers"`
	ActiveUsers       int       `json:"activeUsers"`
	TotalTokens       int       `json:"totalTokens"`
	UsedTokens        int       `json:"usedTokens"`
	TotalPlans        int       `json:"totalPlans"`
	ActivePlans       int       `json:"activePlans"`
	RecentAuditCount  int       `json:"recentAuditCount"`
	MostUsedService   string    `json:"mostUsedService"`
	MostUsedCalls     int       `json:"mostUsedCalls"`
	TotalUsageCredits int64     `json:"totalUsageCredits"`
}

// Note: CreditsResponse and RegisterUserResponse should be in response.go, not here
