package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// SlidingWindowMetrics implements advanced ETA calculation with multiple time windows
type SlidingWindowMetrics struct {
	redisClient *redis.Client
	eventID     string
	logger      *logrus.Logger
}

// TimeWindow represents a time window for rate calculation
type TimeWindow struct {
	Duration time.Duration
	Weight   float64
}

// NewSlidingWindowMetrics creates a new sliding window metrics tracker
func NewSlidingWindowMetrics(redis *redis.Client, eventID string, logger *logrus.Logger) *SlidingWindowMetrics {
	return &SlidingWindowMetrics{
		redisClient: redis,
		eventID:     eventID,
		logger:      logger,
	}
}

// GetWeightedAdmissionRate calculates weighted average across multiple time windows
// This provides more accurate predictions by considering both recent and historical data
func (s *SlidingWindowMetrics) GetWeightedAdmissionRate(ctx context.Context) float64 {
	now := time.Now()

	// Define time windows with weights
	// Recent data gets higher weight
	windows := []TimeWindow{
		{Duration: 1 * time.Minute, Weight: 0.5},  // 50% weight on last 1 minute
		{Duration: 5 * time.Minute, Weight: 0.3},  // 30% weight on last 5 minutes
		{Duration: 15 * time.Minute, Weight: 0.2}, // 20% weight on last 15 minutes
	}

	var weightedSum float64
	var totalWeight float64

	for _, window := range windows {
		rate := s.getAdmissionRateForWindow(ctx, now, window.Duration)
		if rate > 0 {
			weightedSum += rate * window.Weight
			totalWeight += window.Weight

			s.logger.WithFields(logrus.Fields{
				"window":   window.Duration.String(),
				"rate":     rate,
				"weight":   window.Weight,
				"weighted": rate * window.Weight,
			}).Debug("Window rate calculated")
		}
	}

	// If no data available, return 0
	if totalWeight == 0 {
		return 0
	}

	// Normalize by actual total weight (in case some windows had no data)
	weightedRate := weightedSum / totalWeight

	s.logger.WithFields(logrus.Fields{
		"event_id":      s.eventID,
		"weighted_rate": weightedRate,
		"total_weight":  totalWeight,
	}).Debug("Weighted admission rate calculated")

	return weightedRate
}

// getAdmissionRateForWindow calculates admission rate for a specific time window
func (s *SlidingWindowMetrics) getAdmissionRateForWindow(ctx context.Context, now time.Time, duration time.Duration) float64 {
	key := fmt.Sprintf("metrics:admission:%s", s.eventID)

	start := now.Add(-duration).Unix()
	end := now.Unix()

	count, err := s.redisClient.ZCount(ctx, key,
		fmt.Sprintf("%d", start),
		fmt.Sprintf("%d", end)).Result()

	if err != nil || count == 0 {
		return 0
	}

	// Calculate rate: admissions per second
	rate := float64(count) / duration.Seconds()

	return rate
}

// CalculateAdvancedETA calculates ETA using sliding window with hourly patterns
func (s *SlidingWindowMetrics) CalculateAdvancedETA(ctx context.Context, position int) int {
	// Get weighted admission rate
	rate := s.GetWeightedAdmissionRate(ctx)

	// Apply hourly weight adjustment
	hourWeight := s.getHourlyWeight(time.Now().Hour())
	adjustedRate := rate * hourWeight

	s.logger.WithFields(logrus.Fields{
		"position":      position,
		"base_rate":     rate,
		"hour_weight":   hourWeight,
		"adjusted_rate": adjustedRate,
	}).Debug("Advanced ETA calculation")

	// Fallback if no rate data
	if adjustedRate == 0 {
		s.logger.Warn("No admission rate data, using fallback")
		return position * 2
	}

	// Calculate ETA with 10% safety buffer
	eta := float64(position) / adjustedRate * 1.1

	// Clamp between 1 and 600 seconds
	if eta < 1 {
		return 1
	} else if eta > 600 {
		return 600
	}

	return int(eta)
}

