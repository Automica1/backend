// internal/handlers/subscription.go
package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type SubscriptionHandler struct {
	subService services.SubscriptionService
}

func NewSubscriptionHandler(subService services.SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{
		subService: subService,
	}
}

func (h *SubscriptionHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	userID := email // UserID is email in this system

	var req models.CreateSubscriptionRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.subService.CreateOrder(r.Context(), userID, email, req.PlanID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *SubscriptionHandler) VerifyPayment(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	userID := email

	var req models.VerifyPaymentRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.subService.VerifyPayment(r.Context(), userID, &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *SubscriptionHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	userID := email

	sub, err := h.subService.GetSubscriptionStatus(r.Context(), userID)
	if err != nil {
		// If simply not found, return null so the frontend shows "no subscription"
		if apperrors.IsErrorType(err, apperrors.ErrNotFound) {
			utils.SendJSONResponse(w, http.StatusOK, nil)
			return
		}
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, sub)
}

func (h *SubscriptionHandler) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	userID := email

	if err := h.subService.CancelSubscription(r.Context(), userID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"message": "Subscription cancelled successfully"})
}

func (h *SubscriptionHandler) GetAllSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := h.subService.GetAllSubscriptions(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, subs)
}

func (h *SubscriptionHandler) GetActiveCount(w http.ResponseWriter, r *http.Request) {
	count, err := h.subService.GetActiveSubscriptionCount(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *SubscriptionHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	var payload models.WebhookPayload
	if err := utils.DecodeJSONBody(r, &payload); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	signature := r.Header.Get("X-Razorpay-Signature")

	err := h.subService.HandleWebhook(r.Context(), &payload, signature)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
