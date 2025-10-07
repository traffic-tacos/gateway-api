package routes

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"

	"github.com/traffic-tacos/gateway-api/internal/models"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	dynamoClient *dynamodb.Client
	tableName    string
	jwtSecret    string
	logger       *logrus.Logger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(dynamoClient *dynamodb.Client, tableName string, jwtSecret string, logger *logrus.Logger) *AuthHandler {
	return &AuthHandler{
		dynamoClient: dynamoClient,
		tableName:    tableName,
		jwtSecret:    jwtSecret,
		logger:       logger,
	}
}

// Login handles user login
// @Summary User login
// @Description Authenticate user and return JWT token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} models.AuthResponse
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 401 {object} map[string]interface{} "Invalid credentials"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INVALID_REQUEST",
				"message": "Invalid request body",
			},
		})
	}

	// Get user by username from DynamoDB
	user, err := h.getUserByUsername(c.Context(), req.Username)
	if err != nil {
		h.logger.WithError(err).WithField("username", req.Username).Warn("User not found")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INVALID_CREDENTIALS",
				"message": "Invalid username or password",
			},
		})
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		h.logger.WithError(err).WithField("username", req.Username).Warn("Invalid password")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INVALID_CREDENTIALS",
				"message": "Invalid username or password",
			},
		})
	}

	// Generate JWT token
	token, expiresIn, err := h.generateJWT(user)
	if err != nil {
		h.logger.WithError(err).Error("Failed to generate JWT")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "TOKEN_ERROR",
				"message": "Failed to generate token",
			},
		})
	}

	h.logger.WithFields(logrus.Fields{
		"user_id":  user.UserID,
		"username": user.Username,
	}).Info("User logged in successfully")

	return c.JSON(models.AuthResponse{
		Token:       token,
		UserID:      user.UserID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		ExpiresIn:   expiresIn,
	})
}

// Register handles user registration
// @Summary User registration
// @Description Register a new user
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body models.RegisterRequest true "Registration data"
// @Success 201 {object} models.AuthResponse
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 409 {object} map[string]interface{} "Username already exists"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req models.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "INVALID_REQUEST",
				"message": "Invalid request body",
			},
		})
	}

	// Check if username already exists
	existingUser, _ := h.getUserByUsername(c.Context(), req.Username)
	if existingUser != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "USERNAME_EXISTS",
				"message": "Username already exists",
			},
		})
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.WithError(err).Error("Failed to hash password")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "HASH_ERROR",
				"message": "Failed to process password",
			},
		})
	}

	// Create user
	now := time.Now()
	user := &models.User{
		UserID:       uuid.New().String(),
		Username:     req.Username,
		PasswordHash: string(passwordHash),
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		Role:         "user",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Save to DynamoDB
	if err := h.createUser(c.Context(), user); err != nil {
		h.logger.WithError(err).Error("Failed to create user")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "CREATE_ERROR",
				"message": "Failed to create user",
			},
		})
	}

	// Generate JWT token
	token, expiresIn, err := h.generateJWT(user)
	if err != nil {
		h.logger.WithError(err).Error("Failed to generate JWT")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "TOKEN_ERROR",
				"message": "Failed to generate token",
			},
		})
	}

	h.logger.WithFields(logrus.Fields{
		"user_id":  user.UserID,
		"username": user.Username,
	}).Info("User registered successfully")

	return c.Status(fiber.StatusCreated).JSON(models.AuthResponse{
		Token:       token,
		UserID:      user.UserID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		ExpiresIn:   expiresIn,
	})
}

// Helper methods

func (h *AuthHandler) getUserByUsername(ctx context.Context, username string) (*models.User, error) {
	// Query by username (GSI assumed: username-index)
	result, err := h.dynamoClient.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(h.tableName),
		IndexName:              aws.String("username-index"),
		KeyConditionExpression: aws.String("username = :username"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":username": &types.AttributeValueMemberS{Value: username},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	var user models.User
	if err := attributevalue.UnmarshalMap(result.Items[0], &user); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %w", err)
	}

	return &user, nil
}

func (h *AuthHandler) createUser(ctx context.Context, user *models.User) error {
	item, err := attributevalue.MarshalMap(user)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	_, err = h.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(h.tableName),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(user_id)"),
	})

	if err != nil {
		return fmt.Errorf("put item failed: %w", err)
	}

	return nil
}

func (h *AuthHandler) generateJWT(user *models.User) (string, int, error) {
	expiresIn := 24 * 3600 // 24 hours
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	claims := jwt.MapClaims{
		"sub":      user.UserID, // Standard JWT claim for user ID
		"user_id":  user.UserID, // Keep for backward compatibility
		"username": user.Username,
		"role":     user.Role,
		"exp":      expiresAt.Unix(),
		"iat":      time.Now().Unix(),
		"iss":      "traffic-tacos-gateway",
		"aud":      "traffic-tacos-api",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		return "", 0, err
	}

	return tokenString, expiresIn, nil
}
