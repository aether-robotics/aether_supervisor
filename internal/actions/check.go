package actions

import (
	"context"
	"fmt"

	dockerClient "github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	checkAPI "github.com/aether-robotics/aether_supervisor/pkg/api/check"
	"github.com/aether-robotics/aether_supervisor/pkg/container"
	"github.com/aether-robotics/aether_supervisor/pkg/registry"
	"github.com/aether-robotics/aether_supervisor/pkg/registry/digest"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

// CheckForUpdates inspects filtered containers and returns only those with newer upstream images.
func CheckForUpdates(
	ctx context.Context,
	client container.Client,
	filter types.Filter,
) (*checkAPI.Result, error) {
	containers, err := client.ListContainers(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list containers for check: %w", err)
	}

	api, err := newDockerAPIClient()
	if err != nil {
		return nil, err
	}
	defer api.Close()

	result := &checkAPI.Result{
		Scanned: len(containers),
	}

	for _, c := range containers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pinned, err := isPinned(c, nil, types.UpdateParams{})
		if err != nil {
			result.Failed++
			logrus.WithError(err).WithFields(logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
			}).Debug("Skipping container during check due to invalid image reference")

			continue
		}
		if pinned {
			continue
		}

		upstreamDigest, err := fetchRemoteDigest(ctx, api, c)
		if err != nil {
			result.Failed++
			logrus.WithError(err).WithFields(logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
			}).Debug("Failed to fetch upstream digest during check")

			continue
		}

		if digest.DigestsMatch(c.ImageInfo().RepoDigests, upstreamDigest) {
			continue
		}

		result.Services = append(result.Services, checkAPI.ServiceUpdate{
			Name:           c.Name(),
			Image:          c.ImageName(),
			CurrentDigest:  preferredContainerDigest(c),
			UpstreamDigest: upstreamDigest,
		})
	}

	result.Updatable = len(result.Services)

	return result, nil
}

func fetchRemoteDigest(
	ctx context.Context,
	api dockerClient.APIClient,
	c types.Container,
) (string, error) {
	opts, err := registry.GetPullOptions(c.ImageName())
	if err != nil {
		return "", fmt.Errorf("load auth for %s: %w", c.ImageName(), err)
	}

	return digest.FetchDigest(ctx, api, c, opts.RegistryAuth)
}

func preferredContainerDigest(c types.Container) string {
	if c.HasImageInfo() && len(c.ImageInfo().RepoDigests) > 0 {
		return c.ImageInfo().RepoDigests[0]
	}

	return string(c.ImageID())
}
