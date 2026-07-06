package services

import (
	"context"
	"testing"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/pkg/billing"

	"go.mongodb.org/mongo-driver/bson"
)

type fakePlanRepo struct {
	plans map[string]*models.Plan
}

func newFakePlanRepo() *fakePlanRepo {
	return &fakePlanRepo{plans: map[string]*models.Plan{}}
}

func (f *fakePlanRepo) Create(_ context.Context, plan *models.Plan) error {
	f.plans[plan.PlanID] = plan
	return nil
}

func (f *fakePlanRepo) GetAll(_ context.Context, onlyActive bool) ([]models.Plan, error) {
	out := make([]models.Plan, 0, len(f.plans))
	for _, plan := range f.plans {
		if onlyActive && !plan.IsActive {
			continue
		}
		out = append(out, *plan)
	}
	return out, nil
}

func (f *fakePlanRepo) GetByPlanID(_ context.Context, planID string) (*models.Plan, error) {
	plan, ok := f.plans[planID]
	if !ok {
		return nil, errNotFound("plan not found")
	}
	copy := *plan
	return &copy, nil
}

func (f *fakePlanRepo) Update(_ context.Context, planID string, updates bson.M) error {
	plan, ok := f.plans[planID]
	if !ok {
		return errNotFound("plan not found")
	}
	if v, ok := updates["pricing"].(map[string]models.PlanCurrencyPricing); ok {
		plan.Pricing = v
	}
	if v, ok := updates["price"].(int); ok {
		plan.Price = v
	}
	if v, ok := updates["razorpayPlanId"].(string); ok {
		plan.RazorpayPlanID = v
	}
	return nil
}

func (f *fakePlanRepo) Delete(_ context.Context, planID string) error {
	plan, ok := f.plans[planID]
	if !ok {
		return errNotFound("plan not found")
	}
	plan.IsActive = false
	return nil
}

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

type fakeRazorpayPlanProvisioner struct {
	created []provisionCall
	err     error
}

type provisionCall struct {
	currency       string
	amount         int
	planName       string
	automicaPlanID string
}

func (f *fakeRazorpayPlanProvisioner) CreatePlan(currency string, amount int, planName, automicaPlanID string) (string, error) {
	f.created = append(f.created, provisionCall{
		currency:       currency,
		amount:         amount,
		planName:       planName,
		automicaPlanID: automicaPlanID,
	})
	if f.err != nil {
		return "", f.err
	}
	return "plan_auto_" + currency, nil
}

func TestCreatePlanAutoProvisionsRazorpayIDs(t *testing.T) {
	repo := newFakePlanRepo()
	provisioner := &fakeRazorpayPlanProvisioner{}
	svc := NewPlanService(repo, provisioner)

	plan, err := svc.CreatePlan(context.Background(), &models.CreatePlanRequest{
		PlanID:   "tier-test",
		Name:     "Test Plan",
		Credits:  100,
		IsActive: true,
		PriceUSD: 2900,
		PriceINR: 249900,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if len(provisioner.created) != 2 {
		t.Fatalf("expected 2 razorpay creates, got %d", len(provisioner.created))
	}
	if plan.Pricing[billing.CurrencyUSD].RazorpayPlanID != "plan_auto_USD" {
		t.Fatalf("unexpected USD razorpay id: %q", plan.Pricing[billing.CurrencyUSD].RazorpayPlanID)
	}
	if plan.Pricing[billing.CurrencyINR].RazorpayPlanID != "plan_auto_INR" {
		t.Fatalf("unexpected INR razorpay id: %q", plan.Pricing[billing.CurrencyINR].RazorpayPlanID)
	}
}

func TestCreatePlanKeepsManualRazorpayIDs(t *testing.T) {
	repo := newFakePlanRepo()
	provisioner := &fakeRazorpayPlanProvisioner{}
	svc := NewPlanService(repo, provisioner)

	plan, err := svc.CreatePlan(context.Background(), &models.CreatePlanRequest{
		PlanID:            "tier-manual",
		Name:              "Manual Plan",
		Credits:           50,
		IsActive:          true,
		PriceUSD:          1900,
		RazorpayPlanIDUSD: "plan_manual_usd",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if len(provisioner.created) != 0 {
		t.Fatalf("expected no auto provision, got %d calls", len(provisioner.created))
	}
	if plan.Pricing[billing.CurrencyUSD].RazorpayPlanID != "plan_manual_usd" {
		t.Fatalf("unexpected USD razorpay id: %q", plan.Pricing[billing.CurrencyUSD].RazorpayPlanID)
	}
}

func TestUpdatePlanRecreatesRazorpayWhenPriceChanges(t *testing.T) {
	repo := newFakePlanRepo()
	provisioner := &fakeRazorpayPlanProvisioner{}
	svc := NewPlanService(repo, provisioner)

	_, err := svc.CreatePlan(context.Background(), &models.CreatePlanRequest{
		PlanID:            "tier-update",
		Name:              "Update Plan",
		Credits:           100,
		IsActive:          true,
		PriceUSD:          2900,
		RazorpayPlanIDUSD: "plan_old_usd",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	provisioner.created = nil
	newPrice := 3900
	if err := svc.UpdatePlan(context.Background(), "tier-update", &models.UpdatePlanRequest{
		PriceUSD: &newPrice,
	}); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if len(provisioner.created) != 1 {
		t.Fatalf("expected 1 razorpay create on price change, got %d", len(provisioner.created))
	}
	updated, _ := repo.GetByPlanID(context.Background(), "tier-update")
	if updated.Pricing[billing.CurrencyUSD].RazorpayPlanID != "plan_auto_USD" {
		t.Fatalf("expected new razorpay id, got %q", updated.Pricing[billing.CurrencyUSD].RazorpayPlanID)
	}
	if updated.Pricing[billing.CurrencyUSD].Amount != 3900 {
		t.Fatalf("expected updated amount 3900, got %d", updated.Pricing[billing.CurrencyUSD].Amount)
	}
}
