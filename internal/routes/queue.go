package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type QueueHandler struct {
	redisClient *redis.Client
	logger      *logrus.Logger
}

type JoinQueueRequest struct {
	EventID string `json:"event_id" validate:"required"`
	UserID  string `json:"user_id,omitempty"`
}

type QueueStatusResponse struct {
	Status      string `json:"status"`      // waiting|ready|expired
	Position    int    `json:"position"`    // Current position in queue
	ETASeconds  int    `json:"eta_sec"`     // Estimated time to admission
	WaitingTime int    `json:"waiting_time"` // Time already waited in seconds
}

type JoinQueueResponse struct {
	WaitingToken string `json:"waiting_token"`
	PositionHint int    `json:"position_hint"`
	Status       string `json:"status"`
}

type EnterQueueRequest struct {
	WaitingToken string `json:"waiting_token" validate:"required"`
}

type EnterQueueResponse struct {
	Admission        string `json:"admission"`         // granted|denied
	ReservationToken string `json:"reservation_token,omitempty"`
	TTLSeconds       int    `json:"ttl_sec,omitempty"`
}

type QueueData struct {
	EventID     string    `json:"event_id"`
	UserID      string    `json:"user_id,omitempty"`
	JoinedAt    time.Time `json:"joined_at"`
	Position    int       `json:"position"`
	Status      string    `json:"status"` // waiting|ready|expired
}

func NewQueueHandler(redisClient *redis.Client, logger *logrus.Logger) *QueueHandler {
	return &QueueHandler{
		redisClient: redisClient,
		logger:      logger,
	}
}

// Join handles queue joining
// @Summary Join waiting queue
// @Description Join the waiting queue for an event
// @Tags Queue
// @Accept json
// @Produce json
// @Param request body JoinQueueRequest true "Join queue request"
// @Success 202 {object} JoinQueueResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /queue/join [post]
func (q *QueueHandler) Join(c *fiber.Ctx) error {
	var req JoinQueueRequest
	if err := c.BodyParser(&req); err != nil {
		return q.badRequestError(c, "INVALID_REQUEST", "Invalid request body")
	}

	if req.EventID == "" {
		return q.badRequestError(c, "MISSING_EVENT_ID", "event_id is required")
	}

	// Get user ID from auth context if available
	if userID := middleware.GetUserID(c); userID != "" {
		req.UserID = userID
	}

	// Generate waiting token
	waitingToken := uuid.New().String()

	// Create queue data
	queueData := QueueData{
		EventID:  req.EventID,
		UserID:   req.UserID,
		JoinedAt: time.Now(),
		Status:   "waiting",
	}

	// Get current queue position
	position, err := q.getNextQueuePosition(c.Context(), req.EventID)
	if err != nil {
		q.logger.WithError(err).Error("Failed to get queue position")
		return q.internalError(c, "QUEUE_ERROR", "Failed to join queue")
	}

	queueData.Position = position

	// Store in Redis with TTL (30 minutes)
	queueKey := fmt.Sprintf("queue:waiting:%s", waitingToken)
	queueDataBytes, _ := json.Marshal(queueData)

	ctx := context.Background()
	if err := q.redisClient.Set(ctx, queueKey, queueDataBytes, 30*time.Minute).Err(); err != nil {
		q.logger.WithError(err).Error("Failed to store queue data")
		return q.internalError(c, "QUEUE_ERROR", "Failed to join queue")
	}

	// Add to event queue
	eventQueueKey := fmt.Sprintf("queue:event:%s", req.EventID)
	if err := q.redisClient.ZAdd(ctx, eventQueueKey, redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: waitingToken,
	}).Err(); err != nil {
		q.logger.WithError(err).Error("Failed to add to event queue")
	}

	q.logger.WithFields(logrus.Fields{
		"waiting_token": waitingToken,
		"event_id":      req.EventID,
		"user_id":       req.UserID,
		"position":      position,
	}).Info("User joined queue")

	return c.Status(fiber.StatusAccepted).JSON(JoinQueueResponse{
		WaitingToken: waitingToken,
		PositionHint: position,
		Status:       "waiting",
	})
}

