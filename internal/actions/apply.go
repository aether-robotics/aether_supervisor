package actions

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/aether-robotics/aether_supervisor/pkg/metrics"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

// ApplyLocalUpdates runs the standard Watchtower update procedure using already-downloaded images only.
// Explicitly sets NoPull to true to prevent any image pulls, and RunOnce to true to ensure the update process runs only once.
func ApplyLocalUpdates(
	ctx context.Context,
	filter types.Filter,
	cleanup bool,
	monitorOnly bool,
	runUpdates func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric,
) *metrics.Metric {
	params := types.UpdateParams{
		Cleanup:        cleanup,
		RunOnce:        true,
		MonitorOnly:    monitorOnly,
		NoPull:         true,
		SkipSelfUpdate: false,
	}

	logrus.WithFields(logrus.Fields{
		"cleanup":      params.Cleanup,
		"monitor_only": params.MonitorOnly,
		"run_once":     params.RunOnce,
		"no_pull":      params.NoPull,
	}).Info("Starting local apply update run")

	metric := runUpdates(ctx, filter, params)

	logrus.WithFields(logrus.Fields{
		"cleanup":      params.Cleanup,
		"monitor_only": params.MonitorOnly,
		"run_once":     params.RunOnce,
		"no_pull":      params.NoPull,
		"scanned":      metric.Scanned,
		"updated":      metric.Updated,
		"failed":       metric.Failed,
		"restarted":    metric.Restarted,
	}).Info("Completed local apply update run")

	return metric
}
