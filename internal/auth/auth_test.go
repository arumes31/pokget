package auth

import (
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	password := "SecretPassword123"
	
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if hash == password {
		t.Error("Hash should not be equal to plain password")
	}

	if !CheckPasswordHash(password, hash) {
		t.Error("Password check should succeed with correct password")
	}

	if CheckPasswordHash("wrongpassword", hash) {
		t.Error("Password check should fail with incorrect password")
	}
}
