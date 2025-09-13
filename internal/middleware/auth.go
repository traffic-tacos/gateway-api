package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/traffic-tacos/gateway-api/internal/config"
	"github.com/traffic-tacos/gateway-api/internal/logging"
	"github.com/traffic-tacos/gateway-api/pkg/errors"
)

// AuthMiddleware handles JWT authentication
type AuthMiddleware struct {
	jwksURL    string
	issuer     string
	audience   string
	cacheTTL   time.Duration
	skipVerify bool
	jwkSet     *jwk.Set
	cacheTime  time.Time
	logger     *logging.Logger
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(cfg config.JWTConfig, logger *logging.Logger) (*AuthMiddleware, error) {
	return &AuthMiddleware{
		jwksURL:    cfg.JWKSURL,
		issuer:     cfg.Issuer,
		audience:   cfg.Audience,
		cacheTTL:   cfg.CacheTTL,
		skipVerify: cfg.SkipVerify,
		logger:     logger,
	}, nil
}

// AuthRequired returns the authentication middleware
func (a *AuthMiddleware) AuthRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check if route is anonymous
		path := c.Path()
		if a.isAnonymousRoute(path) {
			return c.Next()
		}

		// Extract token from Authorization header
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return errors.NewAppError(errors.CodeUnauthenticated, "Authorization header is required", nil)
		}

		tokenStr, err := a.extractBearerToken(authHeader)
		if err != nil {
			return errors.NewAppError(errors.CodeUnauthenticated, "Invalid authorization header format", err)
		}

		// Verify and parse token
		claims, err := a.verifyToken(c.Context(), tokenStr)
		if err != nil {
			a.logger.WarnWithContext(c.Context(), "JWT verification failed", "error", err.Error())
			return err
		}

		// Extract user information
		userID, ok := claims["sub"].(string)
		if !ok || userID == "" {
			return errors.NewAppError(errors.CodeForbidden, "Invalid token: missing subject", nil)
		}

		// Store user information in context
		c.Locals("user_id", userID)
		c.Locals("user_claims", claims)

		return c.Next()
	}
}

// extractBearerToken extracts the token from Authorization header
func (a *AuthMiddleware) extractBearerToken(authHeader string) (string, error) {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return "", fmt.Errorf("authorization header must start with 'Bearer '")
	}
	return strings.TrimPrefix(authHeader, bearerPrefix), nil
}

// verifyToken verifies and parses the JWT token
func (a *AuthMiddleware) verifyToken(ctx context.Context, tokenStr string) (map[string]interface{}, error) {
	// Parse token without verification first
	token, err := jwt.Parse([]byte(tokenStr), jwt.WithVerify(false))
	if err != nil {
		return nil, errors.NewAppError(errors.CodeUnauthenticated, "Invalid JWT token format", err)
	}

	// Verify issuer and audience
	if err := jwt.Validate(token,
		jwt.WithIssuer(a.issuer),
		jwt.WithAudience(a.audience),
	); err != nil {
		return nil, errors.NewAppError(errors.CodeForbidden, "Token validation failed", err)
	}

	// Skip signature verification if configured (for development)
	if a.skipVerify {
		claims, err := token.AsMap(ctx)
		if err != nil {
			return nil, errors.NewAppError(errors.CodeUnauthenticated, "Failed to extract token claims", err)
		}
		return claims, nil
	}

	// Verify signature using JWKS
	if err := a.verifySignature(ctx, tokenStr); err != nil {
		return nil, err
	}

	// Extract claims
	claims, err := token.AsMap(ctx)
	if err != nil {
		return nil, errors.NewAppError(errors.CodeUnauthenticated, "Failed to extract token claims", err)
	}

	return claims, nil
}

// verifySignature verifies the JWT signature using JWKS
func (a *AuthMiddleware) verifySignature(ctx context.Context, tokenStr string) error {
	// Fetch or use cached JWK Set
	jwkSet, err := a.getJWKSet(ctx)
	if err != nil {
		return errors.NewAppError(errors.CodeUnauthenticated, "Failed to fetch JWK set", err)
	}

	// Parse and verify token with JWK set
	_, err = jwt.Parse([]byte(tokenStr),
		jwt.WithKeySet(jwkSet),
		jwt.WithIssuer(a.issuer),
		jwt.WithAudience(a.audience),
	)
	if err != nil {
		return errors.NewAppError(errors.CodeUnauthenticated, "Token signature verification failed", err)
	}

	return nil
}

// getJWKSet fetches JWK set from cache or remote
func (a *AuthMiddleware) getJWKSet(ctx context.Context) (*jwk.Set, error) {
	now := time.Now()

	// Return cached JWK set if still valid
	if a.jwkSet != nil && now.Sub(a.cacheTime) < a.cacheTTL {
		return a.jwkSet, nil
	}

	// Fetch new JWK set
	resp, err := http.Get(a.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	// Parse JWK set
	jwkSet, err := jwk.ParseReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	// Update cache
	a.jwkSet = &jwkSet
	a.cacheTime = now

	a.logger.InfoWithContext(ctx, "Updated JWK set cache")

	return &jwkSet, nil
}

// isAnonymousRoute checks if the route allows anonymous access
func (a *AuthMiddleware) isAnonymousRoute(path string) bool {
	anonymousRoutes := []string{
		"/healthz",
		"/readyz",
		"/version",
		"/metrics",
		"/api/v1/queue/join",
		"/api/v1/queue/status",
	}

	for _, route := range anonymousRoutes {
		if strings.HasPrefix(path, route) {
			return true
		}
	}
	return false
}
