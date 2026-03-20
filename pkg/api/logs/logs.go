package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/internal/actions"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

// Handler serves log snapshot requests via HTTP.
type Handler struct {
	fn   func(context.Context, types.ServiceTarget, actions.LogWindow) (*actions.LogsResult, error)
	Path string
}

// New creates a new logs handler.
func New(fn func(context.Context, types.ServiceTarget, actions.LogWindow) (*actions.LogsResult, error)) *Handler {
	return &Handler{
		fn:   fn,
		Path: "/v1/logs",
	}
}

// Handle processes HTTP log snapshot requests.
//
// Query parameters:
//   - name   (required) app/project name
//   - service (optional) specific service within the app
//   - since  (optional) time window: a duration like "2.5h" or "5.6d", or "all".
//     Defaults to "24h" when omitted.
func (handle *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API logs request")

	_, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)

		return
	}

	query := r.URL.Query()

	name := query.Get("name")
	if name == "" {
		http.Error(w, "query parameter 'name' is required", http.StatusBadRequest)

		return
	}

	target := types.ServiceTarget{
		Name:    name,
		Service: query.Get("service"),
	}

	window, err := actions.ParseLogWindow(query.Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if handle.fn == nil {
		http.Error(w, "logs operation is not implemented", http.StatusNotImplemented)

		return
	}

	startTime := time.Now()

	result, err := handle.fn(r.Context(), target, window)
	if err != nil {
		logrus.WithError(err).Error("Logs fetch failed")
		http.Error(w, "Failed to fetch logs", http.StatusInternalServerError)

		return
	}

	if result == nil {
		result = &actions.LogsResult{
			Logs:   []actions.ContainerLogs{},
			Window: window,
		}
	}

	duration := time.Since(startTime)

	writeJSON(w, http.StatusOK, map[string]any{
		"result": map[string]any{
			"logs":        result.Logs,
			"containers":  result.Containers,
			"total_lines": result.TotalLines,
			"window":      result.Window,
		},
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"timing": map[string]any{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	var buf bytes.Buffer

	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		logrus.WithError(err).Error("Failed to encode JSON response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err := w.Write(buf.Bytes()); err != nil {
		logrus.WithError(err).Error("Failed to write response")
	}
}
