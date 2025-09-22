package clients

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"
	reservationv1 "github.com/traffic-tacos/proto-contracts/gen/go/reservation/v1"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type ReservationClient struct {
	conn   *grpc.ClientConn
	client reservationv1.ReservationServiceClient
	logger *logrus.Logger
}

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
	} `json:"error"`
}

func NewReservationClient(cfg *config.ReservationAPIConfig, logger *logrus.Logger) (*ReservationClient, error) {
	// Setup gRPC connection options
	var opts []grpc.DialOption

	if cfg.TLSEnabled {
		creds := credentials.NewTLS(&tls.Config{
			ServerName: cfg.GRPCAddress,
		})
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Add timeout
	opts = append(opts, grpc.WithTimeout(cfg.Timeout))

	// Create gRPC connection
	conn, err := grpc.Dial(cfg.GRPCAddress, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to reservation service: %w", err)
	}

	// Create gRPC client
	client := reservationv1.NewReservationServiceClient(conn)

	return &ReservationClient{
		conn:   conn,
		client: client,
		logger: logger,
	}, nil
}

// Close closes the gRPC connection
func (r *ReservationClient) Close() error {
	return r.conn.Close()
}

// CreateReservation creates a new reservation
func (r *ReservationClient) CreateReservation(ctx context.Context, eventID string, seatIDs []string, quantity int32, reservationToken, userID string) (*reservationv1.CreateReservationResponse, error) {
	req := &reservationv1.CreateReservationRequest{
		EventId:          eventID,
		SeatIds:          seatIDs,
		Quantity:         quantity,
		ReservationToken: reservationToken,
		UserId:           userID,
	}

	r.logger.WithFields(logrus.Fields{
		"event_id":          eventID,
		"seat_ids":          seatIDs,
		"quantity":          quantity,
		"reservation_token": reservationToken,
		"user_id":           userID,
	}).Debug("Creating reservation via gRPC")

	start := time.Now()
	resp, err := r.client.CreateReservation(ctx, req)
	latency := time.Since(start)

	r.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Create reservation gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to create reservation: %w", err)
	}

	return resp, nil
}

// GetReservation retrieves a reservation by ID
func (r *ReservationClient) GetReservation(ctx context.Context, reservationID string) (*reservationv1.GetReservationResponse, error) {
	req := &reservationv1.GetReservationRequest{
		ReservationId: reservationID,
	}

	r.logger.WithFields(logrus.Fields{
		"reservation_id": reservationID,
	}).Debug("Getting reservation via gRPC")

	start := time.Now()
	resp, err := r.client.GetReservation(ctx, req)
	latency := time.Since(start)

	r.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Get reservation gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to get reservation: %w", err)
	}

	return resp, nil
}

// ConfirmReservation confirms a reservation
func (r *ReservationClient) ConfirmReservation(ctx context.Context, reservationID, paymentIntentID string) (*reservationv1.ConfirmReservationResponse, error) {
	req := &reservationv1.ConfirmReservationRequest{
		ReservationId:   reservationID,
		PaymentIntentId: paymentIntentID,
	}

	r.logger.WithFields(logrus.Fields{
		"reservation_id":    reservationID,
		"payment_intent_id": paymentIntentID,
	}).Debug("Confirming reservation via gRPC")

	start := time.Now()
	resp, err := r.client.ConfirmReservation(ctx, req)
	latency := time.Since(start)

	r.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Confirm reservation gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to confirm reservation: %w", err)
	}

	return resp, nil
}

// CancelReservation cancels a reservation
func (r *ReservationClient) CancelReservation(ctx context.Context, reservationID string) (*reservationv1.CancelReservationResponse, error) {
	req := &reservationv1.CancelReservationRequest{
		ReservationId: reservationID,
	}

	r.logger.WithFields(logrus.Fields{
		"reservation_id": reservationID,
	}).Debug("Canceling reservation via gRPC")

	start := time.Now()
	resp, err := r.client.CancelReservation(ctx, req)
	latency := time.Since(start)

	r.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Cancel reservation gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to cancel reservation: %w", err)
	}

	return resp, nil
}

