package main

import (
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

func registerStaticRoutes(router *mux.Router) {
	router.Handle("/sw.js", serviceWorkerHandler("static/js/sw.js")).Methods(http.MethodGet)
	// Containers contain built assets in static/. Native development generates
	// large OCR assets in dist/ while continuing to serve authored JS in static/.
	ocrAssets := "static/vendor/ocr"
	if info, err := os.Stat(ocrAssets); err != nil || !info.IsDir() {
		ocrAssets = "dist/static/vendor/ocr"
	}
	router.PathPrefix("/static/vendor/ocr/").Handler(
		http.StripPrefix("/static/vendor/ocr/", http.FileServer(http.Dir(ocrAssets))),
	)
	router.PathPrefix("/static/").Handler(
		http.StripPrefix("/static/", http.FileServer(http.Dir("static"))),
	)
}

func serviceWorkerHandler(path string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(writer, request, path)
	})
}
