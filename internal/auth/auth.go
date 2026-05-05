package auth

import (
	"context"
	"net/http"
	"os"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

var Store *sessions.CookieStore

func init() {
	key := os.Getenv("SESSION_KEY")
	// For local development or tests, allow a default key if not explicitly set
	// but still enforce length in production environments
	if key == "" {
		key = "temporary-insecure-dev-key-32-chars-long" 
	}
	if len(key) < 32 {
		panic("SESSION_KEY must be at least 32 characters long")
	}
	Store = sessions.NewCookieStore([]byte(key))
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// UserContextKey is the key for the user ID in the context
type UserContextKey struct{}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := Store.Get(r, "session")
		if err != nil {
			// If session is invalid/tampered, clear it and redirect to auth
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		userID, ok := session.Values["user_id"].(string)
		if !ok || userID == "" {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
