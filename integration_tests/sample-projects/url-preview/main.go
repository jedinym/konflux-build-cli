// Command url-preview runs a small web service that fetches a URL and
// returns preview metadata for it (title, description, status, etc).
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"url-preview.samples.konflux-ci.dev/internal/preview"
)

const (
	defaultPort           = "8080"
	defaultRequestTimeout = 10 * time.Second
)

func main() {
	port := getEnv("PORT", defaultPort)
	timeout := getEnvDuration("REQUEST_TIMEOUT_SECONDS", defaultRequestTimeout)

	fetcher := preview.NewFetcher(timeout)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("static")))
	mux.HandleFunc("POST /preview", previewHandler(fetcher))

	addr := ":" + port
	log.Printf("url-preview service listening on %s (request timeout: %s)", addr, timeout)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

type previewRequest struct {
	URL string `json:"url"`
}

func previewHandler(fetcher *preview.Fetcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req previewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
			writeError(w, http.StatusBadRequest, "request body must be JSON with a non-empty \"url\" field")
			return
		}

		result, err := fetcher.Fetch(req.URL)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		log.Printf("invalid %s=%q, using default %s", key, value, fallback)
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
