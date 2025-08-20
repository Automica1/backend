// 1. First, create the usage model
// internal/models/usage.go
package models

import (
	"time"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ServiceUsage represents a single service usage record
type ServiceUsage struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      string             `bson:"user_id" json:"user_id"`
	Email       string             `bson:"email" json:"email"`
	ServiceName string             `bson:"service_name" json:"service_name"`
	Endpoint    string             `bson:"endpoint" json:"endpoint"`
	Method      string             `bson:"method" json:"method"`
	Success     bool               `bson:"success" json:"success"`
	ErrorMsg    string             `bson:"error_msg,omitempty" json:"error_msg,omitempty"`
	CreditsUsed int                `bson:"credits_used" json:"credits_used"`
	RequestID   string             `bson:"request_id,omitempty" json:"request_id,omitempty"`
	IPAddress   string             `bson:"ip_address,omitempty" json:"ip_address,omitempty"`
	UserAgent   string             `bson:"user_agent,omitempty" json:"user_agent,omitempty"`
	AuthMethod  string             `bson:"auth_method" json:"auth_method"` // "bearer" or "api_key"
	ProcessTime int64              `bson:"process_time_ms" json:"process_time_ms"` // Processing time in milliseconds
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
}

// UsageStats represents aggregated usage statistics
type UsageStats struct {
	ServiceName  string `bson:"_id" json:"service_name"`
	TotalCalls   int    `bson:"total_calls" json:"total_calls"`
	SuccessCalls int    `bson:"success_calls" json:"success_calls"`
	FailedCalls  int    `bson:"failed_calls" json:"failed_calls"`
	TotalCredits int    `bson:"total_credits" json:"total_credits"`
}

// UserUsageStats represents per-user usage statistics
type UserUsageStats struct {
	UserID       string `bson:"_id" json:"user_id"`
	Email        string `bson:"email" json:"email"`
	TotalCalls   int    `bson:"total_calls" json:"total_calls"`
	SuccessCalls int    `bson:"success_calls" json:"success_calls"`
	FailedCalls  int    `bson:"failed_calls" json:"failed_calls"`
	TotalCredits int    `bson:"total_credits" json:"total_credits"`
}

// ServiceUserStats represents service usage by specific user
type ServiceUserStats struct {
	UserID       string `bson:"user_id" json:"user_id"`
	Email        string `bson:"email" json:"email"`
	ServiceName  string `bson:"service_name" json:"service_name"`
	TotalCalls   int    `bson:"total_calls" json:"total_calls"`
	SuccessCalls int    `bson:"success_calls" json:"success_calls"`
	FailedCalls  int    `bson:"failed_calls" json:"failed_calls"`
	TotalCredits int    `bson:"total_credits" json:"total_credits"`
	LastUsed     time.Time `bson:"last_used" json:"last_used"`
}

// UsageTrackingRequest for recording usage
type UsageTrackingRequest struct {
	UserID      string
	Email       string
	ServiceName string
	Endpoint    string
	Method      string
	Success     bool
	ErrorMsg    string
	CreditsUsed int
	RequestID   string
	IPAddress   string
	UserAgent   string
	AuthMethod  string
	ProcessTime int64
}