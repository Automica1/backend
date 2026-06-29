package billing

import (
	"testing"

	"chi-mongo-backend/internal/models"
)

func TestResolveBillingCurrency(t *testing.T) {
	tests := []struct {
		name string
		in   CurrencyInput
		want string
	}{
		{
			name: "subscription lock",
			in: CurrencyInput{
				SubscriptionCurrency: "USD",
				RequestedCurrency:    "INR",
				Contact:              "+919876543210",
			},
			want: CurrencyUSD,
		},
		{
			name: "user preference",
			in: CurrencyInput{
				UserBillingCurrency: "INR",
				RequestedCurrency:   "USD",
			},
			want: CurrencyINR,
		},
		{
			name: "request",
			in: CurrencyInput{
				RequestedCurrency: "INR",
			},
			want: CurrencyINR,
		},
		{
			name: "indian phone default",
			in: CurrencyInput{
				Contact: "+919876543210",
			},
			want: CurrencyINR,
		},
		{
			name: "default usd",
			in:   CurrencyInput{},
			want: CurrencyUSD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveBillingCurrency(tt.in); got != tt.want {
				t.Fatalf("ResolveBillingCurrency() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePlanPricing(t *testing.T) {
	plan := &models.Plan{
		Price:          1200,
		RazorpayPlanID: "plan_usd",
		Pricing: map[string]models.PlanCurrencyPricing{
			CurrencyUSD: {Amount: 1200, RazorpayPlanID: "plan_usd"},
			CurrencyINR: {Amount: 99900, RazorpayPlanID: "plan_inr"},
		},
	}

	amount, id, ok := ResolvePlanPricing(plan, CurrencyINR)
	if !ok || amount != 99900 || id != "plan_inr" {
		t.Fatalf("INR pricing = %d %s %v, want 99900 plan_inr true", amount, id, ok)
	}

	amount, id, ok = ResolvePlanPricing(plan, CurrencyUSD)
	if !ok || amount != 1200 || id != "plan_usd" {
		t.Fatalf("USD pricing = %d %s %v, want 1200 plan_usd true", amount, id, ok)
	}
}

func TestFormatAmount(t *testing.T) {
	if got := FormatAmount(99900, CurrencyINR); got != "₹999" {
		t.Fatalf("FormatAmount INR = %q", got)
	}
	if got := FormatAmount(1200, CurrencyUSD); got != "$12.00" {
		t.Fatalf("FormatAmount USD = %q", got)
	}
}
