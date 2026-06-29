package billing

import (
	"os"
	"strings"
)

// SubscriptionTestResetAllowed reports whether admin test-reset is permitted.
// Requires explicit env opt-in AND Razorpay test-mode keys so prod cannot enable
// this accidentally even if the env var is mis-set.
func SubscriptionTestResetAllowed(razorpayKeyID string) bool {
	if strings.TrimSpace(os.Getenv("ALLOW_SUBSCRIPTION_TEST_RESET")) != "1" {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(razorpayKeyID), "rzp_test_")
}
