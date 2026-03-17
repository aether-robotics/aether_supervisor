package actions_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aether-robotics/aether_supervisor/internal/actions"
	"github.com/aether-robotics/aether_supervisor/pkg/filters"
	"github.com/aether-robotics/aether_supervisor/pkg/metrics"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

func TestApplyLocalUpdatesUsesNoPull(t *testing.T) {
	t.Parallel()

	var receivedFilter types.Filter
	var receivedParams types.UpdateParams

	wantMetric := &metrics.Metric{Scanned: 1, Updated: 1}
	got := actions.ApplyLocalUpdates(
		context.Background(),
		filters.NoFilter,
		true,
		false,
		func(_ context.Context, filter types.Filter, params types.UpdateParams) *metrics.Metric {
			receivedFilter = filter
			receivedParams = params

			return wantMetric
		},
	)

	require.Same(t, wantMetric, got)
	require.NotNil(t, receivedFilter)
	require.True(t, receivedParams.Cleanup)
	require.True(t, receivedParams.RunOnce)
	require.True(t, receivedParams.NoPull)
	require.False(t, receivedParams.MonitorOnly)
}
