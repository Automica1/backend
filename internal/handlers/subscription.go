// internal/handlers/subscription.go
package handlers

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type SubscriptionHandler struct {
	subService   services.SubscriptionService
	adminService services.AdminService
}

func NewSubscriptionHandler(subService services.SubscriptionService, adminService services.AdminService) *SubscriptionHandler {
	return &SubscriptionHandler{
		subService:   subService,
		adminService: adminService,
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

	// If frontend provided customer details, prefer them; otherwise keep email
	name := req.Name
	contact := req.Contact
	if req.Email == "" {
		req.Email = email
	}

	response, err := h.subService.CreateOrder(r.Context(), userID, req.Email, name, contact, req.PlanID, req.Currency)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// If we created a customer or subscription id, attach any returned fields (service handles customer creation)
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

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"message": "Subscription cancellation scheduled successfully"})
}

func (h *SubscriptionHandler) CalculateUpgradePrice(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	planID := r.URL.Query().Get("planId")
	if planID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, 400, "planId is required", ""))
		return
	}

	price, currency, err := h.subService.CalculateUpgradePrice(r.Context(), email, planID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{"price": price, "currency": currency})
}

func (h *SubscriptionHandler) CreateUpgradeOrder(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	var req models.CreateSubscriptionRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.subService.CreateUpgradeOrder(r.Context(), email, req.PlanID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *SubscriptionHandler) DowngradeSubscription(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "email not found", ""))
		return
	}

	var req models.CreateSubscriptionRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if err := h.subService.DowngradeSubscription(r.Context(), email, req.PlanID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"message": "Downgrade scheduled for end of cycle"})
}

func (h *SubscriptionHandler) GetAllSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := h.subService.GetAllSubscriptions(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, subs)
}

func (h *SubscriptionHandler) GetAdminSubscriptions(w http.ResponseWriter, r *http.Request) {
	query := models.AdminSubscriptionQuery{
		Search:         r.URL.Query().Get("search"),
		Status:         r.URL.Query().Get("status"),
		UserID:         r.URL.Query().Get("userId"),
		Email:          r.URL.Query().Get("email"),
		PlanID:         r.URL.Query().Get("planId"),
		SubscriptionID: r.URL.Query().Get("subscriptionId"),
	}

	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			query.Limit = parsed
		}
	}
	if value := r.URL.Query().Get("skip"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			query.Skip = parsed
		}
	}

	response, err := h.subService.ListAdminSubscriptions(r.Context(), query)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *SubscriptionHandler) GetAdminSubscription(w http.ResponseWriter, r *http.Request) {
	subscriptionID := chi.URLParam(r, "subscriptionId")
	if subscriptionID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, 400, "subscriptionId is required", ""))
		return
	}

	response, err := h.subService.GetAdminSubscription(r.Context(), subscriptionID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *SubscriptionHandler) ReconcileAdminSubscription(w http.ResponseWriter, r *http.Request) {
	subscriptionID := chi.URLParam(r, "subscriptionId")
	if subscriptionID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, 400, "subscriptionId is required", ""))
		return
	}

	response, err := h.subService.ReconcileAdminSubscription(r.Context(), subscriptionID)
	if err != nil {
		h.recordAdminSubscriptionAction(r, "subscription_reconcile", subscriptionID, "failed", map[string]interface{}{
			"error": err.Error(),
		})
		utils.SendErrorResponse(w, err)
		return
	}

	h.recordAdminSubscriptionAction(r, "subscription_reconcile", subscriptionID, "success", map[string]interface{}{
		"status": response.Subscription.Status,
	})

	utils.SendJSONResponse(w, http.StatusOK, response)
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
	rawPayload, err := io.ReadAll(r.Body)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrBadRequest, http.StatusBadRequest, "failed to read webhook body", err.Error()))
		return
	}

	signature := r.Header.Get("X-Razorpay-Signature")
	if err := h.subService.HandleWebhookRaw(r.Context(), rawPayload, signature); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SubscriptionHandler) recordAdminSubscriptionAction(r *http.Request, action, targetID, outcome string, metadata map[string]interface{}) {
	if h.adminService == nil {
		return
	}

	actorEmail, _ := middleware.GetEmailFromContext(r.Context())
	_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
		ActorEmail: actorEmail,
		Action:     action,
		TargetType: "subscription",
		TargetID:   targetID,
		Outcome:    outcome,
		Metadata:   metadata,
	})
}
