package check_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/pkg/api/check"
)

func TestCheck(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Check Handler Suite")
}

var _ = ginkgo.Describe("Check Handler", func() {
	var (
		handler *check.Handler
		server  *ghttp.Server
	)

	ginkgo.BeforeEach(func() {
		handler = check.New(func(_ []string) (*check.Result, error) {
			return &check.Result{
				Scanned:   2,
				Updatable: 1,
				Services: []check.ServiceUpdate{
					{
						Name:           "web",
						Image:          "nginx:latest",
						CurrentDigest:  "repo@sha256:old",
						UpstreamDigest: "sha256:new",
					},
				},
			}, nil
		}, nil)
		server = ghttp.NewServer()
		logrus.SetOutput(io.Discard)
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.It("should expose the check endpoint path", func() {
		gomega.Expect(handler.Path).To(gomega.Equal("/v1/check"))
	})

	ginkgo.It("should execute a check and return JSON results", func() {
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(server.URL()+"/v1/check", "application/json", bytes.NewBufferString("ignored"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

		summary := response["summary"].(map[string]any)
		gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(2)))
		gomega.Expect(summary["updatable"]).To(gomega.Equal(float64(1)))
		gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))

		services := response["services"].([]any)
		gomega.Expect(services).To(gomega.HaveLen(1))
		service := services[0].(map[string]any)
		gomega.Expect(service["name"]).To(gomega.Equal("web"))
		gomega.Expect(service["image"]).To(gomega.Equal("nginx:latest"))
	})

	ginkgo.It("should pass image query parameters to the check function", func() {
		var received []string
		handler = check.New(func(images []string) (*check.Result, error) {
			received = images
			return &check.Result{}, nil
		}, nil)

		server.AppendHandlers(handler.Handle)
		resp, err := http.Post(server.URL()+"/v1/check?image=foo/bar,baz/qux", "application/json", nil)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(received).To(gomega.Equal([]string{"foo/bar", "baz/qux"}))
	})

	ginkgo.It("should reject concurrent full checks with 429", func() {
		lock := make(chan bool, 1)
		handler = check.New(func(_ []string) (*check.Result, error) {
			return &check.Result{}, nil
		}, lock)

		server.AppendHandlers(handler.Handle)
		resp, err := http.Post(server.URL()+"/v1/check", "application/json", nil)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusTooManyRequests))
		gomega.Expect(resp.Header.Get("Retry-After")).To(gomega.Equal("30"))
	})

	ginkgo.It("should return 500 when body reading fails", func() {
		faultyReader := &faultyReadCloser{err: errors.New("read error")}

		server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
			r.Body = faultyReader
			handler.Handle(w, r)
		})

		resp, err := http.Post(server.URL()+"/v1/check", "application/json", bytes.NewBufferString("ignored"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusInternalServerError))
	})
})

type faultyReadCloser struct {
	err error
}

func (f *faultyReadCloser) Read(_ []byte) (int, error) {
	return 0, f.err
}

func (f *faultyReadCloser) Close() error {
	return nil
}