// getHourlyWeight returns traffic pattern weight based on hour of day
// This accounts for peak and off-peak hours
func (s *SlidingWindowMetrics) getHourlyWeight(hour int) float64 {
	// Traffic pattern weights (based on typical e-commerce patterns)
	weights := map[int]float64{
		0:  0.3, // Late night - very low traffic
		1:  0.2, // Early morning - lowest traffic
		2:  0.2,
		3:  0.2,
		4:  0.3,
		5:  0.4,
		6:  0.6, // Morning - traffic starts
		7:  0.8,
		8:  1.0, // Work hours - normal traffic
		9:  1.2, // Morning peak
		10: 1.3,
		11: 1.4,
		12: 1.5, // Lunch time - peak
		13: 1.4,
		14: 1.2,
		15: 1.1,
		16: 1.0,
		17: 1.2,
		18: 1.8, // Evening - highest peak
		19: 2.0, // Prime time
		20: 1.8,
		21: 1.5,
		22: 1.2,
		23: 0.8, // Late evening
	}

	if weight, ok := weights[hour]; ok {
		return weight
	}

	return 1.0 // Default weight
}

// GetETAConfidence returns confidence level of ETA prediction (0.0 to 1.0)
func (s *SlidingWindowMetrics) GetETAConfidence(ctx context.Context) float64 {
	now := time.Now()

	// Check data availability across windows
	count1min := s.getCountForWindow(ctx, now, 1*time.Minute)
	count5min := s.getCountForWindow(ctx, now, 5*time.Minute)
	count15min := s.getCountForWindow(ctx, now, 15*time.Minute)

	// Calculate confidence based on data availability
	var confidence float64

	if count15min >= 30 {
		confidence = 1.0 // Very high confidence
	} else if count5min >= 10 {
		confidence = 0.8 // High confidence
	} else if count1min >= 3 {
		confidence = 0.6 // Medium confidence
	} else if count1min >= 1 {
		confidence = 0.4 // Low confidence
	} else {
		confidence = 0.2 // Very low confidence (fallback mode)
	}

	s.logger.WithFields(logrus.Fields{
		"confidence":  confidence,
		"count_1min":  count1min,
		"count_5min":  count5min,
		"count_15min": count15min,
	}).Debug("ETA confidence calculated")

	return confidence
}

// getCountForWindow returns admission count for a time window
func (s *SlidingWindowMetrics) getCountForWindow(ctx context.Context, now time.Time, duration time.Duration) int64 {
	key := fmt.Sprintf("metrics:admission:%s", s.eventID)

	start := now.Add(-duration).Unix()
	end := now.Unix()

	count, err := s.redisClient.ZCount(ctx, key,
		fmt.Sprintf("%d", start),
		fmt.Sprintf("%d", end)).Result()

	if err != nil {
		return 0
	}

	return count
}

// GetDetailedMetrics returns comprehensive metrics for monitoring
type DetailedMetrics struct {
	Position     int     `json:"position"`
	ETA          int     `json:"eta_sec"`
	Confidence   float64 `json:"confidence"`
	Rate1Min     float64 `json:"rate_1min"`
	Rate5Min     float64 `json:"rate_5min"`
	Rate15Min    float64 `json:"rate_15min"`
	WeightedRate float64 `json:"weighted_rate"`
	HourWeight   float64 `json:"hour_weight"`
	Count1Min    int64   `json:"count_1min"`
	Count5Min    int64   `json:"count_5min"`
	Count15Min   int64   `json:"count_15min"`
}

func (s *SlidingWindowMetrics) GetDetailedMetrics(ctx context.Context, position int) *DetailedMetrics {
	now := time.Now()

	metrics := &DetailedMetrics{
		Position:     position,
		Rate1Min:     s.getAdmissionRateForWindow(ctx, now, 1*time.Minute),
		Rate5Min:     s.getAdmissionRateForWindow(ctx, now, 5*time.Minute),
		Rate15Min:    s.getAdmissionRateForWindow(ctx, now, 15*time.Minute),
		WeightedRate: s.GetWeightedAdmissionRate(ctx),
		HourWeight:   s.getHourlyWeight(now.Hour()),
		Count1Min:    s.getCountForWindow(ctx, now, 1*time.Minute),
		Count5Min:    s.getCountForWindow(ctx, now, 5*time.Minute),
		Count15Min:   s.getCountForWindow(ctx, now, 15*time.Minute),
		Confidence:   s.GetETAConfidence(ctx),
		ETA:          s.CalculateAdvancedETA(ctx, position),
	}

	return metrics
}
