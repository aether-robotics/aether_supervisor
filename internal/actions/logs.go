package actions

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

// LogWindow is a duration string specifying how far back in time to retrieve logs.
// Accepts Go duration strings (e.g. "30m", "2.5h") plus a "d" suffix for days
// (e.g. "1d", "5.6d"), and the special value "all" to retrieve all available logs.
type LogWindow string

const (
	// LogWindowAll is a special value meaning no time restriction — fetch all available logs.
	LogWindowAll LogWindow = "all"
)

// ContainerLogs holds the log output for a single container.
type ContainerLogs struct {
	Container string   `json:"container"`
	Service   string   `json:"service,omitempty"`
	Stdout    []string `json:"stdout"`
	Stderr    []string `json:"stderr"`
}

// LogsResult summarizes a log snapshot request.
type LogsResult struct {
	Logs       []ContainerLogs `json:"logs"`
	Containers int             `json:"containers"`
	TotalLines int             `json:"total_lines"`
	Window     LogWindow       `json:"window"`
}

// FetchLogs retrieves a snapshot of logs for containers matching the given target.
// The window controls how far back in time to look: a duration like "2.5h" or "5.6d", or "all".
func FetchLogs(ctx context.Context, target types.ServiceTarget, window LogWindow) (*LogsResult, error) {
	if target.Name == "" {
		return nil, errAppNameRequired
	}

	api, err := newDockerAPIClient()
	if err != nil {
		return nil, err
	}
	defer api.Close()

	containers, err := listTargetContainers(ctx, api, target)
	if err != nil {
		return nil, err
	}

	fmt.Println(target.Name, target.Service, "→", len(containers), "matching containers found")

	// Convert the window into an RFC3339 timestamp for Docker's Since filter.
	// An empty string means no time restriction (fetch all available logs).
	sinceTimestamp := windowToTimestamp(window)

	result := &LogsResult{
		Window: window,
		Logs:   []ContainerLogs{},
	}
	fmt.Println(len(containers), "containers found matching target")

	for _, c := range containers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		name := firstContainerName(c)
		service := c.Labels["com.docker.compose.service"]

		logrus.WithFields(logrus.Fields{
			"container": name,
			"service":   service,
			"since":     sinceTimestamp,
		}).Info("Fetching container logs snapshot")

		opts := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: false,
			Follow:     false,
			Tail:       "all",
			Since:      sinceTimestamp, // empty string = no restriction
		}

		rc, err := api.ContainerLogs(ctx, c.ID, opts)
		if err != nil {
			logrus.WithError(err).WithField("container", name).Warn("Failed to fetch logs for container")

			continue
		}

		// Docker log stream is multiplexed (stdout + stderr in one stream).
		// stdcopy.StdCopy demultiplexes it into separate buffers.
		var stdoutBuf, stderrBuf bytes.Buffer
		if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, rc); err != nil {
			logrus.WithError(err).WithField("container", name).Warn("Failed to read log stream for container")
			rc.Close()

			continue
		}
		rc.Close()

		cl := ContainerLogs{
			Container: name,
			Service:   service,
			Stdout:    splitLines(stdoutBuf.String()),
			Stderr:    splitLines(stderrBuf.String()),
		}
		result.Logs = append(result.Logs, cl)
		result.TotalLines += len(cl.Stdout) + len(cl.Stderr)
	}

	result.Containers = len(result.Logs)

	return result, nil
}

// ParseLogWindow validates and normalises a log window string.
// Accepts:
//   - "" or "24h" → defaults to 24h
//   - "all"       → no time restriction
//   - Any Go duration string: "30m", "2.5h", "90s", etc.
//   - A "d"-suffixed float for days: "1d", "5.6d", etc.
func ParseLogWindow(s string) (LogWindow, error) {
	if s == "" {
		return LogWindow("24h"), nil
	}

	if LogWindow(s) == LogWindowAll {
		return LogWindowAll, nil
	}

	if _, err := parseLogDuration(s); err != nil {
		return "", fmt.Errorf("invalid log window %q: %w", s, err)
	}

	return LogWindow(s), nil
}

// windowToTimestamp converts a LogWindow into the RFC3339 timestamp that marks
// the earliest point from which logs should be returned.
// Returns an empty string for LogWindowAll (no restriction).
func windowToTimestamp(window LogWindow) string {
	if window == LogWindowAll {
		return ""
	}

	dur, err := parseLogDuration(string(window))
	if err != nil {
		// Fallback: treat as no restriction rather than failing silently.
		logrus.WithError(err).WithField("window", window).Warn("Failed to parse log window, returning all logs")

		return ""
	}

	return time.Now().UTC().Add(-dur).Format(time.RFC3339)
}

