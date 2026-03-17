package check

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const retryAfterSeconds = "30"

// ServiceUpdate describes a service with an upstream image update available.
type ServiceUpdate struct {
	Name           string `json:"name"`
	Image          string `json:"image"`
	CurrentDigest  string `json:"current_digest,omitempty"`
	UpstreamDigest string `json:"upstream_digest,omitempty"`
}

// Result summarizes a check request.
type Result struct {
	Scanned   int             `json:"scanned"`
	Updatable int             `json:"updatable"`
	Failed    int             `json:"failed"`
	Services  []ServiceUpdate `json:"services"`
}

// Handler triggers update checks via HTTP.
type Handler struct {
	fn   func(images []string) (*Result, error)
	Path string
	lock chan bool
}

// New creates a new check handler.
func New(checkFn func(images []string) (*Result, error), checkLock chan bool) *Handler {
	var hLock chan bool
	if checkLock != nil {
		hLock = checkLock
	} else {
		hLock = make(chan bool, 1)
		hLock <- true
	}

	return &Handler{
		fn:   checkFn,
		Path: "/v1/check",
		lock: hLock,
	}
}

// Handle processes HTTP check requests.
func (handle *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API check request")

	_, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)

		return
	}

	var images []string
	imageQueries, found := r.URL.Query()["image"]
	if found {
		for _, image := range imageQueries {
			images = append(images, strings.Split(image, ",")...)
		}
	}

	if len(images) > 0 {
		select {
		case chanValue := <-handle.lock:
			defer func() {
				handle.lock <- chanValue
			}()
		case <-r.Context().Done():
			http.Error(w, "request cancelled", http.StatusServiceUnavailable)

			return
		}
	} else {
		select {
		case chanValue := <-handle.lock:
			defer func() {
				handle.lock <- chanValue
			}()
		default:
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":       "another check is already running",
				"api_version": "v1",
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			}, true)

			return
		}
	}

	if handle.fn == nil {
		http.Error(w, "check operation is not implemented", http.StatusNotImplemented)

		return
	}

	startTime := time.Now()
	result, err := handle.fn(images)
	if err != nil {
		logrus.WithError(err).Error("Check execution failed")
		http.Error(w, "Failed to execute check", http.StatusInternalServerError)

		return
	}
	if result == nil {
		result = &Result{}
	}
	if result.Services == nil {
		result.Services = []ServiceUpdate{}
	}
	duration := time.Since(startTime)

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]any{
			"scanned":   result.Scanned,
			"updatable": result.Updatable,
			"failed":    result.Failed,
		},
		"services":    result.Services,
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"timing": map[string]any{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
	}, false)
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any, retry bool) {
	var buf bytes.Buffer

	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		logrus.WithError(err).Error("Failed to encode JSON response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	if retry {
		w.Header().Set("Retry-After", retryAfterSeconds)
	}
	w.WriteHeader(status)

	if _, err := w.Write(buf.Bytes()); err != nil {
		logrus.WithError(err).Error("Failed to write response")
	}
}
