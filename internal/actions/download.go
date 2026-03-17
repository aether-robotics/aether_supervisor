package actions

import (
	"context"
	"fmt"
	"slices"
	"strings"

	dockerImage "github.com/docker/docker/api/types/image"
	dockerClient "github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	downloadAPI "github.com/aether-robotics/aether_supervisor/pkg/api/download"
	"github.com/aether-robotics/aether_supervisor/pkg/container"
	"github.com/aether-robotics/aether_supervisor/pkg/types"
)

type downloadTarget struct {
	ImageName string
	Services  []string
}

// DownloadImages pulls unique image references either from the request directly
// or from the filtered set of monitored containers when no explicit images are provided.
func DownloadImages(
	ctx context.Context,
	client container.Client,
	filter types.Filter,
	images []string,
) (*downloadAPI.Result, error) {
	targets, err := resolveDownloadTargets(ctx, client, filter, images)
	if err != nil {
		return nil, err
	}

	api, err := newDockerAPIClient()
	if err != nil {
		return nil, err
	}
	defer api.Close()

	result := &downloadAPI.Result{Requested: len(targets)}

	for _, target := range targets {
		currentDigest := inspectImageDigest(ctx, api, target.ImageName)
		logrus.WithFields(logrus.Fields{
			"image":          target.ImageName,
			"services":       target.Services,
			"current_digest": shortDigestForLog(currentDigest),
		}).Info("Starting image download")

		if err := pullImage(ctx, api, target.ImageName); err != nil {
			result.Failed++
			logrus.WithFields(logrus.Fields{
				"image":          target.ImageName,
				"services":       target.Services,
				"current_digest": shortDigestForLog(currentDigest),
			}).WithError(err).Error("Image download failed")

			return result, fmt.Errorf("download image %s: %w", target.ImageName, err)
		}

		result.Downloaded++
		updatedDigest := inspectImageDigest(ctx, api, target.ImageName)
		logrus.WithFields(logrus.Fields{
			"image":          target.ImageName,
			"services":       target.Services,
			"current_digest": shortDigestForLog(currentDigest),
			"updated_digest": shortDigestForLog(updatedDigest),
			"digest_changed": currentDigest != updatedDigest,
		}).Info("Completed image download")
	}

	return result, nil
}

func resolveDownloadTargets(
	ctx context.Context,
	client container.Client,
	filter types.Filter,
	images []string,
) ([]downloadTarget, error) {
	containers, err := client.ListContainers(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list containers for download: %w", err)
	}

	servicesByImage := make(map[string]map[string]struct{}, len(containers))
	for _, c := range containers {
		imageName := strings.TrimSpace(c.ImageName())
		if imageName == "" {
			continue
		}

		if _, ok := servicesByImage[imageName]; !ok {
			servicesByImage[imageName] = make(map[string]struct{})
		}

		servicesByImage[imageName][c.Name()] = struct{}{}
	}

	imageNames := images
	if len(imageNames) == 0 {
		imageNames = make([]string, 0, len(servicesByImage))
		for imageName := range servicesByImage {
			imageNames = append(imageNames, imageName)
		}
	}

	return buildDownloadTargets(imageNames, servicesByImage), nil
}

func buildDownloadTargets(images []string, servicesByImage map[string]map[string]struct{}) []downloadTarget {
	seen := make(map[string]struct{}, len(images))
	targets := make([]downloadTarget, 0, len(images))

	for _, imageName := range images {
		normalized := strings.TrimSpace(imageName)
		if normalized == "" {
			continue
		}

		if _, ok := seen[normalized]; ok {
			continue
		}

		seen[normalized] = struct{}{}
		targets = append(targets, downloadTarget{
			ImageName: normalized,
			Services:  sortedServiceNames(servicesByImage[normalized]),
		})
	}

	slices.SortFunc(targets, func(a, b downloadTarget) int {
		return strings.Compare(a.ImageName, b.ImageName)
	})

	return targets
}

func sortedServiceNames(services map[string]struct{}) []string {
	if len(services) == 0 {
		return nil
	}

	names := make([]string, 0, len(services))
	for service := range services {
		names = append(names, service)
	}

	slices.Sort(names)

	return names
}

func inspectImageDigest(ctx context.Context, api dockerClient.APIClient, imageName string) string {
	inspect, _, err := api.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return "unknown"
	}

	return preferredImageDigest(inspect)
}

func preferredImageDigest(inspect dockerImage.InspectResponse) string {
	if len(inspect.RepoDigests) > 0 {
		repoDigests := append([]string(nil), inspect.RepoDigests...)
		slices.Sort(repoDigests)

		return repoDigests[0]
	}

	if inspect.ID != "" {
		return inspect.ID
	}

	return "unknown"
}

func shortDigestForLog(digest string) string {
	if digest == "" || digest == "unknown" {
		return "unknown"
	}

	repo, hash, found := strings.Cut(digest, "@")
	if found {
		return repo + "@" + shortHash(hash)
	}

	return shortHash(digest)
}

func shortHash(value string) string {
	if value == "" {
		return "unknown"
	}

	if strings.HasPrefix(value, "sha256:") {
		return "sha256:" + types.ImageID(value).ShortID()
	}

	return types.ImageID(value).ShortID()
}
