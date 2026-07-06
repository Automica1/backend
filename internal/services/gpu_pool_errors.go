package services

import "strings"

// SanitizeGPUPoolUserError maps internal worker/script failures to short user-facing text.
// Full stderr stays in worker logs only (logs/worker/*.log on dev2).
func SanitizeGPUPoolUserError(raw string) string {
	if raw == "" {
		return ""
	}
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

	case strings.Contains(lower, "failed recreate"),
		strings.Contains(lower, "e2e node") && strings.Contains(lower, "failed"),
		strings.Contains(lower, "stalled in status"),
		strings.Contains(lower, "provider error"),
		strings.Contains(lower, "provision stalled"),
		strings.Contains(lower, "gpu plan temporarily not available"):
		return "All test resources are busy right now. Try again in about 15 minutes."

	case strings.Contains(lower, "timed out"),
		strings.Contains(lower, "stalled"),
		strings.Contains(lower, "ssh not reachable"),
		strings.Contains(lower, "wait-ssh"):
		return "All test resources are busy right now. Try again in about 15 minutes."

	case strings.Contains(lower, "e2e api"),
		strings.Contains(lower, "503"),
		strings.Contains(lower, "502"),
		strings.Contains(lower, "504"):
		return "All test resources are busy right now. Try again in about 15 minutes."

	case strings.Contains(lower, "gateway"),
		strings.Contains(lower, "beta-seed"):
		return "All test resources are busy right now. Try again in about 15 minutes."

	default:
		return "All test resources are busy right now. Try again in about 15 minutes."
	}
}

// IsTerminalProvisionError is true when retrying the same E2E create/wait is unlikely to help.
func IsTerminalProvisionError(raw string) bool {
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "failed recreate") ||
		strings.Contains(lower, "e2e node") && strings.Contains(lower, "failed") ||
		strings.Contains(lower, "stalled in status") ||
		strings.Contains(lower, "provider error")
}