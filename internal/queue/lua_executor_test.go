package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLuaExecutor_EnqueueAtomic(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	executor := NewLuaExecutor(redisClient, logger)
	ctx := context.Background()

	dedupeKey := "dedupe:test-123"
	streamKey := "stream:event:{test}:user:user1"

	// Cleanup
	defer func() {
		redisClient.Del(ctx, dedupeKey, streamKey)
	}()

	// Test 1: First enqueue should succeed
	result1, err := executor.EnqueueAtomic(
		ctx,
		dedupeKey,
		streamKey,
		"token-1",
		"test-event",
		"user1",
		300, // 5 min TTL
	)

	require.NoError(t, err)
	require.NotNil(t, result1)
	assert.Empty(t, result1.Error, "First enqueue should succeed")
	assert.NotEmpty(t, result1.StreamID, "Should return stream ID")

	t.Logf("✅ First enqueue: StreamID=%s", result1.StreamID)

	// Test 2: Duplicate enqueue should be rejected
	result2, err := executor.EnqueueAtomic(
		ctx,
		dedupeKey,
		streamKey,
		"token-2",
		"test-event",
		"user1",
		300,
	)

	require.NoError(t, err)
	require.NotNil(t, result2)
	assert.Equal(t, "DUPLICATE", result2.Error, "Duplicate should be rejected")
	assert.Empty(t, result2.StreamID, "Should not return stream ID on duplicate")

	t.Logf("✅ Duplicate rejected: Error=%s", result2.Error)

	// Verify stream has exactly 1 message
	count, err := redisClient.XLen(ctx, streamKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "Stream should have exactly 1 message")

	t.Logf("✅ Stream length verified: %d message", count)
}

