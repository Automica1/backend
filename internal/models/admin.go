// internal/models/admin.go
package models

import (
	"time"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Response models for admin endpoints
type AdminUserListResponse struct {
	Message string        `json:"message"`
	Users   []AdminUser   `json:"users"`
	Total   int           `json:"total"`
}

type AdminUser struct {
	ID        primitive.ObjectID `json:"id"`
	UserID    string             `json:"userId"`
	Email     string             `json:"email"`
	Credits   int                `json:"credits"`
	CreatedAt time.Time          `json:"createdAt,omitempty"`
	UpdatedAt time.Time          `json:"updatedAt,omitempty"`
}

type AdminUserDetailResponse struct {
	Message string    `json:"message"`
	User    AdminUser `json:"user"`
}

type UserStatsResponse struct {
	Message      string `json:"message"`
	TotalUsers   int64  `json:"totalUsers"`
	TotalCredits int64  `json:"totalCredits"`
	AvgCredits   float64 `json:"avgCredits"`
}

type UserActivityResponse struct {
	Message   string         `json:"message"`
	UserID    string         `json:"userId"`
	Activities []ActivityLog `json:"activities"`
}

type ActivityLog struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	UserID      string             `bson:"userId" json:"userId"`
	Action      string             `bson:"action" json:"action"`
	Description string             `bson:"description" json:"description"`
	Timestamp   time.Time          `bson:"timestamp" json:"timestamp"`
	Metadata    map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
}

type UserCreditsResponse struct {
	Message string `json:"message"`
	UserID  string `json:"userId"`
	Credits int    `json:"credits"`
}

// Note: CreditsResponse and RegisterUserResponse should be in response.go, not here