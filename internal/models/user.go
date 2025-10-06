package models

import "time"

// User represents a user in the system
type User struct {
	UserID       string    `json:"user_id" dynamodbav:"user_id"`           // Primary Key
	Username     string    `json:"username" dynamodbav:"username"`         // Unique username
	PasswordHash string    `json:"-" dynamodbav:"password_hash"`           // bcrypt hash (never in JSON)
	Email        string    `json:"email" dynamodbav:"email"`               // Email
	DisplayName  string    `json:"display_name" dynamodbav:"display_name"` // Display name
	Role         string    `json:"role" dynamodbav:"role"`                 // user/admin
	CreatedAt    time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" dynamodbav:"updated_at"`
}

// LoginRequest represents login request payload
type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// RegisterRequest represents registration request payload
type RegisterRequest struct {
	Username    string `json:"username" validate:"required,min=3,max=20"`
	Password    string `json:"password" validate:"required,min=6"`
	Email       string `json:"email" validate:"required,email"`
	DisplayName string `json:"display_name" validate:"required"`
}

// AuthResponse represents authentication response
type AuthResponse struct {
	Token       string `json:"token"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	ExpiresIn   int    `json:"expires_in"` // seconds
}
