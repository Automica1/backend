package handlers

import (
	"context"
	"net/http"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
)

// ServiceBillingContext holds resolved identity for credit deduction.
type ServiceBillingContext struct {
	UserID      string
	Email       string
	AuthMethod  string
	IsGuestPass bool
	GuestPass   *models.GuestPass
}

func ResolveServiceBilling(r *http.Request, serviceSlug string) (ServiceBillingContext, error) {
	if guestPass, ok := middleware.GetGuestPassFromContext(r.Context()); ok {
		if serviceSlug != "" && !guestPass.AllowsService(serviceSlug) {
			return ServiceBillingContext{}, apperrors.NewAppError(
				apperrors.ErrForbidden,
				http.StatusForbidden,
				"guest pass is not valid for this service",
			)
		}
		return ServiceBillingContext{
			UserID:      guestPass.WalletUserID,
			Email:       guestPass.WalletUserID,
			AuthMethod:  "guest_pass",
			IsGuestPass: true,
			GuestPass:   guestPass,
		}, nil
	}

	email, ok := r.Context().Value("email").(string)
	if !ok || email == "" {
		return ServiceBillingContext{}, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		)
	}

	authMethod := "bearer_token"
	if _, isAPIKeyAuth := middleware.GetAPIKeyFromContext(r.Context()); isAPIKeyAuth {
		authMethod = "api_key"
	}

	return ServiceBillingContext{
		UserID:     email,
		Email:      email,
		AuthMethod: authMethod,
	}, nil
}

// ResolveServiceUser returns the billing user ID, auto-creating Kinde users when needed.
// Guest passes skip user table lookup entirely.
func ResolveServiceUser(ctx context.Context, userService services.UserService, billing ServiceBillingContext) (string, error) {
	if billing.IsGuestPass {
		return billing.UserID, nil
	}

	user, err := userService.GetUserByEmail(ctx, billing.Email)
	if err != nil {
		if apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
			registerReq := &models.RegisterUserRequest{
				UserID: billing.Email,
				Email:  billing.Email,
			}
			createdUser, createErr := userService.RegisterUser(ctx, registerReq)
			if createErr != nil {
				return "", apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to auto-create user: "+createErr.Error(),
				)
			}
			return createdUser.User.UserID, nil
		}
		return "", err
	}
	return user.UserID, nil
}
