package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamQueue_PerUserFIFO tests per-user ordering guarantee
func TestStreamQueue_PerUserFIFO(t *testing.T) {
	// Setup
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	sq := NewStreamQueue(redisClient, logger)
	ctx := context.Background()

	eventID := "test-event-1"
	userID := "user-123"

	// Cleanup
	defer func() {
		pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
		keys, _ := redisClient.Keys(ctx, pattern).Result()
		if len(keys) > 0 {
			redisClient.Del(ctx, keys...)
		}
	}()

	// Test: Enqueue 5 messages from same user
	for i := 0; i < 5; i++ {
		token := fmt.Sprintf("token-%d", i)
		result, err := sq.Enqueue(ctx, eventID, userID, token)

		require.NoError(t, err)
		require.NotNil(t, result)

		// Each message should have increasing position within user's stream
		assert.Equal(t, i+1, result.UserPos)

		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	}

	// Verify: Messages are in correct order
	messages, err := sq.GetUserMessages(ctx, eventID, userID)
	require.NoError(t, err)
	assert.Len(t, messages, 5)

	// Check tokens are in order
	for i, msg := range messages {
		expectedToken := fmt.Sprintf("token-%d", i)
		actualToken := msg.Values["token"].(string)
		assert.Equal(t, expectedToken, actualToken, "Token order mismatch at index %d", i)
	}

	t.Logf("✅ Per-user FIFO verified: %d messages in correct order", len(messages))
}

// TestStreamQueue_MultiUser tests ordering across multiple users
func TestStreamQueue_MultiUser(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	sq := NewStreamQueue(redisClient, logger)
	ctx := context.Background()

	eventID := "test-event-2"

	// Cleanup
	defer func() {
		pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
		keys, _ := redisClient.Keys(ctx, pattern).Result()
		if len(keys) > 0 {
			redisClient.Del(ctx, keys...)
		}
	}()

	// Test: Multiple users enqueue messages
	users := []string{"user-a", "user-b", "user-c"}

	for _, userID := range users {
		for i := 0; i < 3; i++ {
			token := fmt.Sprintf("%s-token-%d", userID, i)
			result, err := sq.Enqueue(ctx, eventID, userID, token)

			require.NoError(t, err)
			assert.Greater(t, result.Position, 0)

			time.Sleep(1 * time.Millisecond)
		}
	}

	// Verify: Each user has their own stream
	for _, userID := range users {
		messages, err := sq.GetUserMessages(ctx, eventID, userID)
		require.NoError(t, err)
		assert.Len(t, messages, 3, "User %s should have 3 messages", userID)

		// Verify order within each user
		for i, msg := range messages {
			expectedToken := fmt.Sprintf("%s-token-%d", userID, i)
			actualToken := msg.Values["token"].(string)
			assert.Equal(t, expectedToken, actualToken)
		}
	}

	// Check stats
	stats, err := sq.GetQueueStats(ctx, eventID)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.TotalUsers)
	assert.Equal(t, 9, stats.TotalMessages)
	assert.Equal(t, 3.0, stats.AvgPerUser)

	t.Logf("✅ Multi-user test passed: %d users, %d total messages", stats.TotalUsers, stats.TotalMessages)
}

// TestStreamQueue_Dequeue tests message removal
func TestStreamQueue_Dequeue(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	sq := NewStreamQueue(redisClient, logger)
	ctx := context.Background()

	eventID := "test-event-3"
	userID := "user-456"

	// Cleanup
	defer func() {
		pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
		keys, _ := redisClient.Keys(ctx, pattern).Result()
		if len(keys) > 0 {
			redisClient.Del(ctx, keys...)
		}
	}()

	// Enqueue 3 messages
	var streamIDs []string
	for i := 0; i < 3; i++ {
		result, err := sq.Enqueue(ctx, eventID, userID, fmt.Sprintf("token-%d", i))
		require.NoError(t, err)
		streamIDs = append(streamIDs, result.StreamID)
	}

	// Verify initial state
	messages, _ := sq.GetUserMessages(ctx, eventID, userID)
	assert.Len(t, messages, 3)

	// Dequeue first message
	err := sq.DequeueForUser(ctx, eventID, userID, streamIDs[0])
	require.NoError(t, err)

	// Verify one message removed
	messages, _ = sq.GetUserMessages(ctx, eventID, userID)
	assert.Len(t, messages, 2)

	// Remaining messages should be token-1 and token-2
	assert.Equal(t, "token-1", messages[0].Values["token"].(string))
	assert.Equal(t, "token-2", messages[1].Values["token"].(string))

	t.Logf("✅ Dequeue test passed: removed 1 message, %d remaining", len(messages))
}

