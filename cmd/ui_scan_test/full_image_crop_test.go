package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"pokget/internal/middleware"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestFullImageCropUsesGuideControls(t *testing.T) {
	ctx := mobileScannerRegressionPage(t)
	fixture, err := filepath.Abs(filepath.Join("..", "..", "static", "img", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.SetUploadFiles(fileInputSelector, []string{fixture}, chromedp.ByQuery),
		chromedp.Poll(`Boolean(Alpine.$data(document.querySelector('.scanner-shell')).previewURL)`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if err := includeFullImageWithGuides(ctx); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Poll(`(() => {const state=Alpine.$data(document.querySelector('.scanner-shell'));return state.lines.left===0&&state.lines.top===0&&state.lines.right===100&&state.lines.bottom===100&&Boolean(state.previewURL)&&!state.scanning})()`, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestScanFixtureUsesExistingAppShell(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	pages := newMobileShellServer(t)
	defer pages.Close()
	var shellRequests, scanRequests atomic.Int32
	var receivedDigest atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			shellRequests.Add(1)
		}
		if r.URL.Path == "/api/scan" {
			scanRequests.Add(1)
			file, _, err := r.FormFile("card_image")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer file.Close()
			upload, err := io.ReadAll(file)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			receivedDigest.Store(fmt.Sprintf("%x", sha256.Sum256(upload)))
			http.Error(w, "Synthetic stop after upload", http.StatusBadRequest)
			return
		}
		pages.Config.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 30*time.Second)
	defer cancel()
	capture := newScanArtifactCapture(ctx)
	if err := chromedp.Run(ctx, network.Enable(), chromedp.EmulateViewport(390, 844), chromedp.Navigate(server.URL+"/"), chromedp.WaitVisible("#main-content .app-page", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "static", "img", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanFixture(ctx, config{baseURL: server.URL, fixture: fixture, timeout: 30 * time.Second, game: "pokemon", language: "eng", fullImage: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error == "" || scanRequests.Load() != 1 {
		t.Fatalf("expected the synthetic stop after one uploaded request; requests=%d, result=%+v", scanRequests.Load(), result)
	}
	if shellRequests.Load() != 1 {
		t.Errorf("scanner flow reloaded the established app shell: %d total shell requests, want1", shellRequests.Load())
	}
	artifacts := t.TempDir()
	if err := capture.save(ctx, artifacts, result); err != nil {
		t.Fatal(err)
	}
	upload, err := os.ReadFile(filepath.Join(artifacts, "upload-crop.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(upload)) != receivedDigest.Load() {
		t.Error("saved crop differs from the actual received upload")
	}
	response, err := os.ReadFile(filepath.Join(artifacts, "scan-response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "Synthetic stop after upload\n" {
		t.Errorf("captured response body = %q", response)
	}
}

func TestScannerPreviewRendersWithProductionSecurityPolicy(t *testing.T) {
	chrome := mobileTestChromePath()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	pages := newMobileShellServer(t)
	defer pages.Close()
	server := httptest.NewServer(middleware.SecurityHeadersMiddleware(pages.Config.Handler))
	defer server.Close()
	ctx, cancel := context.WithTimeout(newHeadlessBrowserContext(t, chrome), 20*time.Second)
	defer cancel()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "static", "img", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(server.URL+"/?view=scan"),
		chromedp.WaitReady(fileInputSelector, chromedp.ByQuery),
		chromedp.SetUploadFiles(fileInputSelector, []string{fixture}, chromedp.ByQuery),
		chromedp.Poll(`Boolean(Alpine.$data(document.querySelector('.scanner-shell')).previewURL)`, nil),
		chromedp.Poll(`(() => {const image=document.querySelector('.scanner-frame img');return image.complete && image.naturalWidth>0 && image.getBoundingClientRect().height>0})()`, nil, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		t.Fatalf("uploaded photo must decode and render under the production CSP: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.createImageBitmap=undefined; Alpine.$data(document.querySelector('.scanner-shell')).clearPreview()`, nil),
		chromedp.SetUploadFiles(fileInputSelector, []string{fixture}, chromedp.ByQuery),
		chromedp.Poll(`(() => {const state=Alpine.$data(document.querySelector('.scanner-shell'));const image=document.querySelector('.scanner-frame img');return state.previewURL.startsWith('data:image/jpeg;base64,')&&image.complete&&image.naturalWidth>0})()`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("Image decoding fallback must also respect the unchanged production CSP: %v", err)
	}
}
