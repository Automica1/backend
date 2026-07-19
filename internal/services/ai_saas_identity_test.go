package services

import "testing"

func TestAISaaSIdentityResolverKeepsAPIAsPrimaryUnit(t *testing.T) {
	resolver := NewAISaaSIdentityResolver()

	def, ok, ambiguous := resolver.Resolve(aliasKindGPUServiceTag, "ocr-gpu")
	if !ok || ambiguous {
		t.Fatalf("expected ocr-gpu to resolve cleanly, ok=%v ambiguous=%v", ok, ambiguous)
	}
	if def.APIID != "ocr" {
		t.Fatalf("expected GPU tag to map to apiId ocr, got %q", def.APIID)
	}

	def, ok, ambiguous = resolver.Resolve(aliasKindPipelineService, "sign_verify_vlm_gpu")
	if !ok || ambiguous {
		t.Fatalf("expected signature pipeline alias to resolve cleanly, ok=%v ambiguous=%v", ok, ambiguous)
	}
	if def.APIID != "signature-verification" {
		t.Fatalf("expected pipeline alias to map to signature-verification, got %q", def.APIID)
	}
}

func TestAISaaSIdentityResolverMapsHistoricalSignatureBetaUsageName(t *testing.T) {
	resolver := NewAISaaSIdentityResolver()

	def, ok, ambiguous := resolver.Resolve(aliasKindUsageName, "signature-verification-beta")
	if !ok || ambiguous {
		t.Fatalf("expected signature-verification-beta usage name to resolve cleanly, ok=%v ambiguous=%v", ok, ambiguous)
	}
	if def.APIID != "signature-verification" {
		t.Fatalf("expected historical usage name to map to signature-verification, got %q", def.APIID)
	}

	matches := resolver.Matches(aliasKindUsageName, "signature-verification-beta")
	if len(matches) != 1 {
		t.Fatalf("expected exactly one apiId match for signature-verification-beta usage name, got %d", len(matches))
	}
}

func TestAISaaSSanitizeTextRedactsSecrets(t *testing.T) {
	raw := "deploy failed access_key=AKIAABCDEFGHIJKLMNOP token=my-secret-value"
	got := sanitizeAISaaSText(raw, 0)

	if got == raw {
		t.Fatal("expected secret-like values to be redacted")
	}
	if containsAny(got, "AKIAABCDEFGHIJKLMNOP", "my-secret-value") {
		t.Fatalf("expected redacted output, got %q", got)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && len(value) >= len(needle) {
			for i := 0; i+len(needle) <= len(value); i++ {
				if value[i:i+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}
