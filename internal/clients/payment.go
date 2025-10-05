package clients

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/traffic-tacos/gateway-api/internal/config"
	commonv1 "github.com/traffic-tacos/proto-contracts/gen/go/common/v1"
	paymentv1 "github.com/traffic-tacos/proto-contracts/gen/go/payment/v1"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type PaymentClient struct {
	conn   *grpc.ClientConn
	client paymentv1.PaymentServiceClient
	logger *logrus.Logger
}

func NewPaymentClient(cfg *config.PaymentAPIConfig, logger *logrus.Logger) (*PaymentClient, error) {
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

	// Create gRPC connection (grpc.NewClient replaces deprecated grpc.Dial)
	// Note: Timeout is handled per-call via context, not at connection level
	conn, err := grpc.NewClient(cfg.GRPCAddress, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to payment service: %w", err)
	}

	// Create gRPC client
	client := paymentv1.NewPaymentServiceClient(conn)

	return &PaymentClient{
		conn:   conn,
		client: client,
		logger: logger,
	}, nil
}

// Close closes the gRPC connection
func (p *PaymentClient) Close() error {
	return p.conn.Close()
}

// CreatePaymentIntent creates a new payment intent
func (p *PaymentClient) CreatePaymentIntent(ctx context.Context, reservationID, userID string, amount *commonv1.Money) (*paymentv1.CreatePaymentIntentResponse, error) {
	req := &paymentv1.CreatePaymentIntentRequest{
		ReservationId: reservationID,
		UserId:        userID,
		Amount:        amount,
	}

	p.logger.WithFields(logrus.Fields{
		"reservation_id": reservationID,
		"user_id":        userID,
		"amount":         amount.Amount,
		"currency":       amount.Currency,
	}).Debug("Creating payment intent via gRPC")

	start := time.Now()
	resp, err := p.client.CreatePaymentIntent(ctx, req)
	latency := time.Since(start)

	p.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Payment intent gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	return resp, nil
}

// GetPaymentStatus retrieves payment status by intent ID
func (p *PaymentClient) GetPaymentStatus(ctx context.Context, paymentIntentID string) (*paymentv1.GetPaymentStatusResponse, error) {
	req := &paymentv1.GetPaymentStatusRequest{
		PaymentIntentId: paymentIntentID,
	}

	p.logger.WithFields(logrus.Fields{
		"payment_intent_id": paymentIntentID,
	}).Debug("Getting payment status via gRPC")

	start := time.Now()
	resp, err := p.client.GetPaymentStatus(ctx, req)
	latency := time.Since(start)

	p.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Payment status gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to get payment status: %w", err)
	}

	return resp, nil
}

// ProcessPayment manually triggers payment processing (for testing)
func (p *PaymentClient) ProcessPayment(ctx context.Context, paymentIntentID string, action string) (*paymentv1.ProcessPaymentResponse, error) {
	req := &paymentv1.ProcessPaymentRequest{
		PaymentIntentId: paymentIntentID,
	}

	p.logger.WithFields(logrus.Fields{
		"payment_intent_id": paymentIntentID,
		"action":            action,
	}).Debug("Processing payment via gRPC")

	start := time.Now()
	resp, err := p.client.ProcessPayment(ctx, req)
	latency := time.Since(start)

	p.logger.WithFields(logrus.Fields{
		"latency_ms": latency.Milliseconds(),
		"success":    err == nil,
	}).Debug("Process payment gRPC call completed")

	if err != nil {
		return nil, fmt.Errorf("failed to process payment: %w", err)
	}

	return resp, nil
}