// Status handles queue status queries
// @Summary Check queue status
// @Description Check the current status of a waiting token in the queue
// @Tags Queue
// @Produce json
// @Param token query string true "Waiting token"
// @Success 200 {object} QueueStatusResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 404 {object} map[string]interface{} "Token not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /queue/status [get]
func (q *QueueHandler) Status(c *fiber.Ctx) error {
	waitingToken := c.Query("token")
	if waitingToken == "" {
		return q.badRequestError(c, "MISSING_TOKEN", "waiting token is required")
	}

	// Get queue data
	queueData, err := q.getQueueData(c.Context(), waitingToken)
	if err != nil {
		if err == redis.Nil {
			return q.notFoundError(c, "TOKEN_NOT_FOUND", "Waiting token not found or expired")
		}
		q.logger.WithError(err).Error("Failed to get queue data")
		return q.internalError(c, "QUEUE_ERROR", "Failed to get queue status")
	}

	// Calculate current position and ETA
	currentPosition, eta := q.calculatePositionAndETA(c.Context(), queueData)
	waitingTime := int(time.Since(queueData.JoinedAt).Seconds())

	return c.JSON(QueueStatusResponse{
		Status:      queueData.Status,
		Position:    currentPosition,
		ETASeconds:  eta,
		WaitingTime: waitingTime,
	})
}

// Enter handles queue entrance requests
// @Summary Request queue entrance
// @Description Request entrance to the main system after waiting in queue
// @Tags Queue
// @Accept json
// @Produce json
// @Param request body EnterQueueRequest true "Enter queue request"
// @Success 200 {object} EnterQueueResponse
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 403 {object} map[string]interface{} "Not ready for entrance"
// @Failure 404 {object} map[string]interface{} "Token not found"
// @Failure 500 {object} map[string]interface{} "Internal error"
// @Router /queue/enter [post]
func (q *QueueHandler) Enter(c *fiber.Ctx) error {
	var req EnterQueueRequest
	if err := c.BodyParser(&req); err != nil {
		return q.badRequestError(c, "INVALID_REQUEST", "Invalid request body")
	}

	if req.WaitingToken == "" {
		return q.badRequestError(c, "MISSING_TOKEN", "waiting_token is required")
	}

	// Get queue data
	queueData, err := q.getQueueData(c.Context(), req.WaitingToken)
	if err != nil {
		if err == redis.Nil {
			return q.notFoundError(c, "TOKEN_NOT_FOUND", "Waiting token not found or expired")
		}
		return q.internalError(c, "QUEUE_ERROR", "Failed to validate waiting token")
	}

	// Check if user is at front of queue (simplified logic)
	if !q.isEligibleForEntry(c.Context(), queueData) {
		return q.forbiddenError(c, "NOT_READY", "Your turn has not arrived yet")
	}

	// Generate reservation token
	reservationToken := uuid.New().String()

	// Store reservation token with TTL (30 seconds as per spec)
	reservationKey := fmt.Sprintf("queue:reservation:%s", reservationToken)
	reservationData := map[string]interface{}{
		"event_id":       queueData.EventID,
		"user_id":        queueData.UserID,
		"waiting_token":  req.WaitingToken,
		"granted_at":     time.Now(),
	}

	ctx := context.Background()
	reservationDataBytes, _ := json.Marshal(reservationData)
	if err := q.redisClient.Set(ctx, reservationKey, reservationDataBytes, 30*time.Second).Err(); err != nil {
		q.logger.WithError(err).Error("Failed to store reservation token")
		return q.internalError(c, "QUEUE_ERROR", "Failed to grant admission")
	}

	// Update queue status to ready
	queueData.Status = "ready"
	queueDataBytes, _ := json.Marshal(queueData)
	queueKey := fmt.Sprintf("queue:waiting:%s", req.WaitingToken)
	q.redisClient.Set(ctx, queueKey, queueDataBytes, 30*time.Minute)

	q.logger.WithFields(logrus.Fields{
		"waiting_token":     req.WaitingToken,
		"reservation_token": reservationToken,
		"event_id":          queueData.EventID,
		"user_id":           queueData.UserID,
	}).Info("Queue admission granted")

	return c.JSON(EnterQueueResponse{
		Admission:        "granted",
		ReservationToken: reservationToken,
		TTLSeconds:       30,
	})
}

