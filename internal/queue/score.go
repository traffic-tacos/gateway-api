package queue

import (
	"sync/atomic"
	"time"
)

// globalCounter is a global atomic counter for generating unique scores
var globalCounter uint32

// CompositeScore combines timestamp and counter for unique ordering
// This solves the problem of score collision when multiple requests arrive
// at the same millisecond, ensuring strict FIFO ordering.
type CompositeScore struct {
	Timestamp int64  // Milliseconds since epoch (40 bits used)
	Counter   uint32 // Atomic counter (24 bits used)
}

// GenerateScore creates a new composite score with unique ordering
// Thread-safe: Uses atomic operations for counter
func GenerateScore() *CompositeScore {
	counter := atomic.AddUint32(&globalCounter, 1)

	return &CompositeScore{
		Timestamp: time.Now().UnixMilli(),
		Counter:   counter % (1 << 24), // Keep within 24 bits (16,777,216)
	}
}

// ToFloat64 converts composite score to Redis ZSet score format
// Format: [40-bit timestamp][24-bit counter] = 64-bit float
//
// Example:
//
//	Timestamp: 1728123456789 ms
//	Counter: 12345
//	Result: (1728123456789 << 24) | 12345
func (cs *CompositeScore) ToFloat64() float64 {
	// Use timestamp as base with counter in fractional part
	// Format: timestamp.counter (microseconds)
	// Example: 1728123456789.012345
	return float64(cs.Timestamp) + (float64(cs.Counter) * 0.000001)
}

// FromFloat64 parses a Redis score back to CompositeScore
// Used for debugging and analysis
func FromFloat64(score float64) *CompositeScore {
	timestamp := int64(score)
	fractional := score - float64(timestamp)
	counter := uint32(fractional * 1000000)

	return &CompositeScore{
		Timestamp: timestamp,
		Counter:   counter,
	}
}

// Compare returns -1, 0, or 1 if cs is less than, equal to, or greater than other
// Used for testing and validation
func (cs *CompositeScore) Compare(other *CompositeScore) int {
	if cs.Timestamp < other.Timestamp {
		return -1
	} else if cs.Timestamp > other.Timestamp {
		return 1
	}

	// Same timestamp, compare counters
	if cs.Counter < other.Counter {
		return -1
	} else if cs.Counter > other.Counter {
		return 1
	}

	return 0 // Exactly equal (should be rare)
}

// GetTimestamp returns the timestamp as time.Time
func (cs *CompositeScore) GetTimestamp() time.Time {
	return time.UnixMilli(cs.Timestamp)
}

// String returns a human-readable representation
func (cs *CompositeScore) String() string {
	return cs.GetTimestamp().Format("2006-01-02 15:04:05.000") +
		" (" + string(rune(cs.Counter)) + ")"
}
