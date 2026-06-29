package billing

import (
	"fmt"
	"regexp"
	"strings"

	"chi-mongo-backend/internal/models"
)

const (
	CurrencyUSD = "USD"
	CurrencyINR = "INR"
)

var SupportedCurrencies = []string{CurrencyUSD, CurrencyINR}

var indianPhonePattern = regexp.MustCompile(`^(\+?91)?[6-9]\d{9}$`)

type CurrencyInput struct {
	SubscriptionCurrency string
	UserBillingCurrency  string
	RequestedCurrency    string
	Contact              string
}

// NormalizeCurrency returns a supported currency code or empty string.
func NormalizeCurrency(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case CurrencyUSD:
		return CurrencyUSD
	case CurrencyINR:
		return CurrencyINR
	default:
		return ""
	}
}

// IsIndianPhone reports whether contact looks like an Indian mobile number.
func IsIndianPhone(contact string) bool {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r == '+' {
			return r
		}
		return -1
	}, strings.TrimSpace(contact))
	if digits == "" {
		return false
	}
	return indianPhonePattern.MatchString(digits)
}

// ResolveBillingCurrency picks the billing currency using the priority chain from the plan.
func ResolveBillingCurrency(in CurrencyInput) string {
	if c := NormalizeCurrency(in.SubscriptionCurrency); c != "" {
		return c
	}
	if c := NormalizeCurrency(in.UserBillingCurrency); c != "" {
		return c
	}
	if c := NormalizeCurrency(in.RequestedCurrency); c != "" {
		return c
	}
	if IsIndianPhone(in.Contact) {
		return CurrencyINR
	}
	return CurrencyUSD
}

// ResolvePlanPricing returns amount (minor units) and Razorpay plan ID for the currency.
func ResolvePlanPricing(plan *models.Plan, currency string) (amount int, razorpayPlanID string, ok bool) {
	if plan == nil {
		return 0, "", false
	}

	currency = NormalizeCurrency(currency)
	if currency == "" {
		currency = CurrencyUSD
	}

	if plan.Pricing != nil {
		if p, exists := plan.Pricing[currency]; exists && p.Amount > 0 && p.RazorpayPlanID != "" {
			return p.Amount, p.RazorpayPlanID, true
		}
	}

	// Legacy USD fields
	if currency == CurrencyUSD && plan.Price > 0 && plan.RazorpayPlanID != "" {
		return plan.Price, plan.RazorpayPlanID, true
	}

	return 0, "", false
}

// PlanAmountInCurrency returns the plan amount for comparisons (upgrade proration).
func PlanAmountInCurrency(plan *models.Plan, currency string) int {
	amount, _, ok := ResolvePlanPricing(plan, currency)
	if ok {
		return amount
	}
	return 0
}

// MinimumCharge returns the smallest charge allowed for a currency (e.g. $1 or ₹1).
func MinimumCharge(currency string) int {
	return 100
}

// FormatAmount formats minor units for display in emails and admin views.
func FormatAmount(amount int, currency string) string {
	switch NormalizeCurrency(currency) {
	case CurrencyINR:
		major := amount / 100
		if amount%100 == 0 {
			return fmt.Sprintf("₹%d", major)
		}
		return fmt.Sprintf("₹%.2f", float64(amount)/100)
	default:
		return fmt.Sprintf("$%.2f", float64(amount)/100)
	}
}

// ToResolvedPlan returns a copy of the plan with price/razorpayPlanId/currency set for the requested currency.
func ToResolvedPlan(plan models.Plan, currency string) (models.Plan, bool) {
	currency = NormalizeCurrency(currency)
	if currency == "" {
		currency = CurrencyUSD
	}

	amount, razorpayID, ok := ResolvePlanPricing(&plan, currency)
	if !ok {
		return plan, false
	}

	plan.Price = amount
	plan.RazorpayPlanID = razorpayID
	plan.Currency = currency
	return plan, true
}
