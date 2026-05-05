package models

import "time"

type User struct {
	ID                string    `json:"id"`
	Email             string    `json:"email"`
	PasswordHash      string    `json:"-"`
	IsVerified        bool      `json:"is_verified"`
	VerificationToken string    `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
}
