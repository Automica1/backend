package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

const registryCatalogTagLimit = 30

var ecrHostRegion = regexp.MustCompile(`^[0-9]+\.dkr\.ecr\.([^.]+)\.amazonaws\.com(?:\.cn)?$`)

// ListRegistryImages returns live tags for the service's configured ECR/GHCR repository.
func (s *aiServicesFacade) ListRegistryImages(ctx context.Context, apiID, runtimeProfile, providerOverride, imageOverride string) (*models.RegistryImageCatalog, error) {
	def, err := s.ResolveDefinition(apiID)
	if err != nil {
		return nil, err
	}
	serviceTag, err := s.ResolveGPUServiceTag(apiID, runtimeProfile)
	if err != nil {
		return nil, err
	}
	ownerTag := models.CanonicalGPUServiceTag(serviceTag)
	svc, err := s.betaServices.GetByTag(ctx, ownerTag)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		// Fall back to legacy alias owner when canonical has no doc yet.
		svc, err = s.betaServices.GetByTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
	}

	settings := &models.BetaServiceRegistrySettings{}
	if svc != nil && svc.RegistrySettings != nil {
		*settings = *svc.RegistrySettings
	}
	settings.ApplyAutomicaDefaults()
	if providerOverride != "" {
		settings.Provider = strings.ToLower(strings.TrimSpace(providerOverride))
		settings.ApplyAutomicaDefaults()
	}
	provider := strings.ToLower(strings.TrimSpace(settings.Provider))
	if provider != "ecr" && provider != "ghcr" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest,
			"registry catalog requires provider ecr or ghcr (set registry on this service first)")
	}

	pipelineName := ""
	if len(def.PipelineServices) > 0 {
		pipelineName = def.PipelineServices[0]
	}
	if pipelineName == "" {
		pipelineName = models.ServiceTagToPipelineService[ownerTag]
	}
	imageName := strings.TrimSpace(imageOverride)
	if imageName == "" {
		imageName = defaultRegistryImageName(pipelineName, ownerTag)
	}
	repo := resolveRegistryRepository(settings.Namespace, imageName)
	related := relatedRegistryImages(pipelineName, ownerTag)

	catalog := &models.RegistryImageCatalog{
		Provider:     provider,
		Server:       settings.Server,
		Namespace:    settings.Namespace,
		Repository:   repo,
		Region:       settings.Region,
		RelatedRepos: related,
		Tags:         []models.RegistryImageTag{},
	}

	switch provider {
	case "ecr":
		region := settings.Region
		if region == "" && settings.Server != "" {
			if m := ecrHostRegion.FindStringSubmatch(settings.Server); len(m) == 2 {
				region = m[1]
			}
		}
		if region == "" {
			region = models.AutomicaECRRegion
		}
		catalog.Region = region
		tags, src, listErr := listECRImageTags(ctx, region, repo)
		catalog.Source = src
		if listErr != nil {
			return nil, apperrors.NewAppError(apperrors.ErrInternalServer, http.StatusBadGateway,
				"ECR list failed: "+listErr.Error())
		}
		catalog.Tags = tags
	case "ghcr":
		tags, src, listErr := listGHCRImageTags(ctx, settings.Namespace, imageName)
		catalog.Source = src
		if listErr != nil {
			return nil, apperrors.NewAppError(apperrors.ErrInternalServer, http.StatusBadGateway,
				"GHCR list failed: "+listErr.Error())
		}
		catalog.Tags = tags
	}
	if len(catalog.Tags) == 0 {
		catalog.Message = "No tagged images found in this repository."
	}
	return catalog, nil
}

func defaultRegistryImageName(pipelineName, serviceTag string) string {
	p := strings.ToLower(pipelineName)
	switch {
	case strings.Contains(p, "ocr") || serviceTag == models.OCRGPUServiceTag:
		return "ocr-api"
	case strings.Contains(p, "sign") || serviceTag == models.VLMGPUServiceTag || serviceTag == models.VLMGPUServiceTagLegacy:
		return "sign_verify_vlm_gpu"
	default:
		if pipelineName != "" {
			return strings.ReplaceAll(pipelineName, "-", "_")
		}
		return "sign_verify_vlm_gpu"
	}
}

func relatedRegistryImages(pipelineName, serviceTag string) []string {
	p := strings.ToLower(pipelineName)
	if strings.Contains(p, "ocr") || serviceTag == models.OCRGPUServiceTag {
		return []string{"ocr-api", "mineru-engine", "ocr-canary"}
	}
	return []string{"sign_verify_vlm_gpu"}
}

func resolveRegistryRepository(namespace, imageName string) string {
	ns := strings.Trim(strings.TrimSpace(strings.ToLower(namespace)), "/")
	img := strings.Trim(strings.TrimSpace(strings.ToLower(imageName)), "/")
	if ns == "" {
		ns = models.AutomicaRegistryNamespace
	}
	// Namespace already includes image path (automica-ai/ocr-api).
	if strings.Contains(ns, "/") {
		parts := strings.Split(ns, "/")
		last := parts[len(parts)-1]
		if last == img || img == "" {
			return ns
		}
		// org/name + different image → org/image
		return parts[0] + "/" + img
	}
	if img == "" {
		return ns
	}
	return ns + "/" + img
}

