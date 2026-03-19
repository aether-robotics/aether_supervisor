package apply

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/pkg/metrics"
)

const retryAfterSeconds = "30"

// Handler triggers local-image apply operations via HTTP.
type Handler struct {
	fn   func(images []string) *metrics.Metric
	Path string
	lock chan bool
}

// New creates a new apply handler.
func New(applyFn func(images []string) *metrics.Metric, applyLock chan bool) *Handler {
	var hLock chan bool
	if applyLock != nil {
		hLock = applyLock

		logrus.WithField("source", "provided").
			Debug("Initialized apply lock from provided channel")
	} else {
		hLock = make(chan bool, 1)
		hLock <- true

		logrus.Debug("Initialized new apply lock channel")
	}

	return &Handler{
		fn:   applyFn,
		Path: "/v1/apply",
		lock: hLock,
	}
}

// Handle processes HTTP apply requests.
func (handle *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API apply request")

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

		logrus.WithFields(logrus.Fields{
			"images":      images,
			"image_count": len(images),
		}).Debug("Extracted images from apply query parameters")
	} else {
		logrus.Debug("No image query parameters provided for apply request")
	}

	logrus.WithFields(logrus.Fields{
		"targeted":    len(images) > 0,
		"image_count": len(images),
	}).Debug("Apply handler attempting to acquire lock")

	if len(images) > 0 {
		select {
		case chanValue := <-handle.lock:
			logrus.WithFields(logrus.Fields{
				"targeted":    true,
				"image_count": len(images),
			}).Debug("Apply handler acquired lock")

			defer func() {
				logrus.WithFields(logrus.Fields{
					"targeted":    true,
					"image_count": len(images),
				}).Debug("Apply handler releasing lock")
				handle.lock <- chanValue
			}()
		case <-r.Context().Done():
			logrus.WithFields(logrus.Fields{
				"targeted":    true,
				"image_count": len(images),
			}).Debug("Apply request cancelled while waiting for lock")
			http.Error(w, "request cancelled", http.StatusServiceUnavailable)

			return
		}

		logrus.WithFields(logrus.Fields{
			"images":      images,
			"image_count": len(images),
		}).Info("Executing targeted apply")
	} else {
		select {
		case chanValue := <-handle.lock:
			logrus.WithField("targeted", false).Debug("Apply handler acquired lock")

			defer func() {
				logrus.WithField("targeted", false).Debug("Apply handler releasing lock")
				handle.lock <- chanValue
			}()
		default:
			logrus.Debug("Skipped apply, another apply already in progress")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":       "another apply is already running",
				"api_version": "v1",
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			}, true)

			return
		}

		logrus.Info("Executing full apply")
	}

	logrus.WithFields(logrus.Fields{
		"targeted":    len(images) > 0,
		"image_count": len(images),
	}).Debug("Apply handler executing apply function")

	startTime := time.Now()
	metric := handle.fn(images)
	duration := time.Since(startTime)

	logrus.WithFields(logrus.Fields{
		"targeted":     len(images) > 0,
		"image_count":  len(images),
		"duration_ms":  duration.Milliseconds(),
		"duration":     duration.String(),
		"scanned":      metric.Scanned,
		"updated":      metric.Updated,
		"failed":       metric.Failed,
		"restarted":    metric.Restarted,
	}).Info("Apply operation completed")

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]any{
			"scanned":   metric.Scanned,
			"updated":   metric.Updated,
			"failed":    metric.Failed,
			"restarted": metric.Restarted,
		},
		"timing": map[string]any{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
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
