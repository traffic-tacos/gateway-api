package queue

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Embed Lua scripts at compile time
//
//go:embed lua/enqueue_atomic_streams.lua
var enqueueAtomicStreamsScript string

//go:embed lua/hold_seat_atomic.lua
var holdSeatAtomicScript string

//go:embed lua/release_seat_atomic.lua
var releaseSeatAtomicScript string

// LuaExecutor executes Lua scripts atomically on Redis
type LuaExecutor struct {
	redis redis.UniversalClient // 🔴 Changed to UniversalClient for Cluster support

	// Preloaded scripts
	enqueueScript *redis.Script
	holdScript    *redis.Script
	releaseScript *redis.Script

	logger *logrus.Logger
}

// NewLuaExecutor creates a new Lua script executor
func NewLuaExecutor(redisClient redis.UniversalClient, logger *logrus.Logger) *LuaExecutor {
	return &LuaExecutor{
		redis:         redisClient,
		enqueueScript: redis.NewScript(enqueueAtomicStreamsScript),
		holdScript:    redis.NewScript(holdSeatAtomicScript),
		releaseScript: redis.NewScript(releaseSeatAtomicScript),
		logger:        logger,
	}
}

// EnqueueAtomicResult contains the result of atomic enqueue
type EnqueueAtomicResult struct {
	StreamID string
	Error    string
}

// EnqueueAtomic performs atomic enqueue with dedupe check
// Returns stream ID on success, error message on failure
func (le *LuaExecutor) EnqueueAtomic(
	ctx context.Context,
	dedupeKey string,
	streamKey string,
	token string,
	eventID string,
	userID string,
	ttl int,
) (*EnqueueAtomicResult, error) {
	result, err := le.enqueueScript.Run(
		ctx,
		le.redis,
		[]string{dedupeKey, streamKey},
		token, eventID, userID, ttl,
	).Result()

	if err != nil {
		le.logger.WithError(err).WithFields(logrus.Fields{
			"dedupe_key": dedupeKey,
			"stream_key": streamKey,
		}).Error("Lua script execution failed")
		return nil, fmt.Errorf("lua script failed: %w", err)
	}

	// Parse result array: {status, data/error_msg}
	resultArray, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	if len(resultArray) < 2 {
		return nil, fmt.Errorf("invalid result array length: %d", len(resultArray))
	}

	// Check status (1 = success, 0 = error)
	status, ok := resultArray[0].(int64)
	if !ok {
		return nil, fmt.Errorf("invalid status type: %T", resultArray[0])
	}

	if status == 0 {
		// Error case
		errMsg, ok := resultArray[1].(string)
		if !ok {
			errMsg = fmt.Sprintf("%v", resultArray[1])
		}

		le.logger.WithFields(logrus.Fields{
			"error":      errMsg,
			"dedupe_key": dedupeKey,
		}).Debug("Enqueue rejected by Lua script")

		return &EnqueueAtomicResult{
			Error: errMsg,
		}, nil
	}

	// Success case - get stream ID
	streamID, ok := resultArray[1].(string)
	if !ok {
		return nil, fmt.Errorf("invalid stream ID type: %T", resultArray[1])
	}

	le.logger.WithFields(logrus.Fields{
		"stream_id": streamID,
		"event_id":  eventID,
		"user_id":   userID,
	}).Debug("Atomic enqueue successful")

	return &EnqueueAtomicResult{
		StreamID: streamID,
	}, nil
}

// HoldSeatAtomicResult contains the result of atomic seat hold
type HoldSeatAtomicResult struct {
	Success   bool
	Remaining int64
	Error     string
}