func listECRImageTags(ctx context.Context, region, repository string) ([]models.RegistryImageTag, string, error) {
	cmd := exec.CommandContext(ctx, "aws", "ecr", "describe-images",
		"--region", region,
		"--repository-name", repository,
		"--output", "json",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "aws-cli", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var parsed struct {
		ImageDetails []struct {
			ImageTags      []string `json:"imageTags"`
			ImagePushedAt  string   `json:"imagePushedAt"`
			ImageSizeBytes int64    `json:"imageSizeInBytes"`
			ImageDigest    string   `json:"imageDigest"`
		} `json:"imageDetails"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, "aws-cli", err
	}
	type row struct {
		tag models.RegistryImageTag
		at  time.Time
	}
	var rows []row
	for _, d := range parsed.ImageDetails {
		var pushed *time.Time
		var at time.Time
		if d.ImagePushedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, d.ImagePushedAt); err == nil {
				at = t
				pushed = &t
			} else if t, err := time.Parse(time.RFC3339, d.ImagePushedAt); err == nil {
				at = t
				pushed = &t
			}
		}
		tags := d.ImageTags
		if len(tags) == 0 {
			tags = []string{""}
		}
		for _, name := range tags {
			if strings.TrimSpace(name) == "" {
				continue
			}
			rows = append(rows, row{
				tag: models.RegistryImageTag{
					Name:      name,
					PushedAt:  pushed,
					SizeBytes: d.ImageSizeBytes,
					Digest:    d.ImageDigest,
				},
				at: at,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].at.Equal(rows[j].at) {
			return rows[i].tag.Name > rows[j].tag.Name
		}
		return rows[i].at.After(rows[j].at)
	})
	outTags := make([]models.RegistryImageTag, 0, registryCatalogTagLimit)
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.tag.Name] {
			continue
		}
		seen[r.tag.Name] = true
		outTags = append(outTags, r.tag)
		if len(outTags) >= registryCatalogTagLimit {
			break
		}
	}
	return outTags, "aws-cli", nil
}

func listGHCRImageTags(ctx context.Context, namespace, imageName string) ([]models.RegistryImageTag, string, error) {
	token := strings.TrimSpace(os.Getenv("GHCR_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token == "" {
		return nil, "github-api", fmt.Errorf("GHCR_TOKEN (or GITHUB_TOKEN) is not set on the API host")
	}
	owner := strings.Trim(strings.TrimSpace(namespace), "/")
	if i := strings.Index(owner, "/"); i > 0 {
		owner = owner[:i]
	}
	if owner == "" {
		owner = models.AutomicaRegistryNamespace
	}
	pkg := strings.Trim(strings.TrimSpace(imageName), "/")
	if pkg == "" {
		return nil, "github-api", fmt.Errorf("package name is empty")
	}

	urls := []string{
		fmt.Sprintf("https://api.github.com/orgs/%s/packages/container/%s/versions?per_page=%d", owner, pkg, registryCatalogTagLimit),
		fmt.Sprintf("https://api.github.com/users/%s/packages/container/%s/versions?per_page=%d", owner, pkg, registryCatalogTagLimit),
	}
	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, "github-api", err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "automica-admin-registry-catalog")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("package not found at %s", u)
			continue
		}
		if resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("github HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			continue
		}
		var versions []struct {
			UpdatedAt string `json:"updated_at"`
			Name      string `json:"name"`
			Metadata  struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(body, &versions); err != nil {
			return nil, "github-api", err
		}
		type row struct {
			tag models.RegistryImageTag
			at  time.Time
		}
		var rows []row
		for _, v := range versions {
			var pushed *time.Time
			var at time.Time
			if t, err := time.Parse(time.RFC3339, v.UpdatedAt); err == nil {
				at = t
				pushed = &t
			}
			tags := v.Metadata.Container.Tags
			if len(tags) == 0 && v.Name != "" {
				tags = []string{v.Name}
			}
			for _, name := range tags {
				if strings.TrimSpace(name) == "" {
					continue
				}
				rows = append(rows, row{
					tag: models.RegistryImageTag{Name: name, PushedAt: pushed},
					at:  at,
				})
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
		outTags := make([]models.RegistryImageTag, 0, registryCatalogTagLimit)
		seen := map[string]bool{}
		for _, r := range rows {
			if seen[r.tag.Name] {
				continue
			}
			seen[r.tag.Name] = true
			outTags = append(outTags, r.tag)
			if len(outTags) >= registryCatalogTagLimit {
				break
			}
		}
		return outTags, "github-api", nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("GHCR package not found")
	}
	return nil, "github-api", lastErr
}
