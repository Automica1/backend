// pkg/errors/errors.go
package apperrors

import (
	"errors"
	"fmt"
)

// Error types
const (
	ErrValidation          = "VALIDATION_ERROR"
	ErrNotFound            = "NOT_FOUND"
	ErrUserNotFound        = "USER_NOT_FOUND"
	ErrUserAlreadyExists   = "USER_ALREADY_EXISTS"
	ErrCreditsNotFound     = "CREDITS_NOT_FOUND"
	ErrInsufficientCredits = "INSUFFICIENT_CREDITS"
	ErrUnauthorized        = "UNAUTHORIZED"
	ErrForbidden           = "FORBIDDEN"
	ErrConflict            = "CONFLICT"
	ErrInternalServer      = "INTERNAL_SERVER_ERROR"
	ErrBadRequest          = "BAD_REQUEST"
)

// AppError represents a custom application error
type AppError struct {
	Type       string `json:"type"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Details    string `json:"details,omitempty"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s - %s", e.Type, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// NewAppError creates a new AppError
func NewAppError(errorType string, statusCode int, message string, details ...string) *AppError {
	var detail string
	if len(details) > 0 {
		detail = details[0]
	}
	
	return &AppError{
		Type:       errorType,
		StatusCode: statusCode,
		Message:    message,
		Details:    detail,
	}
}

// IsErrorType checks if an error is of a specific type
func IsErrorType(err error, errorType string) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Type == errorType
	}
	return false
}

// GetErrorType extracts the error type from an error
func GetErrorType(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Type
	}
	return ""
}

// GetStatusCode extracts the status code from an error
func GetStatusCode(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.StatusCode
	}
	return 500 // Default to internal server error
}

// GetHTTPStatusCode is an alias for GetStatusCode for backward compatibility
func GetHTTPStatusCode(err error) int {
	return GetStatusCode(err)
}

// Helper functions to create common errors
func NewUserNotFoundError() *AppError {
	return NewAppError(ErrUserNotFound, 404, "User not found")
}

func NewUserAlreadyExistsError() *AppError {
	return NewAppError(ErrUserAlreadyExists, 409, "User already exists")
}

func NewCreditsNotFoundError() *AppError {
	return NewAppError(ErrCreditsNotFound, 404, "Credits record not found")
}

func NewInsufficientCreditsError() *AppError {
	return NewAppError(ErrInsufficientCredits, 400, "Insufficient credits")
}