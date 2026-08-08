// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"database/sql"
	"net/http"

	"pokget/internal/auth"
	"pokget/internal/config"
	"pokget/internal/handlers"
	"pokget/internal/middleware"

	"github.com/gorilla/mux"
)

// buildRouter wires all HTTP routes: health, static assets, scan API, public
// web routes, protected routes, and admin routes.
func buildRouter(cfg *config.Config, database *sql.DB, h *handlers.Handler) *mux.Router {
	r := mux.NewRouter()
	useGlobalMiddleware(r)
	registerHealthRoutes(r, database)

	// CSRF Protection
	csrfKey := deriveKey(cfg.Auth.SessionKey, "pokget:csrf:auth")
	csrfMiddleware := newCSRFMiddleware(csrfKey, cfg.App.SecureCookies)

	// Static files (Exempt from CSRF)
	registerStaticRoutes(r)

	registerScanRoute(
		r,
		database,
		csrfMiddleware,
		cfg.Scan.OCRPoolSize,
		http.HandlerFunc(h.APIScan),
	)

	// Web Routes (Protected by CSRF + 1MB MaxBytes limit)
	web := r.NewRoute().Subrouter()
	web.Use(middleware.MaxBytesMiddleware) // 1MB limit for form submissions
	web.Use(csrfMiddleware)

	// Public Web Routes
	web.Handle("/", auth.Middleware(database)(http.HandlerFunc(h.Index))).Methods("GET")
	web.HandleFunc("/auth", h.Auth).Methods("GET")
	web.HandleFunc("/auth/register", h.Register).Methods("POST")
	web.HandleFunc("/auth/login", h.Login).Methods("POST")
	web.HandleFunc("/auth/resend", h.ResendVerification).Methods("POST")
	web.HandleFunc("/auth/confirm", h.ConfirmEmail).Methods("GET")
	web.HandleFunc("/auth/confirm", h.ProcessConfirmEmail).Methods("POST")
	web.HandleFunc("/auth/logout", h.Logout).Methods("POST")
	web.HandleFunc("/vault/{slug}", h.PublicVault).Methods("GET")
	web.HandleFunc("/errors", h.ErrorDatabase).Methods("GET")

	// Protected Routes (Require Authentication + CSRF)
	protected := web.PathPrefix("/").Subrouter()
	protected.Use(auth.Middleware(database))
	protected.Use(canonicalFragmentRedirectMiddleware)
	protected.HandleFunc("/dashboard", h.Dashboard).Methods("GET")
	protected.HandleFunc("/centering", h.Centering).Methods("GET")
	protected.HandleFunc("/binders", h.Binders).Methods("GET")
	protected.HandleFunc("/binders/create", h.CreateBinder).Methods("POST")
	protected.HandleFunc("/binders/auto-name", h.AutoNameBinder).Methods("POST")
	protected.HandleFunc("/binders/{id}", h.BinderDetail).Methods("GET")
	protected.HandleFunc("/trade", h.Trade).Methods("GET")
	protected.HandleFunc("/settings", h.Settings).Methods("GET", "POST")
	protected.HandleFunc("/settings/change-password", h.ChangePassword).Methods("POST") // BUG-M11: Route for password change with session invalidation
	protected.HandleFunc("/settings/public-profile", h.UpdatePublicProfile).Methods("POST")
	protected.HandleFunc("/portfolio/add", h.AddCardToPortfolio).Methods("POST")
	protected.HandleFunc("/portfolio/edit", h.EditPortfolioItem).Methods("POST")
	protected.HandleFunc("/portfolio/delete", h.DeletePortfolioItem).Methods("POST", "DELETE") // BUG-H02: Delete with ownership check
	protected.HandleFunc("/portfolio/binder", h.UpdatePortfolioBinder).Methods("POST")
	protected.HandleFunc("/portfolio/toggle-visibility", h.ToggleVisibility).Methods("POST")
	protected.HandleFunc("/wantlist", h.Wantlist).Methods("GET")
	protected.HandleFunc("/wantlist/add", h.AddToWantlist).Methods("POST")
	protected.HandleFunc("/wantlist/edit", h.UpdateWantlistItem).Methods("POST")
	protected.HandleFunc("/wantlist/delete", h.DeleteWantlistItem).Methods("POST", "DELETE")
	protected.HandleFunc("/errors/submit", h.SubmitError).Methods("POST")
	protected.HandleFunc("/api/gamification/heartbeat", h.Heartbeat).Methods("POST")
	protected.HandleFunc("/api/portfolio/add", h.AddCardToPortfolio).Methods("POST")

	// Admin Routes (Require Authentication + Admin Role + CSRF)
	admin := protected.PathPrefix("/api/admin").Subrouter()
	admin.Use(auth.AdminMiddleware(database))
	admin.HandleFunc("/refresh-cache", h.RefreshCache).Methods("POST")

	return r
}
