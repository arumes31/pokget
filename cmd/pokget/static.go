package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/mux"
)

func registerStaticRoutes(router *mux.Router) {
	router.Handle("/sw.js", serviceWorkerHandler("static/js/sw.js")).Methods(http.MethodGet)
	// Served through a handler so the icon URLs inside the manifest carry the
	// build version; an installed app must not keep stale artwork.
	router.Handle("/static/manifest.json", manifestHandler("static/manifest.json", detectBuildVersion())).
		Methods(http.MethodGet, http.MethodHead)
	// Containers contain built assets in static/. Native development generates
	// large OCR assets in dist/ while continuing to serve authored JS in static/.
	ocrAssets := "static/vendor/ocr"
	if info, err := os.Stat(ocrAssets); err != nil || !info.IsDir() {
		ocrAssets = "dist/static/vendor/ocr"
	}
	ocrHandler := http.StripPrefix("/static/vendor/ocr/", http.FileServer(http.Dir(ocrAssets)))
	router.PathPrefix("/static/vendor/ocr/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		ocrHandler.ServeHTTP(w, r)
	})
	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir("static"))),
	)
}

// manifestHandler serves the web app manifest with every icon URL tagged with
// the build version, so a deploy refreshes the icons an installed app shows.
func manifestHandler(path, version string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, err := os.ReadFile(path)
		if err != nil {
			http.Error(writer, "manifest unavailable", http.StatusInternalServerError)
			return
		}
		var manifest map[string]any
		if err := json.Unmarshal(payload, &manifest); err != nil {
			http.Error(writer, "manifest unavailable", http.StatusInternalServerError)
			return
		}
		versionManifestIcons(manifest, version)
		body, err := json.Marshal(manifest)
		if err != nil {
			http.Error(writer, "manifest unavailable", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-cache")
		_, _ = writer.Write(body)
	})
}

// versionManifestIcons appends the build version to the icon sources of the
// manifest and of its shortcuts.
func versionManifestIcons(manifest map[string]any, version string) {
	versionIcon := func(icon map[string]any) {
		src, ok := icon["src"].(string)
		if !ok || src == "" || strings.Contains(src, "?v=") {
			return
		}
		icon["src"] = src + "?v=" + url.QueryEscape(version)
	}
	iconsIn := func(entries any) {
		list, ok := entries.([]any)
		if !ok {
			return
		}
		for _, entry := range list {
			if icon, ok := entry.(map[string]any); ok {
				versionIcon(icon)
			}
		}
	}
	iconsIn(manifest["icons"])
	if shortcuts, ok := manifest["shortcuts"].([]any); ok {
		for _, entry := range shortcuts {
			if shortcut, ok := entry.(map[string]any); ok {
				iconsIn(shortcut["icons"])
			}
		}
	}
}


func serviceWorkerHandler(path string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(writer, request, path)
	})
}
