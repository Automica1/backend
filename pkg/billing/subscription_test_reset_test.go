package billing

import (
	"os"
	"testing"
)

func TestSubscriptionTestResetAllowed(t *testing.T) {
	t.Setenv("ALLOW_SUBSCRIPTION_TEST_RESET", "1")
	t.Cleanup(func() {
		_ = os.Unsetenv("ALLOW_SUBSCRIPTION_TEST_RESET")
	})

	if !SubscriptionTestResetAllowed("rzp_test_abc") {
		t.Fatal("expected test reset to be allowed with test key")
	}
	if SubscriptionTestResetAllowed("rzp_live_abc") {
		t.Fatal("expected test reset to be blocked with live key")
	}

	_ = os.Unsetenv("ALLOW_SUBSCRIPTION_TEST_RESET")
	if SubscriptionTestResetAllowed("rzp_test_abc") {
		t.Fatal("expected test reset to be blocked without env opt-in")
	}
}
