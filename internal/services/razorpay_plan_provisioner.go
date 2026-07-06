package services

import (
	"fmt"
	"strings"

	"chi-mongo-backend/pkg/billing"

	"github.com/razorpay/razorpay-go"
)

// RazorpayPlanProvisioner creates Razorpay subscription plan entities.
type RazorpayPlanProvisioner interface {
	CreatePlan(currency string, amount int, planName, automicaPlanID string) (string, error)
}

type razorpayPlanProvisioner struct {
	client *razorpay.Client
}

func NewRazorpayPlanProvisioner(key, secret string) RazorpayPlanProvisioner {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(secret) == "" {
		return nil
	}
	return &razorpayPlanProvisioner{
		client: razorpay.NewClient(key, secret),
	}
}

func (p *razorpayPlanProvisioner) CreatePlan(currency string, amount int, planName, automicaPlanID string) (string, error) {
	currency = billing.NormalizeCurrency(currency)
	if currency == "" {
		return "", fmt.Errorf("unsupported currency %q", currency)
	}
	if amount < billing.MinimumCharge(currency) {
		return "", fmt.Errorf("amount must be at least %d minor units for %s", billing.MinimumCharge(currency), currency)
	}

	itemName := strings.TrimSpace(planName)
	if itemName == "" {
		itemName = automicaPlanID
	}
	itemName = fmt.Sprintf("%s (%s)", itemName, currency)

	body, err := p.client.Plan.Create(map[string]interface{}{
		"period":   "monthly",
		"interval": 1,
		"item": map[string]interface{}{
			"name":     itemName,
			"amount":   amount,
			"currency": currency,
		},
		"notes": map[string]interface{}{
			"automica_plan_id": automicaPlanID,
		},
	}, nil)
	if err != nil {
		return "", err
	}

	id, ok := body["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("razorpay plan create returned no id")
	}
	return id, nil
}
