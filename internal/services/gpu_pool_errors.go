package services

import (
	"fmt"
	"strings"
)

// SanitizeGPUPoolUserError maps internal worker/script failures to short user-facing text.
// hintMin controls the "try again in X minutes" copy (from pool policy).
func SanitizeGPUPoolUserError(raw string, hintMin int) string {
	if raw == "" {
		return ""
	}
	if hintMin <= 0 {
		hintMin = 15
	}
	retryHint := fmt.Sprintf("Try again in about %d minutes.", hintMin)
	lower := strings.ToLower(raw)

	switch {
	case strings.Contains(lower, "no module named pip"),
		strings.Contains(lower, "prepare_offline"),
		strings.Contains(lower, "e2e-offline-cache"),
		strings.Contains(lower, "download_offline_deps"):
		return "Test resource setup could not finish preparing dependencies. Please try Start session again in a few minutes."

	case strings.Contains(lower, "412"),
		strings.Contains(lower, "precondition failed"):
		return "The test resource was still shutting down from a previous run. Please wait a minute and try Start session again."

	case strings.Contains(lower, "e2e_host_ip"),
		strings.Contains(lower, "e2e_host_ip missing"),
		strings.Contains(lower, "host ip missing"):
		return "Test resource networking could not finish. Please try Start session again."

	case strings.Contains(lower, "maintenance"),
		strings.Contains(lower, "temporarily unavailable"):
		return raw

	case strings.Contains(lower, "failed recreate"),
		strings.Contains(lower, "e2e node") && strings.Contains(lower, "failed"),
		strings.Contains(lower, "stalled in status"),
		strings.Contains(lower, "provider error"),
		strings.Contains(lower, "provision stalled"),
		strings.Contains(lower, "gpu plan temporarily not available"):
		return "All test resources are busy right now. " + retryHint

	case strings.Contains(lower, "timed out"),
		strings.Contains(lower, "stalled"),
		strings.Contains(lower, "ssh not reachable"),
		strings.Contains(lower, "wait-ssh"):
		return "All test resources are busy right now. " + retryHint

	case strings.Contains(lower, "e2e api"),
		strings.Contains(lower, "503"),
		strings.Contains(lower, "502"),
		strings.Contains(lower, "504"):
		return "All test resources are busy right now. " + retryHint

	case strings.Contains(lower, "gateway"),
		strings.Contains(lower, "beta-seed"):
		return "All test resources are busy right now. " + retryHint

	default:
		return "All test resources are busy right now. " + retryHint
	}
}

// IsTerminalProvisionError reports errors that should not be retried (provider gave up).
func IsTerminalProvisionError(raw string) bool {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "failed recreate"),
		strings.Contains(lower, "stalled in status"),
		strings.Contains(lower, "gpu plan temporarily not available"),
		strings.Contains(lower, "missing dist/images.tar.gz"),
		strings.Contains(lower, "build from source"),
		strings.Contains(lower, "docker compose build"),
		strings.Contains(lower, "failed to copy: failed to send write"),
		strings.Contains(lower, "error reading from server: eof"):
		return true
	default:
		return false
	}
}