// parseDuration extends time.ParseDuration with support for a "d" suffix (days).
// Examples: "1d" = 24h, "5.6d" = 134.4h, "2.5h" = 2h30m.
func parseLogDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid day value in %q", s)
		}

		return time.Duration(days * float64(24*time.Hour)), nil
	}

	return time.ParseDuration(s)
}

// LogLine represents a single log line emitted by a container.
type LogLine struct {
	Container string `json:"container"`
	Service   string `json:"service,omitempty"`
	Stream    string `json:"stream"` // "stdout" or "stderr"
	Line      string `json:"line"`
}

// StreamLogs streams log lines for all containers matching target to out.
// It always closes out before returning, whether due to completion or error.
func StreamLogs(ctx context.Context, target types.ServiceTarget, window LogWindow, out chan<- LogLine) error {
	if target.Name == "" {
		close(out)

		return errAppNameRequired
	}

	api, err := newDockerAPIClient()
	if err != nil {
		close(out)

		return err
	}
	defer api.Close()

	containers, err := listTargetContainers(ctx, api, target)
	if err != nil {
		close(out)

		return err
	}

	sinceTimestamp := windowToTimestamp(window)

	logrus.WithFields(logrus.Fields{
		"app":        target.Name,
		"service":    target.Service,
		"containers": len(containers),
		"since":      sinceTimestamp,
	}).Info("Starting log stream")

	var wg sync.WaitGroup

	for _, c := range containers {
		wg.Add(1)

		go func(c container.Summary) {
			defer wg.Done()
			streamContainerLogs(ctx, api, c, sinceTimestamp, out)
		}(c)
	}

	wg.Wait()
	close(out)

	return nil
}

// streamContainerLogs streams demultiplexed stdout/stderr lines from a single
// container into out. It returns when the container stops, the context is
// cancelled, or the client disconnects.
func streamContainerLogs(ctx context.Context, api dockerClient.APIClient, c container.Summary, sinceTimestamp string, out chan<- LogLine) {
	name := firstContainerName(c)
	service := c.Labels["com.docker.compose.service"]

	logrus.WithFields(logrus.Fields{
		"container": name,
		"service":   service,
	}).Info("Attaching to container log stream")

	rc, err := api.ContainerLogs(ctx, c.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Since:      sinceTimestamp,
	})
	if err != nil {
		logrus.WithError(err).WithField("container", name).Warn("Failed to start log stream for container")

		return
	}

	// streamDone lets the watcher goroutine exit cleanly when the stream ends
	// naturally, avoiding a goroutine leak.
	streamDone := make(chan struct{})
	defer close(streamDone)
	defer rc.Close()

	// Close rc if ctx is cancelled so stdcopy.StdCopy unblocks immediately.
	go func() {
		select {
		case <-ctx.Done():
			rc.Close()
		case <-streamDone:
		}
	}()

	stdoutPR, stdoutPW := io.Pipe()
	stderrPR, stderrPW := io.Pipe()

	// Demultiplex Docker's combined stream. Exits when rc closes.
	go func() {
		defer stdoutPW.Close()
		defer stderrPW.Close()

		if _, err := stdcopy.StdCopy(stdoutPW, stderrPW, rc); err != nil && ctx.Err() == nil {
			logrus.WithError(err).WithField("container", name).Debug("Log stream demux ended")
		}
	}()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()
		defer stdoutPR.Close()
		scanLines(ctx, stdoutPR, name, service, "stdout", out)
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()
		defer stderrPR.Close()
		scanLines(ctx, stderrPR, name, service, "stderr", out)
	}()

	wg.Wait()
}

// scanLines reads r line by line, sending each non-empty line to out.
// Returns when the reader is exhausted, the context is cancelled, or the
// client can no longer accept lines.
func scanLines(ctx context.Context, r io.Reader, containerName, service, stream string, out chan<- LogLine) {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			select {
			case out <- LogLine{Container: containerName, Service: service, Stream: stream, Line: line}:
			case <-ctx.Done():
				return
			}
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"container": containerName,
			"stream":    stream,
		}).Debug("Log scanner ended with error")
	}
}

// splitLines splits a multi-line string into a slice of non-empty lines.
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}

	var lines []string

	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}

	if lines == nil {
		return []string{}
	}

	return lines
}
