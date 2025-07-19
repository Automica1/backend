// internal/handlers/debug.go
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"chi-mongo-backend/pkg/utils"
	"github.com/golang-jwt/jwt/v5"
)

type DebugHandler struct{}

func NewDebugHandler() *DebugHandler {
	return &DebugHandler{}
}

// ShowTokenData shows all data from the JWT token without verification
func (h *DebugHandler) ShowTokenData(w http.ResponseWriter, r *http.Request) {
	// Get Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.SendJSONResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error": "No Authorization header found",
		})
		return
	}

	// Extract token
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == "" {
		utils.SendJSONResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Bearer token is empty",
		})
		return
	}

	// Parse token WITHOUT verification (just to see the data)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		utils.SendJSONResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Failed to parse token: " + err.Error(),
		})
		return
	}

	// Get claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		utils.SendJSONResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Failed to get claims from token",
		})
		return
	}

	// Pretty print all claims
	prettyJSON, _ := json.MarshalIndent(claims, "", "  ")

	// Send response with all token data
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Token data decoded successfully",
		"token_data": claims,
		"pretty_json": string(prettyJSON),
		"has_role_key": claims["role"] != nil,
		"has_roles": claims["roles"] != nil,
		"has_permissions": claims["permissions"] != nil,
	})
}