// HoldSeatAtomic performs atomic seat hold operation
func (le *LuaExecutor) HoldSeatAtomic(
	ctx context.Context,
	seatStatusKey string,
	holdKey string,
	inventoryKey string,
	seatID string,
	userID string,
	ttl int,
) (*HoldSeatAtomicResult, error) {
	result, err := le.holdScript.Run(
		ctx,
		le.redis,
		[]string{seatStatusKey, holdKey, inventoryKey},
		seatID, userID, ttl,
	).Result()

	if err != nil {
		le.logger.WithError(err).WithFields(logrus.Fields{
			"seat_id": seatID,
			"user_id": userID,
		}).Error("Hold seat Lua script failed")
		return nil, fmt.Errorf("lua script failed: %w", err)
	}

	// Parse result array: {status, data/error_msg}
	resultArray, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	if len(resultArray) < 2 {
		return nil, fmt.Errorf("invalid result array length: %d", len(resultArray))
	}

	// Check status (1 = success, 0 = error)
	status, ok := resultArray[0].(int64)
	if !ok {
		return nil, fmt.Errorf("invalid status type: %T", resultArray[0])
	}

	if status == 0 {
		// Error case
		errMsg, ok := resultArray[1].(string)
		if !ok {
			errMsg = fmt.Sprintf("%v", resultArray[1])
		}

		le.logger.WithFields(logrus.Fields{
			"error":   errMsg,
			"seat_id": seatID,
		}).Debug("Seat hold rejected")

		return &HoldSeatAtomicResult{
			Success: false,
			Error:   errMsg,
		}, nil
	}

	// Success case - parse remaining inventory
	remainingStr, ok := resultArray[1].(string)
	if !ok {
		return nil, fmt.Errorf("invalid remaining count type: %T", resultArray[1])
	}

	// Convert string to int64
	var remaining int64
	_, _ = fmt.Sscanf(remainingStr, "%d", &remaining)

	le.logger.WithFields(logrus.Fields{
		"seat_id":   seatID,
		"user_id":   userID,
		"remaining": remaining,
	}).Info("Seat hold successful")

	return &HoldSeatAtomicResult{
		Success:   true,
		Remaining: remaining,
	}, nil
}

// ReleaseSeatAtomicResult contains the result of atomic seat release
type ReleaseSeatAtomicResult struct {
	Success   bool
	Remaining int64
	Error     string
}

// ReleaseSeatAtomic performs atomic seat release operation
func (le *LuaExecutor) ReleaseSeatAtomic(
	ctx context.Context,
	seatStatusKey string,
	holdKey string,
	inventoryKey string,
	seatID string,
) (*ReleaseSeatAtomicResult, error) {
	result, err := le.releaseScript.Run(
		ctx,
		le.redis,
		[]string{seatStatusKey, holdKey, inventoryKey},
		seatID,
	).Result()

	if err != nil {
		le.logger.WithError(err).WithField("seat_id", seatID).Error("Release seat Lua script failed")
		return nil, fmt.Errorf("lua script failed: %w", err)
	}

	// Parse result array: {status, data/error_msg}
	resultArray, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	if len(resultArray) < 2 {
		return nil, fmt.Errorf("invalid result array length: %d", len(resultArray))
	}

	// Check status (1 = success, 0 = error)
	status, ok := resultArray[0].(int64)
	if !ok {
		return nil, fmt.Errorf("invalid status type: %T", resultArray[0])
	}

	if status == 0 {
		// Error case
		errMsg, ok := resultArray[1].(string)
		if !ok {
			errMsg = fmt.Sprintf("%v", resultArray[1])
		}

		le.logger.WithFields(logrus.Fields{
			"error":   errMsg,
			"seat_id": seatID,
		}).Debug("Seat release rejected")

		return &ReleaseSeatAtomicResult{
			Success: false,
			Error:   errMsg,
		}, nil
	}

	// Success case - parse remaining inventory
	remainingStr, ok := resultArray[1].(string)
	if !ok {
		return nil, fmt.Errorf("invalid remaining count type: %T", resultArray[1])
	}

	// Convert string to int64
	var remaining int64
	_, _ = fmt.Sscanf(remainingStr, "%d", &remaining)

	le.logger.WithFields(logrus.Fields{
		"seat_id":   seatID,
		"remaining": remaining,
	}).Info("Seat release successful")

	return &ReleaseSeatAtomicResult{
		Success:   true,
		Remaining: remaining,
	}, nil
}
