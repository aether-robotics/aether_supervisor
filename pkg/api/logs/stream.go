package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/internal/actions"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

// StreamHandler serves live log stream requests via Server-Sent Events (SSE).
type StreamHandler struct {
	fn   func(context.Context, types.ServiceTarget, actions.LogWindow, chan<- actions.LogLine) error
	Path string
}

// NewStream creates a new log streaming handler.
func NewStream(fn func(context.Context, types.ServiceTarget, actions.LogWindow, chan<- actions.LogLine) error) *StreamHandler {
	return &StreamHandler{
		fn:   fn,
		Path: "/v1/logs/stream",
	}
}

// Handle processes HTTP log stream requests using Server-Sent Events.
//
// Each SSE event carries a JSON-encoded LogLine:
//
//	data: {"container":"...","service":"...","stream":"stdout","line":"..."}
//
// A final "done" event is emitted when all container streams have ended.
//
// Query parameters:
//   - name    (required) app/project name
//   - service (optional) specific service within the app
//   - since   (optional) time window: a duration like "2.5h" or "5.6d", or "all".
//     Defaults to "24h" when omitted.
func (h *StreamHandler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API log stream request")

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

	if h.fn == nil {
		http.Error(w, "log streaming is not implemented", http.StatusNotImplemented)

		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported by server", http.StatusInternalServerError)

		return
	}

	// Disable the server-level write timeout so the SSE connection stays open
	// beyond the default 10-minute window.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		logrus.WithError(err).Debug("Could not clear write deadline for SSE connection")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if present
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	out := make(chan actions.LogLine, 64)

	go func() {
		if err := h.fn(r.Context(), target, window, out); err != nil {
			logrus.WithError(err).Error("Log stream error")
		}
	}()

	for line := range out {
		data, err := json.Marshal(line)
		if err != nil {
			logrus.WithError(err).Warn("Failed to marshal log line")

			continue
		}

		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			// Client disconnected mid-stream.
			logrus.WithError(err).Debug("Client disconnected from log stream")

			return
		}

		flusher.Flush()
	}

	// All container streams ended cleanly — tell the client it's done.
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
