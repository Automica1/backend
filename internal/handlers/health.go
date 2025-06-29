// internal/handlers/health.go
package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/pkg/utils"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := models.HealthResponse{
		Status:  "healthy",
		Message: "Server is running and connected to MongoDB",
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}
