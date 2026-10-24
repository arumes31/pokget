package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"golang.org/x/crypto/bcrypt"
)

func TestProxyMiddlewareRejectsSpoofedHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})

	tests := []struct {
		name           string
		trustedProxies string
		remoteAddr     string
		headers        map[string]string
		want           string
	}{
		{
			name:       "untrusted peer cannot set X-Forwarded-For",
			remoteAddr: "203.0.113.9:4000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.1"},
			want:       "203.0.113.9:4000",
		},
		{
			name:       "untrusted peer cannot set X-Real-IP",
			remoteAddr: "203.0.113.9:4000",
			headers:    map[string]string{"X-Real-IP": "198.51.100.1"},
			want:       "203.0.113.9:4000",
		},
		{
			name:       "X-Forwarded-For wins over client-supplied X-Real-IP",
			remoteAddr: "172.18.0.2:4000",
			headers:    map[string]string{"X-Real-IP": "198.51.100.1", "X-Forwarded-For": "198.51.100.7"},
			want:       "198.51.100.7",
		},
		{
			name:       "trusted hops are skipped from the right",
			remoteAddr: "172.18.0.2:4000",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.7, 10.0.0.3"},
			want:       "198.51.100.7",
		},
		{
			name:       "invalid hop is ignored",
			remoteAddr: "172.18.0.2:4000",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, not-an-ip"},
			want:       "172.18.0.2:4000",
		},
		{
			name:       "invalid X-Real-IP is ignored",
			remoteAddr: "172.18.0.2:4000",
			headers:    map[string]string{"X-Real-IP": "<script>"},
			want:       "172.18.0.2:4000",
		},
		{
			name:           "custom trusted proxy list",
			trustedProxies: "203.0.113.9, 2001:db8::/32",
			remoteAddr:     "203.0.113.9:4000",
			headers:        map[string]string{"X-Forwarded-For": "198.51.100.7"},
			want:           "198.51.100.7",
		},
		{
			name:           "custom list replaces the private defaults",
			trustedProxies: "203.0.113.9",
			remoteAddr:     "10.0.0.5:4000",
			headers:        map[string]string{"X-Forwarded-For": "198.51.100.7"},
			want:           "10.0.0.5:4000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRUST_PROXY", "true")
			t.Setenv("TRUST_CLOUDFLARE", "false")
			t.Setenv("TRUSTED_PROXIES", tc.trustedProxies)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			ProxyMiddleware(next).ServeHTTP(httptest.NewRecorder(), req)

			if req.RemoteAddr != tc.want {
				t.Errorf("RemoteAddr = %q, want %q", req.RemoteAddr, tc.want)
			}
		})
	}
}

func TestLimiterKeyGroupsIPv6ByPrefix(t *testing.T) {
	a := limiterKey("2001:db8:1:2:aaaa::1")
	b := limiterKey("2001:db8:1:2:bbbb::2")
	if a != b || a != "2001:db8:1:2::/64" {
		t.Errorf("keys = %q, %q; want shared /64 key", a, b)
	}
	if limiterKey("2001:db8:1:3::1") == a {
		t.Error("different /64 prefixes must not share a limiter")
	}
	if got := limiterKey("198.51.100.4"); got != "198.51.100.4" {
		t.Errorf("IPv4 key = %q", got)
	}
}

func TestLoginFailureLimiter(t *testing.T) {
	email := "Limited@Example.com"
	t.Cleanup(func() { ResetLoginFailures(email) })

	for range maxLoginFailures {
		if !LoginAllowed(email) {
			t.Fatal("account locked before reaching the failure limit")
		}
		RecordLoginFailure(email)
	}
	if LoginAllowed("limited@example.com") {
		t.Fatal("account not locked after repeated failures")
	}
	ResetLoginFailures(email)
	if !LoginAllowed(email) {
		t.Fatal("reset did not unlock the account")
	}
}

func TestValidateNewPassword(t *testing.T) {
	if err := ValidateNewPassword(strings.Repeat("a", MinPasswordLength-1)); err != ErrPasswordTooShort {
		t.Errorf("short password error = %v", err)
	}
	// Characters, not bytes, are counted.
	if err := ValidateNewPassword(strings.Repeat("界", MinPasswordLength)); err != nil {
		t.Errorf("12-character password rejected: %v", err)
	}
}

func TestHashPasswordUsesReducedCost(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if cost, _ := bcrypt.Cost([]byte(hash)); cost != bcryptCost {
		t.Errorf("cost = %d, want %d", cost, bcryptCost)
	}
}

func TestSessionCookieIsEncrypted(t *testing.T) {
	store := InitStore("0123456789abcdef0123456789abcdef")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	session, _ := store.Get(req, "session")
	session.Values["user_id"] = "user-visible-id"
	rr := httptest.NewRecorder()
	if err := session.Save(req, rr); err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0].Value
	if strings.Contains(cookie, "user-visible-id") {
		t.Fatal("cookie value contains the plaintext user ID")
	}
}

func TestRevokeSessionBumpsCurrentVersionOnly(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	session, _ := Store.Get(req, "session")
	session.Values["user_id"] = "user-1"
	session.Values["session_version"] = int64(4)
	rr := httptest.NewRecorder()
	if err := session.Save(req, rr); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Cookie", rr.Header().Get("Set-Cookie"))

	mock.ExpectExec("UPDATE users SET session_version").
		WithArgs("user-1", int64(4)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := RevokeSession(database, req); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// No cookie: nothing to revoke and no query.
	if err := RevokeSession(database, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil {
		t.Fatal(err)
	}
}