func TestLuaExecutor_EnqueueAtomic_Concurrent(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise

	executor := NewLuaExecutor(redisClient, logger)
	ctx := context.Background()

	dedupeKey := "dedupe:concurrent-test"
	streamKey := "stream:event:{test}:user:concurrent"

	// Cleanup
	defer func() {
		redisClient.Del(ctx, dedupeKey, streamKey)
	}()

	// Test: 100 concurrent enqueues with same dedupe key
	const goroutines = 100
	var wg sync.WaitGroup
	var mu sync.Mutex

	successCount := 0
	duplicateCount := 0

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			result, err := executor.EnqueueAtomic(
				ctx,
				dedupeKey,
				streamKey,
				fmt.Sprintf("token-%d", idx),
				"test-event",
				"concurrent-user",
				300,
			)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			mu.Lock()
			if result.Error == "" {
				successCount++
			} else if result.Error == "DUPLICATE" {
				duplicateCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify: Exactly 1 success, 99 duplicates
	assert.Equal(t, 1, successCount, "Should have exactly 1 success")
	assert.Equal(t, 99, duplicateCount, "Should have 99 duplicates")

	// Verify stream has exactly 1 message
	count, _ := redisClient.XLen(ctx, streamKey).Result()
	assert.Equal(t, int64(1), count, "Stream should have exactly 1 message")

	t.Logf("✅ Concurrent test passed: %d success, %d duplicates", successCount, duplicateCount)
}

func TestLuaExecutor_HoldSeatAtomic(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	executor := NewLuaExecutor(redisClient, logger)
	ctx := context.Background()

	seatStatusKey := "seat:status:test-concert"
	holdKey := "hold:seat:A-12"
	inventoryKey := "inventory:test-concert"
	seatID := "A-12"

	// Setup: Set initial inventory and seat status
	redisClient.Set(ctx, inventoryKey, 100, 0)
	redisClient.HSet(ctx, seatStatusKey, seatID, "AVAILABLE")

	// Cleanup
	defer func() {
		redisClient.Del(ctx, seatStatusKey, holdKey, inventoryKey)
	}()

	// Test 1: Hold available seat
	result1, err := executor.HoldSeatAtomic(
		ctx,
		seatStatusKey,
		holdKey,
		inventoryKey,
		seatID,
		"user-123",
		60, // 1 min TTL
	)

	require.NoError(t, err)
	require.NotNil(t, result1)
	assert.True(t, result1.Success, "Hold should succeed")
	assert.Equal(t, int64(99), result1.Remaining, "Inventory should be decremented")

	t.Logf("✅ Seat hold successful: Remaining=%d", result1.Remaining)

	// Test 2: Try to hold same seat again (should fail)
	result2, err := executor.HoldSeatAtomic(
		ctx,
		seatStatusKey,
		holdKey,
		inventoryKey,
		seatID,
		"user-456",
		60,
	)

	require.NoError(t, err)
	require.NotNil(t, result2)
	assert.False(t, result2.Success, "Second hold should fail")
	assert.Equal(t, "SEAT_UNAVAILABLE", result2.Error)

	t.Logf("✅ Duplicate hold rejected: Error=%s", result2.Error)

	// Verify inventory didn't change
	inventory, _ := redisClient.Get(ctx, inventoryKey).Int64()
	assert.Equal(t, int64(99), inventory, "Inventory should remain 99")
}

func TestLuaExecutor_ReleaseSeatAtomic(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	executor := NewLuaExecutor(redisClient, logger)
	ctx := context.Background()

	seatStatusKey := "seat:status:test-concert-2"
	holdKey := "hold:seat:B-23"
	inventoryKey := "inventory:test-concert-2"
	seatID := "B-23"

	// Setup: Hold a seat first
	redisClient.Set(ctx, inventoryKey, 99, 0)
	redisClient.HSet(ctx, seatStatusKey, seatID, "HOLD")
	redisClient.Set(ctx, holdKey, "user-123", 60)

	// Cleanup
	defer func() {
		redisClient.Del(ctx, seatStatusKey, holdKey, inventoryKey)
	}()

	// Test: Release held seat
	result, err := executor.ReleaseSeatAtomic(
		ctx,
		seatStatusKey,
		holdKey,
		inventoryKey,
		seatID,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success, "Release should succeed")
	assert.Equal(t, int64(100), result.Remaining, "Inventory should be restored")

	t.Logf("✅ Seat release successful: Remaining=%d", result.Remaining)

	// Verify seat status is AVAILABLE
	status, _ := redisClient.HGet(ctx, seatStatusKey, seatID).Result()
	assert.Equal(t, "AVAILABLE", status, "Seat should be available")

	// Verify hold key is deleted
	exists, _ := redisClient.Exists(ctx, holdKey).Result()
	assert.Equal(t, int64(0), exists, "Hold key should be deleted")
}

func TestLuaExecutor_SoldOut(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	executor := NewLuaExecutor(redisClient, logger)
	ctx := context.Background()

	seatStatusKey := "seat:status:soldout-test"
	inventoryKey := "inventory:soldout-test"

	// Setup: Set inventory to 0 (sold out)
	redisClient.Set(ctx, inventoryKey, 0, 0)
	redisClient.HSet(ctx, seatStatusKey, "C-1", "AVAILABLE")

	// Cleanup
	defer func() {
		redisClient.Del(ctx, seatStatusKey, inventoryKey)
	}()

	// Test: Try to hold when sold out
	result, err := executor.HoldSeatAtomic(
		ctx,
		seatStatusKey,
		"hold:seat:C-1",
		inventoryKey,
		"C-1",
		"user-789",
		60,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success, "Hold should fail when sold out")
	assert.Equal(t, "SOLD_OUT", result.Error)

	t.Logf("✅ Sold out handled: Error=%s", result.Error)

	// Verify inventory is still 0 (not negative)
	inventory, _ := redisClient.Get(ctx, inventoryKey).Int64()
	assert.Equal(t, int64(0), inventory, "Inventory should remain 0")
}

func BenchmarkLuaExecutor_EnqueueAtomic(b *testing.B) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	executor := NewLuaExecutor(redisClient, logger)
	ctx := context.Background()

	streamKey := "stream:event:{bench}:user:bench"

	// Cleanup
	defer redisClient.Del(ctx, streamKey)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dedupeKey := fmt.Sprintf("dedupe:bench-%d", i)
		_, _ = executor.EnqueueAtomic(
			ctx,
			dedupeKey,
			streamKey,
			fmt.Sprintf("token-%d", i),
			"bench-event",
			"bench-user",
			300,
		)
	}
}
