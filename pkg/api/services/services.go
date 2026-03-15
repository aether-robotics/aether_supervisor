package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	contentTypeJSON         = "application/json"
	contentTypeYAML         = "application/yaml"
	contentTypeXHTTPYAML    = "application/x-yaml"
	contentTypeTextYAML     = "text/yaml"
	contentTypeTextPlainYML = "text/plain"
)

const (
	actionStop    = "stop"
	actionStart   = "start"
	actionRestart = "restart"
	actionRemove  = "remove"
)

var (
	errUnsupportedContentType = errors.New("unsupported content type")
	errEmptyServices          = errors.New("app spec must define at least one service")
	errAppNameRequired        = errors.New("app name is required")
	errNotImplemented         = errors.New("services operation is not implemented")
	errMissingAppName         = errors.New("target name is required")
	errMissingServiceTarget   = errors.New("service target is required")
)

// DeployResult summarizes an app deployment request.
type DeployResult struct {
	Application string   `json:"application,omitempty"`
	Services    []string `json:"services"`
	Created     int      `json:"created"`
}

// ActionResult summarizes a lifecycle request.
type ActionResult struct {
	Name     string `json:"name,omitempty"`
	Service  string `json:"service,omitempty"`
	Affected int    `json:"affected"`
}

// DeployHandler accepts Compose-like app specs and dispatches them to a deployment function.
type DeployHandler struct {
	fn   func(context.Context, types.AppSpec) (*DeployResult, error)
	Path string
}

// ActionHandler handles non-deploy lifecycle operations for apps and services.
type ActionHandler struct {
	action         string
	fn             func(context.Context, types.ServiceTarget) (*ActionResult, error)
	Path           string
	requireService bool
}

// New creates a deploy handler for Compose-like app specs.
func New(fn func(context.Context, types.AppSpec) (*DeployResult, error)) *DeployHandler {
	return &DeployHandler{
		fn:   fn,
		Path: "/v1/services",
	}
}

// NewStop creates a stop handler.
func NewStop(fn func(context.Context, types.ServiceTarget) (*ActionResult, error)) *ActionHandler {
	return &ActionHandler{action: actionStop, fn: fn, Path: "/v1/services/stop"}
}

// NewStart creates a start handler for individual services.
func NewStart(fn func(context.Context, types.ServiceTarget) (*ActionResult, error)) *ActionHandler {
	return &ActionHandler{
		action:         actionStart,
		fn:             fn,
		Path:           "/v1/services/start",
		requireService: true,
	}
}

// NewRestart creates a restart handler for individual services.
func NewRestart(fn func(context.Context, types.ServiceTarget) (*ActionResult, error)) *ActionHandler {
	return &ActionHandler{
		action:         actionRestart,
		fn:             fn,
		Path:           "/v1/services/restart",
		requireService: true,
	}
}

// NewRemove creates a remove handler.
func NewRemove(fn func(context.Context, types.ServiceTarget) (*ActionResult, error)) *ActionHandler {
	return &ActionHandler{action: actionRemove, fn: fn, Path: "/v1/services/remove"}
}

// Handle processes a service deployment request.
func (handle *DeployHandler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API services deploy request")

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)

		return
	}

	spec, err := decodeAppSpec(body, r.Header.Get("Content-Type"))
	if err != nil {
		logrus.WithError(err).Debug("Failed to decode services request")
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if err := validateSpec(spec); err != nil {
		logrus.WithError(err).Debug("Invalid services request")
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	startTime := time.Now()
	if handle.fn == nil {
		logrus.Warn("Services deployment endpoint called without an execution function")
		http.Error(w, errNotImplemented.Error(), http.StatusNotImplemented)

		return
	}

	result, err := handle.fn(r.Context(), spec)
	if err != nil {
		logrus.WithError(err).Error("Services deployment failed")
		http.Error(w, "Failed to deploy services", http.StatusInternalServerError)

		return
	}

	if result == nil {
		result = defaultDeployResult(spec)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":      result,
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"timing": map[string]any{
			"duration_ms": time.Since(startTime).Milliseconds(),
		},
	})
}

