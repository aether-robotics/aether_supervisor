package apply_test

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

	"github.com/aether-robotics/aether_supervisor/pkg/api/apply"
	"github.com/aether-robotics/aether_supervisor/pkg/metrics"
)

func TestApply(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Apply Handler Suite")
}

var _ = ginkgo.Describe("Apply Handler", func() {
	var (
		handler *apply.Handler
		server  *ghttp.Server
	)

	ginkgo.BeforeEach(func() {
		handler = apply.New(func(_ []string) *metrics.Metric {
			return &metrics.Metric{Scanned: 2, Updated: 1, Failed: 0, Restarted: 0}
		}, nil)
		server = ghttp.NewServer()
		logrus.SetOutput(io.Discard)
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.It("should expose the apply endpoint path", func() {
		gomega.Expect(handler.Path).To(gomega.Equal("/v1/apply"))
	})

	ginkgo.It("should execute apply and return JSON results", func() {
		server.AppendHandlers(handler.Handle)

		resp, err := http.Post(server.URL()+"/v1/apply?image=foo/bar:latest", "application/json", bytes.NewBufferString("ignored"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

		summary := response["summary"].(map[string]any)
		gomega.Expect(summary["scanned"]).To(gomega.Equal(float64(2)))
		gomega.Expect(summary["updated"]).To(gomega.Equal(float64(1)))
		gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))
		gomega.Expect(summary["restarted"]).To(gomega.Equal(float64(0)))
	})

	ginkgo.It("should reject concurrent full apply requests with 429", func() {
		lock := make(chan bool, 1)
		handler = apply.New(func(_ []string) *metrics.Metric {
			return &metrics.Metric{}
		}, lock)

		server.AppendHandlers(handler.Handle)
		resp, err := http.Post(server.URL()+"/v1/apply", "application/json", nil)
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

		resp, err := http.Post(server.URL()+"/v1/apply", "application/json", bytes.NewBufferString("ignored"))
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
