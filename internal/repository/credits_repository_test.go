package repository

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// The deduction must be a single conditional update: the filter has to match
// both the owner and a sufficient balance so the $inc can never go negative.
func TestDeductCreditsFilterRequiresOwnerAndSufficientBalance(t *testing.T) {
	filter := deductCreditsFilter("user-1", 5)

	if got := filter["userId"]; got != "user-1" {
		t.Fatalf("filter userId = %#v, want user-1", got)
	}

	creditsCond, ok := filter["credits"].(bson.M)
	if !ok {
		t.Fatalf("expected credits condition map, got %#v", filter["credits"])
	}
	if got := creditsCond["$gte"]; got != 5 {
		t.Fatalf("credits $gte = %#v, want 5", got)
	}
}
