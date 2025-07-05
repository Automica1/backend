// pkg/errors/errors.go
package apperrors

import (
	"errors"
	"fmt"
	"strings"
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

// AppError represents a custom application error with user-friendly messaging
type AppError struct {
	Type             string `json:"type"`
	StatusCode       int    `json:"status_code"`
	Message          string `json:"message"`
	Details          string `json:"details,omitempty"`
	UserMessage      string `json:"user_message,omitempty"`
	TechnicalMessage string `json:"technical_message,omitempty"`
	Suggestion       string `json:"suggestion,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
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

// =============================================================================
// API Error Mapping System
// =============================================================================

// UserFriendlyError represents a user-friendly error mapping
type UserFriendlyError struct {
	UserMessage      string `json:"user_message"`
	TechnicalMessage string `json:"technical_message"`
	Suggestion       string `json:"suggestion"`
	ErrorCode        string `json:"error_code"`
}

// APIErrorMapper maps technical error messages to user-friendly messages
type APIErrorMapper struct {
	errorMappings map[string]UserFriendlyError
}

// NewAPIErrorMapper creates a new error mapper with predefined mappings
func NewAPIErrorMapper() *APIErrorMapper {
	return &APIErrorMapper{
		errorMappings: map[string]UserFriendlyError{
			// ID Cropping specific errors
			"low confidence in keypoints": {
				UserMessage:      "Please upload a different image",
				TechnicalMessage: "Low confidence in keypoints",
				Suggestion:       "Try uploading a clearer image with better lighting and ensure the ID document is fully visible",
				ErrorCode:        "ID_CROP_001",
			},
			"image too blurry": {
				UserMessage:      "Image quality is too low",
				TechnicalMessage: "Image too blurry",
				Suggestion:       "Please upload a sharper, clearer image of your ID document",
				ErrorCode:        "ID_CROP_002",
			},
			"document not detected": {
				UserMessage:      "ID document not found in image",
				TechnicalMessage: "Document not detected",
				Suggestion:       "Please ensure the entire ID document is visible in the image",
				ErrorCode:        "ID_CROP_003",
			},
			"invalid image format": {
				UserMessage:      "Unsupported image format",
				TechnicalMessage: "Invalid image format",
				Suggestion:       "Please upload a valid image file (JPG, PNG, etc.)",
				ErrorCode:        "ID_CROP_004",
			},
			
			// Face Detection specific errors
			"no face detected": {
				UserMessage:      "No face found in the image",
				TechnicalMessage: "No face detected",
				Suggestion:       "Please ensure your face is clearly visible in the image",
				ErrorCode:        "FACE_DET_001",
			},
			"multiple faces detected": {
				UserMessage:      "Multiple faces detected",
				TechnicalMessage: "Multiple faces detected",
				Suggestion:       "Please upload an image with only one face",
				ErrorCode:        "FACE_DET_002",
			},
			"face too small": {
				UserMessage:      "Face is too small in the image",
				TechnicalMessage: "Face too small",
				Suggestion:       "Please upload an image where your face takes up more of the frame",
				ErrorCode:        "FACE_DET_003",
			},
			
			// QR Code specific errors
			"qr code not found": {
				UserMessage:      "QR code not detected",
				TechnicalMessage: "QR code not found",
				Suggestion:       "Please ensure the QR code is clearly visible and not damaged",
				ErrorCode:        "QR_001",
			},
			"qr code damaged": {
				UserMessage:      "QR code appears to be damaged",
				TechnicalMessage: "QR code damaged",
				Suggestion:       "Please upload an image with a clear, undamaged QR code",
				ErrorCode:        "QR_002",
			},
			
			// Signature Verification specific errors
			"signature not clear": {
				UserMessage:      "Signature is not clear enough",
				TechnicalMessage: "Signature not clear",
				Suggestion:       "Please upload a clearer image of the signature",
				ErrorCode:        "SIG_001",
			},
			"no signature found": {
				UserMessage:      "No signature detected in the image",
				TechnicalMessage: "No signature found",
				Suggestion:       "Please ensure the signature is clearly visible in the image",
				ErrorCode:        "SIG_002",
			},
			
			// Generic/Common errors
			"processing failed": {
				UserMessage:      "Processing failed",
				TechnicalMessage: "Processing failed",
				Suggestion:       "Please try again with a different image",
				ErrorCode:        "GEN_001",
			},
			"timeout": {
				UserMessage:      "Request timed out",
				TechnicalMessage: "Timeout",
				Suggestion:       "Please try again later",
				ErrorCode:        "GEN_002",
			},
			"server error": {
				UserMessage:      "Service temporarily unavailable",
				TechnicalMessage: "Server error",
				Suggestion:       "Please try again later",
				ErrorCode:        "GEN_003",
			},
		},
	}
}

// MapError maps a technical error message to a user-friendly error
func (m *APIErrorMapper) MapError(technicalError string) UserFriendlyError {
	// Convert to lowercase for case-insensitive matching
	lowerError := strings.ToLower(strings.TrimSpace(technicalError))
	
	// Try exact match first
	if userError, exists := m.errorMappings[lowerError]; exists {
		return userError
	}
	
	// Try partial matches for more flexible error handling
	for key, userError := range m.errorMappings {
		if strings.Contains(lowerError, key) {
			return userError
		}
	}
	
	// If no mapping found, return a generic error
	return UserFriendlyError{
		UserMessage:      "Processing failed",
		TechnicalMessage: technicalError,
		Suggestion:       "Please try again with a different image or contact support if the issue persists",
		ErrorCode:        "GEN_UNKNOWN",
	}
}

// AddErrorMapping allows adding custom error mappings
func (m *APIErrorMapper) AddErrorMapping(technicalError string, userError UserFriendlyError) {
	m.errorMappings[strings.ToLower(technicalError)] = userError
}

// NewAPIError creates a new AppError with user-friendly messaging
func NewAPIError(mapper *APIErrorMapper, technicalError string) *AppError {
	userError := mapper.MapError(technicalError)
	
	return &AppError{
		Type:             ErrBadRequest,
		StatusCode:       400,
		Message:          userError.UserMessage,
		Details:          userError.TechnicalMessage,
		UserMessage:      userError.UserMessage,
		TechnicalMessage: userError.TechnicalMessage,
		Suggestion:       userError.Suggestion,
		ErrorCode:        userError.ErrorCode,
	}
}