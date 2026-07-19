package services

import "testing"

func TestResolveRegistryRepository(t *testing.T) {
	cases := []struct {
		ns, img, want string
	}{
		{"automica-ai", "sign_verify_vlm_gpu", "automica-ai/sign_verify_vlm_gpu"},
		{"automica-ai/ocr-api", "ocr-api", "automica-ai/ocr-api"},
		{"automica-ai/ocr-api", "mineru-engine", "automica-ai/mineru-engine"},
		{"", "ocr-api", "automica-ai/ocr-api"},
	}
	for _, tc := range cases {
		got := resolveRegistryRepository(tc.ns, tc.img)
		if got != tc.want {
			t.Fatalf("resolveRegistryRepository(%q,%q)=%q want %q", tc.ns, tc.img, got, tc.want)
		}
	}
}

func TestDefaultRegistryImageName(t *testing.T) {
	if got := defaultRegistryImageName("sign-verify-vlm-gpu", "vlm-gpu"); got != "sign_verify_vlm_gpu" {
		t.Fatalf("got %q", got)
	}
	if got := defaultRegistryImageName("ocr", "ocr-gpu"); got != "ocr-api" {
		t.Fatalf("got %q", got)
	}
}
