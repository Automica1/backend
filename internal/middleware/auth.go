// internal/middleware/auth.go
package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
	"github.com/golang-jwt/jwt/v5"
)

// JWT Claims structure for Kinde
type KindeClaims struct {
	Email string `json:"email"`
	Sub   string `json:"sub"`
	jwt.RegisteredClaims
}

// JWKS structures
type JWKS struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// Auth middleware validates Kinde JWT tokens using RS256
func Auth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"authentication token not found",
				))
				return
			}

			// Check if it's a Bearer token
			if !strings.HasPrefix(authHeader, "Bearer ") {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"invalid authorization format. Expected: Bearer <token>",
				))
				return
			}

			// Extract token
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == "" {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"bearer token is empty",
				))
				return
			}

			// Verify token
			claims, err := verifyKindeToken(tokenString)
			if err != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"authentication failed: "+err.Error(),
				))
				return
			}

			// Validate email exists in claims
			if claims.Email == "" {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"email not found in token",
				))
				return
			}

			// Add email to request context for use in the handler
			ctx := context.WithValue(r.Context(), "email", claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// verifyKindeToken verifies the Kinde JWT token using RS256
func verifyKindeToken(tokenString string) (*KindeClaims, error) {
	// Parse token without verification to get the kid
	token, err := jwt.ParseWithClaims(tokenString, &KindeClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method is RS256
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get kid from token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		// Get public key from JWKS
		publicKey, err := getPublicKeyFromJWKS(kid)
		if err != nil {
			return nil, fmt.Errorf("failed to get public key: %v", err)
		}

		return publicKey, nil
	})

	if err != nil {
		return nil, err
	}

	// Extract and validate claims
	claims, ok := token.Claims.(*KindeClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate issuer
	kindeIssuerURL := os.Getenv("KINDE_ISSUER_URL")
	if kindeIssuerURL == "" {
		return nil, fmt.Errorf("KINDE_ISSUER_URL environment variable not set")
	}

	if claims.Issuer != kindeIssuerURL {
		return nil, fmt.Errorf("invalid issuer")
	}

	return claims, nil
}

// getPublicKeyFromJWKS fetches the public key from Kinde's JWKS endpoint
func getPublicKeyFromJWKS(kid string) (*rsa.PublicKey, error) {
	kindeIssuerURL := os.Getenv("KINDE_ISSUER_URL")
	if kindeIssuerURL == "" {
		return nil, fmt.Errorf("KINDE_ISSUER_URL environment variable not set")
	}

	// Construct JWKS URL
	jwksURL := os.Getenv("KINDE_JWKS_URI")
	if jwksURL == "" {
		jwksURL = kindeIssuerURL + "/.well-known/jwks.json"
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Fetch JWKS
	resp, err := client.Get(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	// Parse JWKS response
	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %v", err)
	}

	// Find the key with matching kid
	for _, key := range jwks.Keys {
		if key.Kid == kid {
			return jwkToRSAPublicKey(key)
		}
	}

	return nil, fmt.Errorf("key with kid %s not found", kid)
}

// jwkToRSAPublicKey converts a JWK to an RSA public key
func jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	// Decode the modulus (n)
	nb, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %v", err)
	}

	// Decode the exponent (e)
	eb, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %v", err)
	}

	// Convert to big integers
	n := new(big.Int).SetBytes(nb)
	e := new(big.Int).SetBytes(eb)

	// Create RSA public key
	publicKey := &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}

	return publicKey, nil
}

// Helper function to extract email from context
func GetEmailFromContext(ctx context.Context) (string, bool) {
	email, ok := ctx.Value("email").(string)
	return email, ok
}