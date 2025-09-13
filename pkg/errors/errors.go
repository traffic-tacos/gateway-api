package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode represents standardized error codes
type ErrorCode string

const (
	CodeUnauthenticated     ErrorCode = "UNAUTHENTICATED"
	CodeForbidden           ErrorCode = "FORBIDDEN"
	CodeRateLimited         ErrorCode = "RATE_LIMITED"
	CodeIdempotencyRequired ErrorCode = "IDEMPOTENCY_REQUIRED"
	CodeIdempotencyConflict ErrorCode = "IDEMPOTENCY_CONFLICT"
	CodeUpstreamTimeout     ErrorCode = "UPSTREAM_TIMEOUT"
	CodeUpstreamUnavailable ErrorCode = "UPSTREAM_UNAVAILABLE"
	CodeBadRequest          ErrorCode = "BAD_REQUEST"
	CodeInternalError       ErrorCode = "INTERNAL_ERROR"
)

// HTTPStatusMap maps error codes to HTTP status codes
var HTTPStatusMap = map[ErrorCode]int{
	CodeUnauthenticated:     http.StatusUnauthorized,
	CodeForbidden:           http.StatusForbidden,
	CodeRateLimited:         http.StatusTooManyRequests,
	CodeIdempotencyRequired: http.StatusBadRequest,
	CodeIdempotencyConflict: http.StatusConflict,
	CodeUpstreamTimeout:     http.StatusGatewayTimeout,
	CodeUpstreamUnavailable: http.StatusServiceUnavailable,
	CodeBadRequest:          http.StatusBadRequest,
	CodeInternalError:       http.StatusInternalServerError,
}

// ErrorResponse represents the standardized error response structure
type ErrorResponse struct {
	Error struct {
		Code    ErrorCode `json:"code"`
		Message string    `json:"message"`
		TraceID string    `json:"trace_id,omitempty"`
	} `json:"error"`
}

// AppError represents an application error with code and message
type AppError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause
func (e *AppError) Unwrap() error {
	return e.Cause
}

// NewAppError creates a new AppError
func NewAppError(code ErrorCode, message string, cause error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// NewAppErrorf creates a new AppError with formatted message
func NewAppErrorf(code ErrorCode, cause error, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}

// ToErrorResponse converts AppError to ErrorResponse
func (e *AppError) ToErrorResponse(traceID string) ErrorResponse {
	resp := ErrorResponse{}
	resp.Error.Code = e.Code
	resp.Error.Message = e.Message
	resp.Error.TraceID = traceID
	return resp
}

// HTTPStatus returns the HTTP status code for this error
func (e *AppError) HTTPStatus() int {
	if status, exists := HTTPStatusMap[e.Code]; exists {
		return status
	}
	return http.StatusInternalServerError
}

// IsRetryable checks if the error is retryable
func (e *AppError) IsRetryable() bool {
	switch e.Code {
	case CodeUpstreamTimeout, CodeUpstreamUnavailable:
		return true
	default:
		return false
	}
}

// WrapError wraps an error with additional context
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*AppError); ok {
		return NewAppError(appErr.Code, message, err)
	}
	return NewAppError(CodeInternalError, message, err)
}
