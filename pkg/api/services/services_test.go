package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/api/services"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestServices(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Services Handler Suite")
}

var _ = ginkgo.Describe("Services Handlers", func() {
	var server *ghttp.Server

	ginkgo.BeforeEach(func() {
		server = ghttp.NewServer()
		logrus.SetOutput(io.Discard)
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.It("should expose all service endpoint paths", func() {
		gomega.Expect(services.New(nil).Path).To(gomega.Equal("/v1/services"))
		gomega.Expect(services.NewStop(nil).Path).To(gomega.Equal("/v1/services/stop"))
		gomega.Expect(services.NewStart(nil).Path).To(gomega.Equal("/v1/services/start"))
		gomega.Expect(services.NewRestart(nil).Path).To(gomega.Equal("/v1/services/restart"))
		gomega.Expect(services.NewRemove(nil).Path).To(gomega.Equal("/v1/services/remove"))
	})

	ginkgo.It("should accept JSON app specs for deploy", func() {
		var received types.AppSpec
		handler := services.New(func(_ context.Context, spec types.AppSpec) (*services.DeployResult, error) {
			received = spec
			return nil, nil
		})
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/json",
			bytes.NewBufferString(`{"name":"demo","services":{"web":{"image":"nginx:latest"}}}`),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(received.Name).To(gomega.Equal("demo"))
		gomega.Expect(received.Services["web"].Image).To(gomega.Equal("nginx:latest"))
	})

	ginkgo.It("should accept YAML app specs for deploy", func() {
		var received types.AppSpec
		handler := services.New(func(_ context.Context, spec types.AppSpec) (*services.DeployResult, error) {
			received = spec
			return nil, nil
		})
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/yaml",
			bytes.NewBufferString("name: demo\nservices:\n  web:\n    image: nginx:latest\n"),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(received.Services["web"].Image).To(gomega.Equal("nginx:latest"))
	})

	ginkgo.It("should reject unsupported content types for deploy", func() {
		handler := services.New(nil)
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/xml",
			bytes.NewBufferString("<services />"),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusBadRequest))
	})

	ginkgo.It("should reject deploy requests without services", func() {
		handler := services.New(nil)
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/json",
			bytes.NewBufferString(`{"name":"demo","services":{}}`),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusBadRequest))
	})

	ginkgo.It("should reject deploy requests without app name", func() {
		handler := services.New(nil)
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/json",
			bytes.NewBufferString(`{"services":{"web":{"image":"nginx:latest"}}}`),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusBadRequest))
	})

	ginkgo.It("should return 501 when deploy is not wired", func() {
		handler := services.New(nil)
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/json",
			bytes.NewBufferString(`{"name":"demo","services":{"web":{"image":"nginx:latest"}}}`),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusNotImplemented))
	})

	ginkgo.It("should reject non-POST methods for deploy", func() {
		handler := services.New(nil)
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+handler.Path, http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusMethodNotAllowed))
	})

	ginkgo.DescribeTable("action handlers",
		func(
			handler *services.ActionHandler,
			body string,
			contentType string,
			expectedStatus int,
			verify func(types.ServiceTarget),
		) {
			var received types.ServiceTarget
			if verify != nil {
				switch handler.Path {
				case "/v1/services/stop":
					handler = services.NewStop(func(_ context.Context, target types.ServiceTarget) (*services.ActionResult, error) {
						received = target
						return nil, nil
					})
				case "/v1/services/start":
					handler = services.NewStart(func(_ context.Context, target types.ServiceTarget) (*services.ActionResult, error) {
						received = target
						return nil, nil
					})
				case "/v1/services/restart":
					handler = services.NewRestart(func(_ context.Context, target types.ServiceTarget) (*services.ActionResult, error) {
						received = target
						return nil, nil
					})
				case "/v1/services/remove":
					handler = services.NewRemove(func(_ context.Context, target types.ServiceTarget) (*services.ActionResult, error) {
						received = target
						return nil, nil
					})
				}
			}

			server.AppendHandlers(handler.Handle)
			resp, err := http.Post(server.URL()+handler.Path, contentType, bytes.NewBufferString(body))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(expectedStatus))
			if verify != nil {
				verify(received)
			}
		},
		ginkgo.Entry(
			"stop app",
			services.NewStop(nil),
			`{"name":"demo"}`,
			"application/json",
			http.StatusOK,
			func(target types.ServiceTarget) {
				gomega.Expect(target.Name).To(gomega.Equal("demo"))
				gomega.Expect(target.Service).To(gomega.BeEmpty())
			},
		),
		ginkgo.Entry(
			"stop service",
			services.NewStop(nil),
			`{"name":"demo","service":"web"}`,
			"application/json",
			http.StatusOK,
			func(target types.ServiceTarget) {
				gomega.Expect(target.Name).To(gomega.Equal("demo"))
				gomega.Expect(target.Service).To(gomega.Equal("web"))
			},
		),
		ginkgo.Entry(
			"start service",
			services.NewStart(nil),
			`{"name":"demo","service":"web"}`,
			"application/json",
			http.StatusOK,
			func(target types.ServiceTarget) {
				gomega.Expect(target.Service).To(gomega.Equal("web"))
			},
		),
		ginkgo.Entry(
			"restart service with yaml",
			services.NewRestart(nil),
			"name: demo\nservice: web\n",
			"application/yaml",
			http.StatusOK,
			func(target types.ServiceTarget) {
				gomega.Expect(target.Name).To(gomega.Equal("demo"))
				gomega.Expect(target.Service).To(gomega.Equal("web"))
			},
		),
		ginkgo.Entry(
			"remove app",
			services.NewRemove(nil),
			`{"name":"demo"}`,
			"application/json",
			http.StatusOK,
			func(target types.ServiceTarget) {
				gomega.Expect(target.Name).To(gomega.Equal("demo"))
			},
		),
		ginkgo.Entry(
			"reject start without service",
			services.NewStart(nil),
			`{"name":"demo"}`,
			"application/json",
			http.StatusBadRequest,
			nil,
		),
		ginkgo.Entry(
			"reject action without app name",
			services.NewStop(nil),
			`{"service":"web"}`,
			"application/json",
			http.StatusBadRequest,
			nil,
		),
	)

	ginkgo.It("should return 501 when action is not wired", func() {
		handler := services.NewRemove(nil)
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/json",
			bytes.NewBufferString(`{"name":"demo","service":"web"}`),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusNotImplemented))
		body, readErr := io.ReadAll(resp.Body)
		gomega.Expect(readErr).ToNot(gomega.HaveOccurred())
		gomega.Expect(string(body)).To(gomega.ContainSubstring("services operation is not implemented"))
	})

	ginkgo.It("should include the action in lifecycle responses", func() {
		handler := services.NewRestart(func(_ context.Context, _ types.ServiceTarget) (*services.ActionResult, error) {
			return &services.ActionResult{Name: "demo", Service: "web", Affected: 1}, nil
		})
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(
			server.URL()+handler.Path,
			"application/json",
			bytes.NewBufferString(`{"name":"demo","service":"web"}`),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())
		gomega.Expect(response["action"]).To(gomega.Equal("restart"))
	})
})
