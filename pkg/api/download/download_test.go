package download_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/pkg/api/download"
)

func TestDownload(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Download Handler Suite")
}

var _ = ginkgo.Describe("Download Handler", func() {
	var (
		downloadLock   chan bool
		mockDownloadFn func(images []string) (*download.Result, error)
		handler        *download.Handler
		server         *ghttp.Server
	)

	ginkgo.BeforeEach(func() {
		downloadLock = nil
		mockDownloadFn = func(images []string) (*download.Result, error) {
			return &download.Result{
				Requested:  len(images),
				Downloaded: len(images),
			}, nil
		}
		handler = download.New(mockDownloadFn, downloadLock)
		server = ghttp.NewServer()

		logrus.SetOutput(io.Discard)
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.It("should expose the download endpoint path", func() {
		gomega.Expect(handler.Path).To(gomega.Equal("/v1/download"))
	})

	ginkgo.It("should execute a targeted download and return JSON", func() {
		var received []string
		handler = download.New(func(images []string) (*download.Result, error) {
			received = images
			return &download.Result{
				Requested:  len(images),
				Downloaded: len(images),
			}, nil
		}, nil)

		server.AppendHandlers(handler.Handle)
		resp, err := http.Post(
			server.URL()+"/v1/download?image=foo/bar:1.0,baz/qux:latest",
			"application/json",
			bytes.NewBufferString("ignored"),
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))
		gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))
		gomega.Expect(received).To(gomega.Equal([]string{"foo/bar:1.0", "baz/qux:latest"}))

		var response map[string]any
		gomega.Expect(json.NewDecoder(resp.Body).Decode(&response)).To(gomega.Succeed())

		summary := response["summary"].(map[string]any)
		gomega.Expect(summary["requested"]).To(gomega.Equal(float64(2)))
		gomega.Expect(summary["downloaded"]).To(gomega.Equal(float64(2)))
		gomega.Expect(summary["failed"]).To(gomega.Equal(float64(0)))
		gomega.Expect(response["api_version"]).To(gomega.Equal("v1"))
	})

	ginkgo.It("should reject concurrent full downloads with 429", func() {
		lock := make(chan bool, 1)
		handler = download.New(mockDownloadFn, lock)

		server.AppendHandlers(handler.Handle)
		resp, err := http.Post(server.URL()+"/v1/download", "application/json", nil)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusTooManyRequests))
		gomega.Expect(resp.Header.Get("Retry-After")).To(gomega.Equal("30"))
	})

	ginkgo.It("should block targeted downloads until the lock is available", func() {
		lock := make(chan bool, 1)
		started := make(chan struct{})
		handler = download.New(func(images []string) (*download.Result, error) {
			return &download.Result{
				Requested:  len(images),
				Downloaded: len(images),
			}, nil
		}, lock)

		req := httptest.NewRequest(http.MethodPost, "/v1/download?image=foo/bar:1.0", http.NoBody)
		rec := httptest.NewRecorder()

		done := make(chan struct{})

		go func() {
			close(started)
			defer close(done)
			handler.Handle(rec, req)
		}()

		<-started
		select {
		case <-done:
			ginkgo.Fail("handler should be waiting for lock")
		case <-time.After(50 * time.Millisecond):
		}

		lock <- true

		select {
		case <-done:
		case <-time.After(time.Second):
			ginkgo.Fail("handler did not finish after lock release")
		}

		gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
	})

	ginkgo.It("should return 500 when the request body cannot be read", func() {
		faultyReader := &faultyReadCloser{err: errors.New("read error")}

		server.AppendHandlers(func(w http.ResponseWriter, r *http.Request) {
			r.Body = faultyReader
			handler.Handle(w, r)
		})

		resp, err := http.Post(server.URL()+"/v1/download", "application/json", bytes.NewBufferString("ignored"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusInternalServerError))
	})

	ginkgo.It("should return 501 when the download function is not wired", func() {
		handler = download.New(nil, nil)

		server.AppendHandlers(handler.Handle)
		resp, err := http.Post(server.URL()+"/v1/download?image=foo/bar:1.0", "application/json", nil)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer resp.Body.Close()

		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusNotImplemented))
	})

	ginkgo.It("should return 503 when the request is cancelled while waiting for the lock", func() {
		lock := make(chan bool)
		handler = download.New(mockDownloadFn, lock)

		req := httptest.NewRequest(http.MethodPost, "/v1/download?image=foo/bar:1.0", http.NoBody)
		ctx, cancel := context.WithCancel(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.Handle(rec, req)
		}()

		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			ginkgo.Fail("handler did not stop after request cancellation")
		}

		gomega.Expect(rec.Code).To(gomega.Equal(http.StatusServiceUnavailable))
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
