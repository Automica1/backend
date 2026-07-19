package services

import (
	"regexp"
	"sort"
	"strings"

	"chi-mongo-backend/internal/models"
)

const (
	aliasKindCatalogSlug     = "catalogSlug"
	aliasKindUsageName       = "usageName"
	aliasKindBetaServiceName = "betaServiceName"
	aliasKindBetaServiceTag  = "betaServiceTag"
	aliasKindGPUServiceTag   = "gpuServiceTag"
	aliasKindPipelineService = "pipelineService"
	aliasKindFeedbackService = "feedbackServiceName"
)

var aiSaaSAliasTokenPattern = regexp.MustCompile(`[^a-z0-9]+`)

type AISaaSAliasDefinition struct {
	APIID                string
	Slug                 string
	DisplayName          string
	CatalogSlugs         []string
	UsageNames           []string
	BetaServiceNames     []string
	BetaServiceTags      []string
	GPUServiceTags       []string
	PipelineServices     []string
	FeedbackServiceNames []string
}

type aiSaaSAliasMatch struct {
	APIID   string
	Kind    string
	Value   string
	Aliases []string
}

type AISaaSIdentityResolver struct {
	definitions []AISaaSAliasDefinition
	byAPIID     map[string]AISaaSAliasDefinition
	byAlias     map[string][]aiSaaSAliasMatch
}

func NewAISaaSIdentityResolver() *AISaaSIdentityResolver {
	defs := defaultAISaaSAliasDefinitions()
	resolver := &AISaaSIdentityResolver{
		definitions: defs,
		byAPIID:     make(map[string]AISaaSAliasDefinition, len(defs)),
		byAlias:     make(map[string][]aiSaaSAliasMatch),
	}

	for _, def := range defs {
		def = normalizeAISaaSAliasDefinition(def)
		resolver.byAPIID[def.APIID] = def
		resolver.index(def, aliasKindCatalogSlug, def.CatalogSlugs)
		resolver.index(def, aliasKindUsageName, def.UsageNames)
		resolver.index(def, aliasKindBetaServiceName, def.BetaServiceNames)
		resolver.index(def, aliasKindBetaServiceTag, def.BetaServiceTags)
		resolver.index(def, aliasKindGPUServiceTag, def.GPUServiceTags)
		resolver.index(def, aliasKindPipelineService, def.PipelineServices)
		resolver.index(def, aliasKindFeedbackService, def.FeedbackServiceNames)
	}

	return resolver
}

func (r *AISaaSIdentityResolver) Definitions() []AISaaSAliasDefinition {
	out := make([]AISaaSAliasDefinition, 0, len(r.definitions))
	for _, def := range r.definitions {
		out = append(out, def)
	}
	return out
}

func (r *AISaaSIdentityResolver) Definition(apiID string) (AISaaSAliasDefinition, bool) {
	def, ok := r.byAPIID[normalizeAISaaSAlias(apiID)]
	return def, ok
}

func (r *AISaaSIdentityResolver) Resolve(kind, value string) (AISaaSAliasDefinition, bool, bool) {
	matches := r.matches(kind, value)
	if len(matches) != 1 {
		return AISaaSAliasDefinition{}, false, len(matches) > 1
	}
	def, ok := r.byAPIID[matches[0].APIID]
	return def, ok, false
}

func (r *AISaaSIdentityResolver) Matches(kind, value string) []aiSaaSAliasMatch {
	return r.matches(kind, value)
}

func (r *AISaaSIdentityResolver) ToModelAliases(def AISaaSAliasDefinition) models.AISaaSAliases {
	return models.AISaaSAliases{
		CatalogSlugs:         uniqueSortedStrings(def.CatalogSlugs),
		UsageNames:           uniqueSortedStrings(def.UsageNames),
		BetaServiceNames:     uniqueSortedStrings(def.BetaServiceNames),
		BetaServiceTags:      uniqueSortedStrings(def.BetaServiceTags),
		GPUServiceTags:       uniqueSortedStrings(def.GPUServiceTags),
		PipelineServices:     uniqueSortedStrings(def.PipelineServices),
		FeedbackServiceNames: uniqueSortedStrings(def.FeedbackServiceNames),
	}
}

func (r *AISaaSIdentityResolver) index(def AISaaSAliasDefinition, kind string, values []string) {
	for _, value := range values {
		key := aliasLookupKey(kind, value)
		if key == "" {
			continue
		}
		r.byAlias[key] = append(r.byAlias[key], aiSaaSAliasMatch{
			APIID:   def.APIID,
			Kind:    kind,
			Value:   normalizeAISaaSAlias(value),
			Aliases: values,
		})
	}
}

func (r *AISaaSIdentityResolver) matches(kind, value string) []aiSaaSAliasMatch {
	key := aliasLookupKey(kind, value)
	if key == "" {
		return nil
	}
	matches := r.byAlias[key]
	out := make([]aiSaaSAliasMatch, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		if seen[match.APIID] {
			continue
		}
		seen[match.APIID] = true
		out = append(out, match)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].APIID < out[j].APIID })
	return out
}

func aliasLookupKey(kind, value string) string {
	normalized := normalizeAISaaSAlias(value)
	if kind == "" || normalized == "" {
		return ""
	}
	return kind + ":" + normalized
}

// NormalizeAISaaSAliasValue exposes alias normalization so facade layers
// (admin AI Services routes) compare client-supplied profile values against
// the catalog using the exact same rules.
func NormalizeAISaaSAliasValue(value string) string {
	return normalizeAISaaSAlias(value)
}

