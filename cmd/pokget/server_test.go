package main

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"pokget/internal/service"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNewHTTPServer(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	srv := newHTTPServer(cfg, handler)

	if srv.Addr != ":18066" {
		t.Errorf("Addr = %q, want %q", srv.Addr, ":18066")
	}
	if srv.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want 15s", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 120*time.Second {
		t.Errorf("WriteTimeout = %v, want 120s", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", srv.IdleTimeout)
	}
	if srv.Handler == nil {
		t.Error("Handler is nil")
	}
}

func TestGracefulShutdown(t *testing.T) {
	database, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := newTestConfig(t)
	dataWorker, err := newDataSyncWorker(database, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	auditSvc := service.NewAuditService(database)

	_, workerCancel := context.WithCancel(context.Background())
	var backgroundWorkers sync.WaitGroup
	backgroundWorkers.Add(1)
	go func() {
		defer backgroundWorkers.Done()
	}()

	// Shutting down a server that never started is a no-op; workers and
	// services drain well before the deadline.
	gracefulShutdown(&http.Server{}, workerCancel, dataWorker, &backgroundWorkers, auditSvc, 5*time.Second)
}

func TestGracefulShutdownWorkerDeadline(t *testing.T) {
	var backgroundWorkers sync.WaitGroup
	backgroundWorkers.Add(1) // worker never finishes: force the deadline branch

	start := time.Now()
	gracefulShutdown(&http.Server{}, func() {}, nil, &backgroundWorkers, nil, 100*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gracefulShutdown blocked for %v despite the deadline", elapsed)
	}
}
