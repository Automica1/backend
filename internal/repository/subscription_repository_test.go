package repository

import (
	"testing"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
)

func TestBuildSubscriptionUpdateClearsCancelFlagsOnResume(t *testing.T) {
	now := time.Now().UTC()
	sub := &models.Subscription{
		SubscriptionID:     "sub_test",
		UserID:             "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		CancelAtCycleEnd:   false,
		CancelScheduledAt:  nil,
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 20),
		CreatedAt:          now.AddDate(0, -2, 0),
	}

	update, err := buildSubscriptionUpdate(sub)
	if err != nil {
		t.Fatalf("buildSubscriptionUpdate: %v", err)
	}

	setFields, ok := update["$set"].(bson.M)
	if !ok {
		t.Fatalf("expected $set map, got %#v", update["$set"])
	}
	if cancel, ok := setFields["cancelAtCycleEnd"].(bool); !ok || cancel {
		t.Fatalf("expected cancelAtCycleEnd=false in $set, got %#v", setFields["cancelAtCycleEnd"])
	}

	unset, ok := update["$unset"].(bson.M)
	if !ok {
		t.Fatalf("expected $unset map, got %#v", update["$unset"])
	}
	if _, ok := unset["cancelScheduledAt"]; !ok {
		t.Fatalf("expected cancelScheduledAt in $unset, got %#v", unset)
	}
}

func TestBuildSubscriptionUpdateClearsPendingPlanChange(t *testing.T) {
	now := time.Now().UTC()
	sub := &models.Subscription{
		SubscriptionID:     "sub_test",
		UserID:             "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		PendingPlanID:      "",
		PlanChangeDate:     nil,
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 20),
		CreatedAt:          now.AddDate(0, -2, 0),
	}

	update, err := buildSubscriptionUpdate(sub)
	if err != nil {
		t.Fatalf("buildSubscriptionUpdate: %v", err)
	}

	unset, ok := update["$unset"].(bson.M)
	if !ok {
		t.Fatalf("expected $unset map, got %#v", update["$unset"])
	}
	if _, ok := unset["pendingPlanId"]; !ok {
		t.Fatalf("expected pendingPlanId in $unset, got %#v", unset)
	}
	if _, ok := unset["planChangeDate"]; !ok {
		t.Fatalf("expected planChangeDate in $unset, got %#v", unset)
	}
}