// Leave handles queue departure
// @Summary Leave waiting queue
// @Description Leave the waiting queue voluntarily
// @Tags Queue
// @Produce json
// @Param token query string true "Waiting token"
// @Success 200 {object} map[string]interface{} "Success"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Router /queue/leave [delete]
func (q *QueueHandler) Leave(c *fiber.Ctx) error {
	waitingToken := c.Query("token")
	if waitingToken == "" {
		return q.badRequestError(c, "MISSING_TOKEN", "waiting token is required")
	}

	ctx := context.Background()

	// Remove from waiting queue
	queueKey := fmt.Sprintf("queue:waiting:%s", waitingToken)
	if err := q.redisClient.Del(ctx, queueKey).Err(); err != nil {
		q.logger.WithError(err).Error("Failed to remove from waiting queue")
	}

	// Get queue data to remove from event queue
	queueData, err := q.getQueueData(ctx, waitingToken)
	if err == nil {
		eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
		q.redisClient.ZRem(ctx, eventQueueKey, waitingToken)
	}

	q.logger.WithFields(logrus.Fields{
		"waiting_token": waitingToken,
	}).Info("User left queue")

	return c.JSON(fiber.Map{
		"status": "left",
	})
}

// Helper methods

func (q *QueueHandler) getQueueData(ctx context.Context, waitingToken string) (*QueueData, error) {
	queueKey := fmt.Sprintf("queue:waiting:%s", waitingToken)
	data, err := q.redisClient.Get(ctx, queueKey).Result()
	if err != nil {
		return nil, err
	}

	var queueData QueueData
	if err := json.Unmarshal([]byte(data), &queueData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue data: %w", err)
	}

	return &queueData, nil
}

func (q *QueueHandler) getNextQueuePosition(ctx context.Context, eventID string) (int, error) {
	eventQueueKey := fmt.Sprintf("queue:event:%s", eventID)
	count, err := q.redisClient.ZCard(ctx, eventQueueKey).Result()
	if err != nil {
		return 0, err
	}
	return int(count) + 1, nil
}

func (q *QueueHandler) calculatePositionAndETA(ctx context.Context, queueData *QueueData) (int, int) {
	// Simplified calculation - in production this would be more sophisticated
	eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)

	// Get rank in sorted set (position in queue)
	rank, err := q.redisClient.ZRank(ctx, eventQueueKey, fmt.Sprintf("queue:waiting:%s", queueData.EventID)).Result()
	if err != nil {
		return queueData.Position, 60 // Default ETA
	}

	position := int(rank) + 1
	eta := position * 2 // 2 seconds per person (simplified)

	if eta < 0 {
		eta = 0
	}

	return position, eta
}

func (q *QueueHandler) isEligibleForEntry(ctx context.Context, queueData *QueueData) bool {
	// Simplified logic - allow entry if user has waited more than 10 seconds
	// In production, this would check actual queue position and admission rate
	waitTime := time.Since(queueData.JoinedAt)
	return waitTime > 10*time.Second
}

// Error response helpers
func (q *QueueHandler) badRequestError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (q *QueueHandler) notFoundError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (q *QueueHandler) forbiddenError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}

func (q *QueueHandler) internalError(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": fiber.Map{
			"code":     code,
			"message":  message,
			"trace_id": c.Get("X-Request-ID"),
		},
	})
}