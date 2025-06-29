// internal/models/response.go
package models

type RegisterUserResponse struct {
	Message string `json:"message"`
	User    User   `json:"user"`
	Credits int    `json:"credits"`
}

type CreditsResponse struct {
	Message string `json:"message"`
	UserID  string `json:"userId"`
	Credits int    `json:"credits"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}