func normalizeAISaaSAlias(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = aiSaaSAliasTokenPattern.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func normalizeAISaaSAliasDefinition(def AISaaSAliasDefinition) AISaaSAliasDefinition {
	def.APIID = normalizeAISaaSAlias(def.APIID)
	def.Slug = normalizeAISaaSAlias(def.Slug)
	def.CatalogSlugs = uniqueSortedStrings(append(def.CatalogSlugs, def.Slug))
	def.UsageNames = uniqueSortedStrings(def.UsageNames)
	def.BetaServiceNames = uniqueSortedStrings(def.BetaServiceNames)
	def.BetaServiceTags = uniqueSortedStrings(def.BetaServiceTags)
	def.GPUServiceTags = uniqueSortedStrings(def.GPUServiceTags)
	def.PipelineServices = uniqueSortedStrings(def.PipelineServices)
	def.FeedbackServiceNames = uniqueSortedStrings(def.FeedbackServiceNames)
	return def
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeAISaaSAlias(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func defaultAISaaSAliasDefinitions() []AISaaSAliasDefinition {
	return []AISaaSAliasDefinition{
		{
			APIID:                "ocr",
			Slug:                 "ocr",
			DisplayName:          "OCR",
			CatalogSlugs:         []string{"ocr"},
			UsageNames:           []string{"ocr"},
			BetaServiceNames:     []string{"ocr"},
			BetaServiceTags:      []string{"ocr-gpu"},
			GPUServiceTags:       []string{"ocr-gpu"},
			PipelineServices:     []string{"ocr"},
			FeedbackServiceNames: []string{"ocr"},
		},
		{
			APIID:        "signature-verification",
			Slug:         "signature-verification",
			DisplayName:  "Signature Verification",
			CatalogSlugs: []string{"signature-verification"},
			// "signature-verification-beta" is a historical usage ServiceName still
			// present in live usage data; keep it as an explicit compatibility alias.
			UsageNames:           []string{"signature-verification", "signature-verification-beta"},
			BetaServiceNames:     []string{"signature-verification"},
			// vlm-e2e-gpu remains a beta-key / pool compat alias; canonical GPU tag is vlm-gpu.
			BetaServiceTags:      []string{"signature-verification-beta", "vlm-gpu", "vlm-e2e-gpu"},
			GPUServiceTags:       []string{"vlm-gpu"},
			PipelineServices:     []string{"sign-verify-vlm-gpu", "sign_verify_vlm_gpu"},
			FeedbackServiceNames: []string{"signature-verification"},
		},
		{
			APIID:                "qr-extract",
			Slug:                 "qr-extract",
			DisplayName:          "QR Extraction",
			CatalogSlugs:         []string{"qr-extract", "qr-extraction"},
			UsageNames:           []string{"qr-extract", "qr-extraction"},
			BetaServiceNames:     []string{"qr-extract", "qr-extraction"},
			FeedbackServiceNames: []string{"qr-extract", "qr-extraction"},
		},
		{
			APIID:                "qr-masking",
			Slug:                 "qr-masking",
			DisplayName:          "QR Masking",
			CatalogSlugs:         []string{"qr-masking"},
			UsageNames:           []string{"qr-masking"},
			BetaServiceNames:     []string{"qr-masking"},
			FeedbackServiceNames: []string{"qr-masking"},
		},
		{
			APIID:                "id-crop",
			Slug:                 "id-crop",
			DisplayName:          "ID Cropping",
			CatalogSlugs:         []string{"id-crop", "id-cropping"},
			UsageNames:           []string{"id-crop", "id-cropping"},
			BetaServiceNames:     []string{"id-crop", "id-cropping"},
			FeedbackServiceNames: []string{"id-crop", "id-cropping"},
		},
		{
			APIID:                "document-enhancement",
			Slug:                 "document-enhancement",
			DisplayName:          "Document Enhancement",
			CatalogSlugs:         []string{"document-enhancement"},
			UsageNames:           []string{"document-enhancement"},
			BetaServiceNames:     []string{"document-enhancement"},
			FeedbackServiceNames: []string{"document-enhancement"},
		},
		{
			APIID:                "face-verify",
			Slug:                 "face-verify",
			DisplayName:          "Face Verification",
			CatalogSlugs:         []string{"face-verify", "face-verification"},
			UsageNames:           []string{"face-verify", "face-verification"},
			BetaServiceNames:     []string{"face-verify", "face-verification"},
			FeedbackServiceNames: []string{"face-verify", "face-verification"},
		},
		{
			APIID:                "face-detect",
			Slug:                 "face-detect",
			DisplayName:          "Face Detection",
			CatalogSlugs:         []string{"face-detect", "face-cropping", "face-detection"},
			UsageNames:           []string{"face-detect", "face-cropping", "face-detection"},
			BetaServiceNames:     []string{"face-detect", "face-cropping", "face-detection"},
			FeedbackServiceNames: []string{"face-detect", "face-cropping", "face-detection"},
		},
		{
			APIID:                "speech-to-text",
			Slug:                 "speech-to-text",
			DisplayName:          "Speech to Text",
			CatalogSlugs:         []string{"speech-to-text"},
			UsageNames:           []string{"speech-to-text"},
			BetaServiceNames:     []string{"speech-to-text"},
			FeedbackServiceNames: []string{"speech-to-text"},
		},
		{
			APIID:                "text-to-speech",
			Slug:                 "text-to-speech",
			DisplayName:          "Text to Speech",
			CatalogSlugs:         []string{"text-to-speech"},
			UsageNames:           []string{"text-to-speech"},
			BetaServiceNames:     []string{"text-to-speech"},
			FeedbackServiceNames: []string{"text-to-speech"},
		},
	}
}