// Handle processes a lifecycle action request.
func (handle *ActionHandler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
		"action": handle.action,
	}).Info("Received HTTP API services action request")

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read action request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)

		return
	}

	target, err := decodeServiceTarget(body, r.Header.Get("Content-Type"))
	if err != nil {
		logrus.WithError(err).Debug("Failed to decode action request")
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if err := validateTarget(target, handle.requireService); err != nil {
		logrus.WithError(err).Debug("Invalid action request")
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	startTime := time.Now()
	if handle.fn == nil {
		logrus.WithField("action", handle.action).Warn("Services action endpoint called without an execution function")
		http.Error(w, errNotImplemented.Error(), http.StatusNotImplemented)

		return
	}

	result, err := handle.fn(r.Context(), target)
	if err != nil {
		logrus.WithError(err).WithField("action", handle.action).Error("Services action failed")
		http.Error(w, "Failed to execute services action", http.StatusInternalServerError)

		return
	}

	if result == nil {
		result = defaultActionResult(target)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":      result,
		"action":      handle.action,
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"timing": map[string]any{
			"duration_ms": time.Since(startTime).Milliseconds(),
		},
	})
}

func decodeAppSpec(body []byte, contentType string) (types.AppSpec, error) {
	var spec types.AppSpec

	switch normalizeContentType(contentType) {
	case "", contentTypeJSON:
		if err := json.Unmarshal(body, &spec); err != nil {
			return types.AppSpec{}, err
		}
	case contentTypeYAML, contentTypeXHTTPYAML, contentTypeTextYAML, contentTypeTextPlainYML:
		if err := yaml.Unmarshal(body, &spec); err != nil {
			return types.AppSpec{}, err
		}
	default:
		return types.AppSpec{}, errUnsupportedContentType
	}

	return spec, nil
}

func decodeServiceTarget(body []byte, contentType string) (types.ServiceTarget, error) {
	var target types.ServiceTarget

	switch normalizeContentType(contentType) {
	case "", contentTypeJSON:
		if err := json.Unmarshal(body, &target); err != nil {
			return types.ServiceTarget{}, err
		}
	case contentTypeYAML, contentTypeXHTTPYAML, contentTypeTextYAML, contentTypeTextPlainYML:
		if err := yaml.Unmarshal(body, &target); err != nil {
			return types.ServiceTarget{}, err
		}
	default:
		return types.ServiceTarget{}, errUnsupportedContentType
	}

	return target, nil
}

func normalizeContentType(value string) string {
	if idx := strings.Index(value, ";"); idx >= 0 {
		value = value[:idx]
	}

	return strings.TrimSpace(strings.ToLower(value))
}

func validateSpec(spec types.AppSpec) error {
	if spec.Name == "" {
		return errAppNameRequired
	}

	if len(spec.Services) == 0 {
		return errEmptyServices
	}

	return nil
}

func validateTarget(target types.ServiceTarget, requireService bool) error {
	if target.Name == "" {
		return errMissingAppName
	}

	if requireService && target.Service == "" {
		return errMissingServiceTarget
	}

	return nil
}

func defaultDeployResult(spec types.AppSpec) *DeployResult {
	serviceNames := make([]string, 0, len(spec.Services))
	for serviceName := range spec.Services {
		serviceNames = append(serviceNames, serviceName)
	}
	sort.Strings(serviceNames)

	return &DeployResult{
		Application: spec.Name,
		Services:    serviceNames,
		Created:     len(serviceNames),
	}
}

func defaultActionResult(target types.ServiceTarget) *ActionResult {
	affected := 1
	if target.Service == "" {
		affected = 0
	}

	return &ActionResult{
		Name:     target.Name,
		Service:  target.Service,
		Affected: affected,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		logrus.WithError(err).Error("Failed to encode services response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)

	if _, err := w.Write(buf.Bytes()); err != nil {
		logrus.WithError(err).Error("Failed to write services response")
	}
}
