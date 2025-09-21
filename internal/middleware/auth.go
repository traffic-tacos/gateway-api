package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type AuthMiddleware struct {
	config     *config.JWTConfig
	redisClient *redis.Client
	logger     *logrus.Logger
	jwkCache   jwk.Cache
}

func NewAuthMiddleware(cfg *config.JWTConfig, redisClient *redis.Client, logger *logrus.Logger) (*AuthMiddleware, error) {
	// Create JWK cache
	cache := jwk.NewCache(context.Background())

	// Register the JWKS endpoint
	if err := cache.Register(cfg.JWKSEndpoint, jwk.WithMinRefreshInterval(cfg.CacheTTL)); err != nil {
		return nil, fmt.Errorf("failed to register JWKS endpoint: %w", err)
	}

	// Pre-fetch the keys
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := cache.Refresh(ctx, cfg.JWKSEndpoint); err != nil {
		logger.WithError(err).Warn("Failed to pre-fetch JWKS, will try during first request")
	}

	return &AuthMiddleware{
		config:      cfg,
		redisClient: redisClient,
		logger:      logger,
		jwkCache:    cache,
	}, nil
}

// JWT authentication middleware
func (a *AuthMiddleware) Authenticate(exemptPaths []string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check if path is exempt from authentication
		path := c.Path()
		for _, exemptPath := range exemptPaths {
			if strings.HasPrefix(path, exemptPath) {
				return c.Next()
			}
		}

		// Extract token from Authorization header
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return a.unauthorizedError(c, "MISSING_AUTHORIZATION", "Authorization header is required")
		}

		// Check Bearer token format
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			return a.unauthorizedError(c, "INVALID_TOKEN_FORMAT", "Authorization header must be Bearer token")
		}

		tokenString := authHeader[len(bearerPrefix):]
		if tokenString == "" {
			return a.unauthorizedError(c, "MISSING_TOKEN", "Token is required")
		}

		// Validate JWT token
		claims, err := a.validateToken(c.Context(), tokenString)
		if err != nil {
			a.logger.WithError(err).WithField("path", path).Debug("Token validation failed")
			return a.unauthorizedError(c, "INVALID_TOKEN", "Token validation failed")
		}

		// Set user context
		c.Locals("user_claims", claims)
		if userID, ok := claims["sub"].(string); ok {
			c.Locals("user_id", userID)
		}

		return c.Next()
	}
}

// validateToken validates JWT token using JWKS
func (a *AuthMiddleware) validateToken(ctx context.Context, tokenString string) (jwt.MapClaims, error) {
	// Parse token without verification to get the key ID
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Get the key ID from token header
		keyID, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		// Get JWK set from cache
		set, err := a.jwkCache.Get(ctx, a.config.JWKSEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get JWK set: %w", err)
		}

		// Find the key with matching kid
		key, found := set.LookupKeyID(keyID)
		if !found {
			return nil, fmt.Errorf("key with ID %s not found", keyID)
		}

		// Convert JWK to verification key
		var verifyKey interface{}
		if err := key.Raw(&verifyKey); err != nil {
			return nil, fmt.Errorf("failed to get raw key: %w", err)
		}

		return verifyKey, nil
	}, jwt.WithValidMethods([]string{"RS256", "ES256"}))

	if err != nil {
		return nil, fmt.Errorf("token parsing failed: %w", err)
	}

	// Check if token is valid
	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	// Get claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to get token claims")
	}

	// Validate standard claims
	if err := a.validateClaims(claims); err != nil {
		return nil, fmt.Errorf("claims validation failed: %w", err)
	}

	return claims, nil
}

// validateClaims validates JWT standard claims
func (a *AuthMiddleware) validateClaims(claims jwt.MapClaims) error {
	// Validate expiration
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return fmt.Errorf("token has expired")
		}
	} else {
		return fmt.Errorf("exp claim is required")
	}

	// Validate not before
	if nbf, ok := claims["nbf"].(float64); ok {
		if time.Now().Unix() < int64(nbf) {
			return fmt.Errorf("token not valid yet")
		}
	}

	// Validate issuer
	if iss, ok := claims["iss"].(string); ok {
		if iss != a.config.Issuer {
			return fmt.Errorf("invalid issuer: expected %s, got %s", a.config.Issuer, iss)
		}
	} else {
		return fmt.Errorf("iss claim is required")
	}

	// Validate audience
	if aud, ok := claims["aud"]; ok {
		switch v := aud.(type) {
		case string:
			if v != a.config.Audience {
				return fmt.Errorf("invalid audience: expected %s, got %s", a.config.Audience, v)
			}
		case []interface{}:
			found := false
			for _, audience := range v {
				if audStr, ok := audience.(string); ok && audStr == a.config.Audience {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("invalid audience: %s not found in %v", a.config.Audience, v)
			}
		default:
			return fmt.Errorf("aud claim must be string or array")
		}
	} else {
		return fmt.Errorf("aud claim is required")
	}

	return nil
}

// unauthorizedError returns a standardized unauthorized error response
func (a *AuthMiddleware) unauthorizedError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

// GetUserID extracts user ID from context
func GetUserID(c *fiber.Ctx) string {
	if userID, ok := c.Locals("user_id").(string); ok {
		return userID
	}
	return ""
}

// GetUserClaims extracts user claims from context
func GetUserClaims(c *fiber.Ctx) jwt.MapClaims {
	if claims, ok := c.Locals("user_claims").(jwt.MapClaims); ok {
		return claims
	}
	return nil
}