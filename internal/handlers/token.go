// internal/handlers/token.go
package handlers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

type TokenHandler struct {
	tokenService   services.CreditTokenService
	creditsService services.CreditsService
	userService    services.UserService
	adminService   services.AdminService
}

func NewTokenHandler(tokenService services.CreditTokenService, creditsService services.CreditsService, userService services.UserService, adminService services.AdminService) *TokenHandler {
	return &TokenHandler{
		tokenService:   tokenService,
		creditsService: creditsService,
		userService:    userService,
		adminService:   adminService,
	}
}

func (h *TokenHandler) GenerateToken(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to generate tokens",
		))
		return
	}

	// Get admin email from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	var req models.GenerateTokenRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.tokenService.GenerateToken(r.Context(), &req, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if actorEmail, ok := middleware.GetEmailFromContext(r.Context()); ok && h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: actorEmail,
			Action:     "generate_token",
			TargetType: "token",
			TargetID:   response.Token,
			Outcome:    "success",
			Metadata: map[string]interface{}{
				"credits":     req.Credits,
				"description": req.Description,
			},
		})
	}

	utils.SendJSONResponse(w, http.StatusCreated, response)
}

func (h *TokenHandler) RedeemToken(w http.ResponseWriter, r *http.Request) {
	// Get email from context (set by auth middleware)
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	var req models.RedeemTokenRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Get user by email to get userID
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		// If user not found, try to auto-create them
		if apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
			registerReq := &models.RegisterUserRequest{
				UserID: email,
				Email:  email,
			}

			_, createErr := h.userService.RegisterUser(r.Context(), registerReq)
			if createErr != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to auto-create user: "+createErr.Error(),
				))
				return
			}

			// After successful creation, fetch the user again
			user, err = h.userService.GetUserByEmail(r.Context(), email)
			if err != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to fetch newly created user: "+err.Error(),
				))
				return
			}
		} else {
			utils.SendErrorResponse(w, err)
			return
		}
	}

	response, err := h.tokenService.RedeemToken(r.Context(), &req, user.UserID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *TokenHandler) GetMyTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view tokens",
		))
		return
	}

	// Get admin email from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	tokens, err := h.tokenService.GetTokensByCreatedBy(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Tokens retrieved successfully",
		"tokens":  tokens,
	})
}

// GetAllTokens - Admin only: Get all tokens in the system
func (h *TokenHandler) GetAllTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view all tokens",
		))
		return
	}

	limit := parseAdminIntQuery(r, "limit", 25)
	skip := parseAdminIntQuery(r, "skip", 0)
	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")

	tokens, total, err := h.tokenService.ListTokens(r.Context(), models.AdminListQuery{
		Limit:  limit,
		Skip:   skip,
		Search: search,
		Status: status,
	})
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "All tokens retrieved successfully",
		"count":   total,
		"tokens":  tokens,
		"limit":   limit,
		"skip":    skip,
	})
}

// GetUsedTokens - Admin only: Get all used tokens
func (h *TokenHandler) GetUsedTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view used tokens",
		))
		return
	}

	limit := parseAdminIntQuery(r, "limit", 25)
	skip := parseAdminIntQuery(r, "skip", 0)
	tokens, total, err := h.tokenService.ListTokens(r.Context(), models.AdminListQuery{Limit: limit, Skip: skip, Status: "used"})
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Used tokens retrieved successfully",
		"count":   total,
		"tokens":  tokens,
		"limit":   limit,
		"skip":    skip,
	})
}

// GetUnusedTokens - Admin only: Get all unused tokens
func (h *TokenHandler) GetUnusedTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view unused tokens",
		))
		return
	}

	limit := parseAdminIntQuery(r, "limit", 25)
	skip := parseAdminIntQuery(r, "skip", 0)
	tokens, total, err := h.tokenService.ListTokens(r.Context(), models.AdminListQuery{Limit: limit, Skip: skip, Status: "unused"})
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Unused tokens retrieved successfully",
		"count":   total,
		"tokens":  tokens,
		"limit":   limit,
		"skip":    skip,
	})
}

func (h *TokenHandler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to delete tokens",
		))
		return
	}

	// Get admin email from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get tokenId from URL path parameter
	tokenID := chi.URLParam(r, "tokenId")
	if tokenID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrBadRequest,
			http.StatusBadRequest,
			"token ID is required",
		))
		return
	}

	response, err := h.tokenService.DeleteToken(r.Context(), tokenID, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if actorEmail, ok := middleware.GetEmailFromContext(r.Context()); ok && h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: actorEmail,
			Action:     "delete_token",
			TargetType: "token",
			TargetID:   tokenID,
			Outcome:    "success",
			Metadata: map[string]interface{}{
				"description": response.Description,
				"credits":     response.Credits,
			},
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *TokenHandler) ExportTokensCSV(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	scope := r.URL.Query().Get("scope")
	var (
		tokens []*models.CreditToken
		err    error
	)

	if scope == "my" {
		email, ok := middleware.GetEmailFromContext(r.Context())
		if !ok {
			utils.SendErrorResponse(w, apperrors.NewAppError(
				apperrors.ErrUnauthorized,
				http.StatusUnauthorized,
				"email not found in context",
			))
			return
		}
		tokens, err = h.tokenService.GetTokensByCreatedBy(r.Context(), email)
		if err == nil {
			search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
			if search != "" {
				filtered := make([]*models.CreditToken, 0, len(tokens))
				for _, token := range tokens {
					if strings.Contains(strings.ToLower(token.Token), search) ||
						strings.Contains(strings.ToLower(token.Description), search) ||
						strings.Contains(strings.ToLower(token.CreatedBy), search) {
						filtered = append(filtered, token)
					}
				}
				tokens = filtered
			}
		}
	} else {
		limit := parseAdminIntQuery(r, "limit", 100000)
		skip := parseAdminIntQuery(r, "skip", 0)
		search := r.URL.Query().Get("search")
		status := r.URL.Query().Get("status")

		tokens, _, err = h.tokenService.ListTokens(r.Context(), models.AdminListQuery{
			Limit:  limit,
			Skip:   skip,
			Search: search,
			Status: status,
		})
	}
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"Token", "Credits", "Created By", "Created At", "Expires At", "Used", "Used By", "Used At", "Description"})
	for _, token := range tokens {
		_ = writer.Write([]string{
			token.Token,
			strconv.Itoa(token.Credits),
			token.CreatedBy,
			token.CreatedAt.Format(time.RFC3339),
			token.ExpiresAt.Format(time.RFC3339),
			strconv.FormatBool(token.IsUsed),
			token.UsedBy,
			formatTokenTime(token.UsedAt),
			token.Description,
		})
	}
	writer.Flush()

	filename := fmt.Sprintf("tokens-%s.csv", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func parseAdminIntQuery(r *http.Request, key string, defaultValue int) int {
	if value := r.URL.Query().Get(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func formatTokenTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}
