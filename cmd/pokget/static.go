package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func registerStaticRoutes(router *mux.Router) {
	router.Handle("/sw.js", serviceWorkerHandler("static/js/sw.js")).Methods(http.MethodGet)
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