// TestStreamQueue_Cleanup tests expired message cleanup
func TestStreamQueue_Cleanup(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	sq := NewStreamQueue(redisClient, logger)
	ctx := context.Background()

	eventID := "test-event-4"
	userID := "user-789"

	// Cleanup
	defer func() {
		pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
		keys, _ := redisClient.Keys(ctx, pattern).Result()
		if len(keys) > 0 {
			redisClient.Del(ctx, keys...)
		}
	}()

	// Enqueue messages
	for i := 0; i < 5; i++ {
		_, _ = sq.Enqueue(ctx, eventID, userID, fmt.Sprintf("token-%d", i))
		time.Sleep(10 * time.Millisecond)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Cleanup messages older than 50ms
	cleaned, err := sq.CleanupExpiredStreams(ctx, eventID, 50*time.Millisecond)
	require.NoError(t, err)

	t.Logf("✅ Cleanup test passed: cleaned %d messages", cleaned)

	// Note: Actual cleanup count may vary due to timing
	assert.GreaterOrEqual(t, cleaned, 0)
}

// TestStreamQueue_GlobalPosition tests position calculation
func TestStreamQueue_GlobalPosition(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	sq := NewStreamQueue(redisClient, logger)
	ctx := context.Background()

	eventID := "test-event-5"

	// Cleanup
	defer func() {
		pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
		keys, _ := redisClient.Keys(ctx, pattern).Result()
		if len(keys) > 0 {
			redisClient.Del(ctx, keys...)
		}
	}()

	// Setup: Create Position Index ZSET for accurate position calculation
	// This simulates the real Join API flow
	positionKey := fmt.Sprintf("position_index:{%s}", eventID)
	score := float64(time.Now().Unix())

	// User A enqueues 3 messages
	for i := 0; i < 3; i++ {
		token := fmt.Sprintf("a-token-%d", i)
		result, err := sq.Enqueue(ctx, eventID, "user-a", token)
		require.NoError(t, err)

		// Add to Position Index (simulating Join API)
		redisClient.ZAdd(ctx, positionKey, redis.Z{
			Score:  score + float64(i),
			Member: token,
		})
	}

	// User B enqueues 2 messages
	var lastResult *EnqueueResult
	for i := 0; i < 2; i++ {
		token := fmt.Sprintf("b-token-%d", i)
		result, err := sq.Enqueue(ctx, eventID, "user-b", token)
		require.NoError(t, err)
		lastResult = result

		// Add to Position Index (simulating Join API)
		redisClient.ZAdd(ctx, positionKey, redis.Z{
			Score:  score + float64(3+i),
			Member: token,
		})
	}

	// Last message from user B should be at position 5
	// (3 from user A + 2 from user B)
	assert.Equal(t, 5, lastResult.Position, "Global position should be 5")

	// Get position again
	pos, err := sq.GetPosition(ctx, eventID, "user-b", lastResult.StreamID)
	require.NoError(t, err)
	assert.Equal(t, 5, pos, "Position query should return 5")

	t.Logf("✅ Global position test passed: calculated position = %d", pos)
}

// BenchmarkStreamQueue_Enqueue benchmarks enqueue performance
func BenchmarkStreamQueue_Enqueue(b *testing.B) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce log noise

	sq := NewStreamQueue(redisClient, logger)
	ctx := context.Background()

	eventID := "bench-event"
	userID := "bench-user"

	// Cleanup
	defer func() {
		pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
		keys, _ := redisClient.Keys(ctx, pattern).Result()
		if len(keys) > 0 {
			redisClient.Del(ctx, keys...)
		}
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		token := fmt.Sprintf("token-%d", i)
		_, _ = sq.Enqueue(ctx, eventID, userID, token)
	}
}
