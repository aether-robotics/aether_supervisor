package actions

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("parseLogDuration", func() {
	ginkgo.DescribeTable("valid inputs",
		func(input string, expected time.Duration) {
			got, err := parseLogDuration(input)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(got).To(gomega.Equal(expected))
		},
		ginkgo.Entry("whole hours", "2h", 2*time.Hour),
		ginkgo.Entry("fractional hours", "2.5h", 2*time.Hour+30*time.Minute),
		ginkgo.Entry("minutes", "90m", 90*time.Minute),
		ginkgo.Entry("whole days via d suffix", "1d", 24*time.Hour),
		ginkgo.Entry("fractional days via d suffix", "5.6d", time.Duration(5.6*float64(24*time.Hour))),
		ginkgo.Entry("large day value", "30d", time.Duration(30*float64(24*time.Hour))),
	)

	ginkgo.DescribeTable("invalid inputs",
		func(input string) {
			_, err := parseLogDuration(input)
			gomega.Expect(err).To(gomega.HaveOccurred())
		},
		ginkgo.Entry("empty string", ""),
		ginkgo.Entry("plain number", "42"),
		ginkgo.Entry("negative days", "-1d"),
		ginkgo.Entry("zero days", "0d"),
		ginkgo.Entry("non-numeric day prefix", "xd"),
		ginkgo.Entry("unknown unit", "3w"),
	)
})

var _ = ginkgo.Describe("splitLines", func() {
	ginkgo.It("returns empty slice for empty string", func() {
		result := splitLines("")
		gomega.Expect(result).To(gomega.BeEmpty())
	})

	ginkgo.It("returns a single line", func() {
		result := splitLines("hello world")
		gomega.Expect(result).To(gomega.Equal([]string{"hello world"}))
	})

	ginkgo.It("splits multiple lines", func() {
		result := splitLines("line one\nline two\nline three")
		gomega.Expect(result).To(gomega.Equal([]string{"line one", "line two", "line three"}))
	})

	ginkgo.It("strips blank lines", func() {
		result := splitLines("first\n\nsecond\n\n")
		gomega.Expect(result).To(gomega.Equal([]string{"first", "second"}))
	})

	ginkgo.It("returns empty slice for whitespace-only string of newlines", func() {
		result := splitLines("\n\n\n")
		gomega.Expect(result).To(gomega.BeEmpty())
	})
})

var _ = ginkgo.Describe("windowToTimestamp", func() {
	ginkgo.It("returns empty string for LogWindowAll", func() {
		gomega.Expect(windowToTimestamp(LogWindowAll)).To(gomega.BeEmpty())
	})

	ginkgo.It("returns a timestamp roughly 24h in the past for '24h'", func() {
		before := time.Now().UTC().Add(-24*time.Hour - time.Second)
		after := time.Now().UTC().Add(-24*time.Hour + time.Second)

		ts, err := time.Parse(time.RFC3339, windowToTimestamp("24h"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(ts).To(gomega.BeTemporally(">", before))
		gomega.Expect(ts).To(gomega.BeTemporally("<", after))
	})

	ginkgo.It("returns a timestamp roughly 7 days in the past for '7d'", func() {
		before := time.Now().UTC().Add(-7*24*time.Hour - time.Second)
		after := time.Now().UTC().Add(-7*24*time.Hour + time.Second)

		ts, err := time.Parse(time.RFC3339, windowToTimestamp("7d"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(ts).To(gomega.BeTemporally(">", before))
		gomega.Expect(ts).To(gomega.BeTemporally("<", after))
	})

	ginkgo.It("returns a timestamp for an arbitrary fractional day window", func() {
		ts, err := time.Parse(time.RFC3339, windowToTimestamp("0.5d"))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		expected := time.Now().UTC().Add(-12 * time.Hour)
		gomega.Expect(ts).To(gomega.BeTemporally("~", expected, 2*time.Second))
	})
})
