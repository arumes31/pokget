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
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"pokget/internal/config"
	"pokget/internal/service"
	"pokget/internal/worker"
)

// newHTTPServer builds the HTTP server for the application.
// BUG-C05 FIX: Use configurable WriteTimeout (default 120s) instead of
// hardcoded 15s which killed scan responses mid-stream during OCR+LLM processing.
func newHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	writeTimeout := time.Duration(cfg.App.WriteTimeout) * time.Second
	return &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}
}

// gracefulShutdown stops background workers, shuts down the HTTP server, and
// drains services before the timeout expires.
func gracefulShutdown(
	srv *http.Server,
	workerCancel context.CancelFunc,
	dataWorker *worker.DataSyncWorker,
	backgroundWorkers *sync.WaitGroup,
	auditSvc *service.AuditService,
	timeout time.Duration,
) {
	slog.Info("Shutting down server...")

	// Stop workers
	workerCancel()
	if dataWorker != nil {
		dataWorker.Stop()
	}

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}
	workersDone := make(chan struct{})
	go func() {
		backgroundWorkers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-ctx.Done():
		slog.Warn("Background workers did not stop before shutdown deadline", "error", ctx.Err())
	}
	if dataWorker != nil {
		if err := dataWorker.Close(); err != nil {
			slog.Warn("Data sync providers did not close cleanly", "error", err)
		}
	}
	if auditSvc != nil {
		// Drain queued audit records while the database pool is still open.
		if err := auditSvc.CloseContext(ctx); err != nil {
			slog.Warn("Audit service did not drain before shutdown deadline", "error", err)
		}
	}

	slog.Info("Server exiting")
}
