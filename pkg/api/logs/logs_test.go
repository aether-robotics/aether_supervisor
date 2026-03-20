package logs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/internal/actions"
	"github.com/aether-robotics/aether_supervisor/pkg/api/logs"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

func TestLogs(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Logs Handler Suite")
}

var _ = ginkgo.Describe("Logs Handler", func() {
	var (
		handler *logs.Handler
		server  *ghttp.Server
	)

	ginkgo.BeforeEach(func() {
		logrus.SetOutput(io.Discard)
		handler = logs.New(func(_ context.Context, target types.ServiceTarget, window actions.LogWindow) (*actions.LogsResult, error) {
			return &actions.LogsResult{
				Window: window,
				Logs: []actions.ContainerLogs{
					{
						Container: "myapp-web-1",
						Service:   "web",
						Stdout:    []string{"starting server", "listening on :8080"},
						Stderr:    []string{},
					},
				},
				Containers: 1,
				TotalLines: 2,
			}, nil
		})
		server = ghttp.NewServer()
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.It("should expose the logs endpoint path", func() {
		gomega.Expect(handler.Path).To(gomega.Equal("/v1/logs"))
	})

	ginkgo.It("should return 200 with log results for a valid request", func() {
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

		gomega.Expect(response["api_version"]).To(gomega.Equal("v1"))
		gomega.Expect(response["timestamp"]).ToNot(gomega.BeEmpty())
		gomega.Expect(response["timing"]).ToNot(gomega.BeNil())

		result := response["result"].(map[string]any)
		gomega.Expect(result["containers"]).To(gomega.Equal(float64(1)))
		gomega.Expect(result["total_lines"]).To(gomega.Equal(float64(2)))

		containerLogs := result["logs"].([]any)
		gomega.Expect(containerLogs).To(gomega.HaveLen(1))
		first := containerLogs[0].(map[string]any)
		gomega.Expect(first["container"]).To(gomega.Equal("myapp-web-1"))
		gomega.Expect(first["service"]).To(gomega.Equal("web"))
		stdout := first["stdout"].([]any)
		gomega.Expect(stdout).To(gomega.ConsistOf("starting server", "listening on :8080"))
	})

	ginkgo.It("should return 400 when 'name' query parameter is missing", func() {
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusBadRequest))
	})

	ginkgo.It("should return 400 for an invalid 'since' value", func() {
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp&since=yesterday", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusBadRequest))
	})

	ginkgo.It("should return 501 when the handler function is not wired", func() {
		handler = logs.New(nil)
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusNotImplemented))
	})

	ginkgo.It("should return 500 when the handler function returns an error", func() {
		handler = logs.New(func(_ context.Context, _ types.ServiceTarget, _ actions.LogWindow) (*actions.LogsResult, error) {
			return nil, errors.New("docker unavailable")
		})
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusInternalServerError))
	})

	ginkgo.It("should forward the 'service' query parameter to the handler function", func() {
		var receivedTarget types.ServiceTarget

		handler = logs.New(func(_ context.Context, target types.ServiceTarget, _ actions.LogWindow) (*actions.LogsResult, error) {
			receivedTarget = target
			return &actions.LogsResult{Logs: []actions.ContainerLogs{}}, nil
		})
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp&service=web", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(receivedTarget.Name).To(gomega.Equal("myapp"))
		gomega.Expect(receivedTarget.Service).To(gomega.Equal("web"))
	})

	ginkgo.DescribeTable("log window forwarding",
		func(since string, expected actions.LogWindow) {
			var receivedWindow actions.LogWindow

			h := logs.New(func(_ context.Context, _ types.ServiceTarget, window actions.LogWindow) (*actions.LogsResult, error) {
				receivedWindow = window
				return &actions.LogsResult{Logs: []actions.ContainerLogs{}}, nil
			})
			server.AppendHandlers(h.Handle)

			url := server.URL() + "/v1/logs?name=myapp"
			if since != "" {
				url += "&since=" + since
			}

			req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
			gomega.Expect(receivedWindow).To(gomega.Equal(expected))
		},
		ginkgo.Entry("omitted since defaults to 24h", "", actions.LogWindow("24h")),
		ginkgo.Entry("explicit all", "all", actions.LogWindowAll),
		ginkgo.Entry("explicit 7d", "7d", actions.LogWindow("7d")),
		ginkgo.Entry("fractional hours", "2.5h", actions.LogWindow("2.5h")),
		ginkgo.Entry("fractional days", "5.6d", actions.LogWindow("5.6d")),
	)

	ginkgo.It("should include timing information in the response", func() {
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

		timing := response["timing"].(map[string]any)
		gomega.Expect(timing).To(gomega.HaveKey("duration_ms"))
		gomega.Expect(timing).To(gomega.HaveKey("duration"))
	})

	ginkgo.It("should reflect the window in the result", func() {
		server.AppendHandlers(handler.Handle)

		req, err := http.NewRequest(http.MethodGet, server.URL()+"/v1/logs?name=myapp&since=7d", http.NoBody)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resp, err := http.DefaultClient.Do(req)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

		result := response["result"].(map[string]any)
		gomega.Expect(result["window"]).To(gomega.Equal("7d"))
	})
})
