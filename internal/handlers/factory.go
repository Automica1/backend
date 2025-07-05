// internal/handlers/factory.go
package handlers

import (
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
)

// HandlerFactory creates handlers with shared error mapping
type HandlerFactory struct {
	errorMapper *apperrors.APIErrorMapper
}

// NewHandlerFactory creates a new handler factory with error mapping
func NewHandlerFactory() *HandlerFactory {
	return &HandlerFactory{
		errorMapper: apperrors.NewAPIErrorMapper(),
	}
}

// CreateIDCroppingHandler creates ID cropping handler with error mapping
func (f *HandlerFactory) CreateIDCroppingHandler(creditsService services.CreditsService, userService services.UserService, idAPIService services.IDCroppingAPIService) *IDCroppingHandler {
	return &IDCroppingHandler{
		creditsService: creditsService,
		userService:    userService,
		idAPIService:   idAPIService,
		errorMapper:    f.errorMapper,
	}
}

// CreateFaceDetectionHandler creates face detection handler with error mapping
func (f *HandlerFactory) CreateFaceDetectionHandler(creditsService services.CreditsService, userService services.UserService, faceAPIService services.FaceDetectionAPIService) *FaceDetectionHandler {
	return &FaceDetectionHandler{
		creditsService: creditsService,
		userService:    userService,
		faceAPIService: faceAPIService,
		errorMapper:    f.errorMapper,
	}
}

// CreateQRMaskingHandler creates QR masking handler with error mapping
func (f *HandlerFactory) CreateQRMaskingHandler(creditsService services.CreditsService, userService services.UserService, qrAPIService services.QRMaskingAPIService) *QRMaskingHandler {
	return &QRMaskingHandler{
		creditsService: creditsService,
		userService:    userService,
		qrAPIService:   qrAPIService,
		errorMapper:    f.errorMapper,
	}
}

// CreateSignatureVerificationHandler creates signature verification handler with error mapping
func (f *HandlerFactory) CreateSignatureVerificationHandler(creditsService services.CreditsService, userService services.UserService, sigAPIService services.SignatureVerificationAPIService) *SignatureVerificationHandler {
	return &SignatureVerificationHandler{
		creditsService:      creditsService,
		userService:         userService,
		signatureAPIService: sigAPIService,
		errorMapper:         f.errorMapper,
	}
}

// AddCustomErrorMapping allows adding service-specific error mappings
func (f *HandlerFactory) AddCustomErrorMapping(technicalError string, userError apperrors.UserFriendlyError) {
	f.errorMapper.AddErrorMapping(technicalError, userError)
}

// Example of how to use the factory in your main.go
/*
func main() {
	// ... existing setup code ...
	
	// Create handler factory
	handlerFactory := handlers.NewHandlerFactory()
	
	// Add custom error mappings if needed
	handlerFactory.AddCustomErrorMapping("specific technical error", apperrors.UserFriendlyError{
		UserMessage: "Custom user message",
		TechnicalMessage: "specific technical error",
		Suggestion: "Custom suggestion",
		ErrorCode: "CUSTOM_001",
	})
	
	// Initialize handlers using factory
	handlers := &routes.Handlers{
		Health:                handlers.NewHealthHandler(),
		User:                  handlers.NewUserHandler(userService),
		Credits:               handlers.NewCreditsHandler(creditsService, userService),
		QRMasking:             handlerFactory.CreateQRMaskingHandler(creditsService, userService, qrAPIService),
		QRExtraction:          handlers.NewQRExtractionHandler(creditsService, userService, qrExtractionAPIService),
		IDCropping:            handlerFactory.CreateIDCroppingHandler(creditsService, userService, idCroppingAPIService),
		SignatureVerification: handlerFactory.CreateSignatureVerificationHandler(creditsService, userService, signatureAPIService),
		FaceDetect:            handlerFactory.CreateFaceDetectionHandler(creditsService, userService, faceDetectionAPIService),
		FaceVerify:            handlers.NewFaceVerificationHandler(creditsService, userService, faceVerificationAPIService),
	}
	
	// ... rest of setup ...
}
*/