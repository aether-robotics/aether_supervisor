package actions_test

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/aether-robotics/aether_supervisor/internal/actions"
)

var _ = ginkgo.Describe("ParseLogWindow", func() {
	ginkgo.DescribeTable("valid inputs",
		func(input string, expected actions.LogWindow) {
			got, err := actions.ParseLogWindow(input)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(got).To(gomega.Equal(expected))
		},
		ginkgo.Entry("empty string defaults to 24h", "", actions.LogWindow("24h")),
		ginkgo.Entry("explicit 24h", "24h", actions.LogWindow("24h")),
		ginkgo.Entry("all logs", "all", actions.LogWindowAll),
		ginkgo.Entry("7 days", "7d", actions.LogWindow("7d")),
		ginkgo.Entry("fractional days", "5.6d", actions.LogWindow("5.6d")),
		ginkgo.Entry("fractional hours", "2.5h", actions.LogWindow("2.5h")),
		ginkgo.Entry("minutes", "90m", actions.LogWindow("90m")),
		ginkgo.Entry("30 days", "30d", actions.LogWindow("30d")),
	)

	ginkgo.DescribeTable("invalid inputs return an error",
		func(input string) {
			_, err := actions.ParseLogWindow(input)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring(input))
		},
		ginkgo.Entry("plain number", "42"),
		ginkgo.Entry("negative days", "-1d"),
		ginkgo.Entry("zero days", "0d"),
		ginkgo.Entry("unknown unit", "3w"),
		ginkgo.Entry("garbage string", "yesterday"),
	)
})
