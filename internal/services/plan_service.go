// internal/services/plan_service.go
package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/pkg/billing"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
)

type PlanService interface {
	CreatePlan(ctx context.Context, req *models.CreatePlanRequest) (*models.Plan, error)
	GetActivePlans(ctx context.Context, currency string) ([]models.PublicPlan, error)
	GetAllPlans(ctx context.Context) ([]models.Plan, error)
	GetPlanByID(ctx context.Context, planID string) (*models.Plan, error)
	UpdatePlan(ctx context.Context, planID string, req *models.UpdatePlanRequest) error
	DeletePlan(ctx context.Context, planID string) error
}

type planService struct {
	planRepo            repository.PlanRepository
	razorpayProvisioner RazorpayPlanProvisioner
}

func NewPlanService(planRepo repository.PlanRepository, razorpayProvisioner RazorpayPlanProvisioner) PlanService {
	return &planService{
		planRepo:            planRepo,
		razorpayProvisioner: razorpayProvisioner,
	}
}

func buildPricingFromCreate(req *models.CreatePlanRequest) map[string]models.PlanCurrencyPricing {
	pricing := map[string]models.PlanCurrencyPricing{}
	for currency, item := range req.Pricing {
		if normalized := billing.NormalizeCurrency(currency); normalized != "" && item.Amount > 0 && item.RazorpayPlanID != "" {
			pricing[normalized] = item
		}
	}

	if req.PriceUSD > 0 && req.RazorpayPlanIDUSD != "" {
		pricing[billing.CurrencyUSD] = models.PlanCurrencyPricing{
			Amount:         req.PriceUSD,
			RazorpayPlanID: req.RazorpayPlanIDUSD,
		}
	} else if req.Price > 0 && req.RazorpayPlanID != "" {
		pricing[billing.CurrencyUSD] = models.PlanCurrencyPricing{
			Amount:         req.Price,
			RazorpayPlanID: req.RazorpayPlanID,
		}
	}

	if req.PriceINR > 0 && req.RazorpayPlanIDINR != "" {
		pricing[billing.CurrencyINR] = models.PlanCurrencyPricing{
			Amount:         req.PriceINR,
			RazorpayPlanID: req.RazorpayPlanIDINR,
		}
	}

	return pricing
}

func applyLegacyUSDFields(plan *models.Plan) {
	if plan.Pricing == nil {
		return
	}
	if usd, ok := plan.Pricing[billing.CurrencyUSD]; ok {
		plan.Price = usd.Amount
		plan.RazorpayPlanID = usd.RazorpayPlanID
	}
}

func (s *planService) CreatePlan(ctx context.Context, req *models.CreatePlanRequest) (*models.Plan, error) {
	if !req.ContactSales {
		if err := s.ensureCreateRazorpayPlans(req); err != nil {
			return nil, err
		}
	}

	pricing := buildPricingFromCreate(req)
	if len(pricing) == 0 && !req.ContactSales {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "at least one currency pricing configuration is required", "")
	}

	plan := &models.Plan{
		PlanID:       req.PlanID,
		Name:         req.Name,
		Description:  req.Description,
		Credits:      req.Credits,
		IsActive:     req.IsActive,
		Pricing:      pricing,
		Features:     req.Features,
		DisplayOrder: req.DisplayOrder,
		IsPopular:    req.IsPopular,
		ContactSales: req.ContactSales,
		CtaLabel:     req.CtaLabel,
	}
	applyLegacyUSDFields(plan)

	err := s.planRepo.Create(ctx, plan)
	if err != nil {
		return nil, err
	}

	return plan, nil
}

func (s *planService) GetActivePlans(ctx context.Context, currency string) ([]models.PublicPlan, error) {
	plans, err := s.planRepo.GetAll(ctx, true)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].DisplayOrder != plans[j].DisplayOrder {
			return plans[i].DisplayOrder < plans[j].DisplayOrder
		}
		return plans[i].Price < plans[j].Price
	})

	resolved := make([]models.PublicPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.ContactSales {
			public := billing.ToPublicPlan(plan)
			public.Currency = currency
			resolved = append(resolved, public)
			continue
		}

		view, ok := billing.ToResolvedPlan(plan, currency)
		if !ok {
			continue
		}
		resolved = append(resolved, billing.ToPublicPlan(view))
	}
	return resolved, nil
}

func (s *planService) GetAllPlans(ctx context.Context) ([]models.Plan, error) {
	return s.planRepo.GetAll(ctx, false)
}

func (s *planService) GetPlanByID(ctx context.Context, planID string) (*models.Plan, error) {
	return s.planRepo.GetByPlanID(ctx, planID)
}

func (s *planService) UpdatePlan(ctx context.Context, planID string, req *models.UpdatePlanRequest) error {
	existing, err := s.planRepo.GetByPlanID(ctx, planID)
	if err != nil {
		return err
	}
	if existing.ContactSales {
		return s.updatePlanFields(ctx, planID, req, nil)
	}

	pricing, err := s.resolveUpdatePricing(existing, req)
	if err != nil {
		return err
	}

	var resolvedPricing map[string]models.PlanCurrencyPricing
	if len(pricing) > 0 {
		resolvedPricing = pricing
		req.Pricing = pricing
	}

	return s.updatePlanFields(ctx, planID, req, resolvedPricing)
}

func (s *planService) updatePlanFields(ctx context.Context, planID string, req *models.UpdatePlanRequest, resolvedPricing map[string]models.PlanCurrencyPricing) error {
	updates := bson.M{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Price != nil {
		updates["price"] = *req.Price
	}
	if req.Credits != nil {
		updates["credits"] = *req.Credits
	}
	if req.IsActive != nil {
		updates["isActive"] = *req.IsActive
	}
	if req.RazorpayPlanID != nil {
		updates["razorpayPlanId"] = *req.RazorpayPlanID
	}
	if req.Features != nil {
		updates["features"] = *req.Features
	}
	if req.DisplayOrder != nil {
		updates["displayOrder"] = *req.DisplayOrder
	}
	if req.IsPopular != nil {
		updates["isPopular"] = *req.IsPopular
	}
	if req.ContactSales != nil {
		updates["contactSales"] = *req.ContactSales
	}
	if req.CtaLabel != nil {
		updates["ctaLabel"] = *req.CtaLabel
	}
	if req.Pricing != nil {
		updates["pricing"] = req.Pricing
	}

	if resolvedPricing != nil {
		updates["pricing"] = resolvedPricing
		if usd, ok := resolvedPricing[billing.CurrencyUSD]; ok {
			updates["price"] = usd.Amount
			updates["razorpayPlanId"] = usd.RazorpayPlanID
		}
	} else if req.PriceUSD != nil || req.RazorpayPlanIDUSD != nil || req.PriceINR != nil || req.RazorpayPlanIDINR != nil {
		existing, err := s.planRepo.GetByPlanID(ctx, planID)
		if err != nil {
			return err
		}
		pricing := existing.Pricing
		if pricing == nil {
			pricing = map[string]models.PlanCurrencyPricing{}
		}
		if req.PriceUSD != nil || req.RazorpayPlanIDUSD != nil {
			usd := pricing[billing.CurrencyUSD]
			if req.PriceUSD != nil {
				usd.Amount = *req.PriceUSD
			}
			if req.RazorpayPlanIDUSD != nil {
				usd.RazorpayPlanID = *req.RazorpayPlanIDUSD
			}
			pricing[billing.CurrencyUSD] = usd
			updates["price"] = usd.Amount
			updates["razorpayPlanId"] = usd.RazorpayPlanID
		}
		if req.PriceINR != nil || req.RazorpayPlanIDINR != nil {
			inr := pricing[billing.CurrencyINR]
			if req.PriceINR != nil {
				inr.Amount = *req.PriceINR
			}
			if req.RazorpayPlanIDINR != nil {
				inr.RazorpayPlanID = *req.RazorpayPlanIDINR
			}
			pricing[billing.CurrencyINR] = inr
		}
		updates["pricing"] = pricing
	}

	if len(updates) == 0 {
		return nil
	}

	return s.planRepo.Update(ctx, planID, updates)
}

func (s *planService) DeletePlan(ctx context.Context, planID string) error {
	return s.planRepo.Delete(ctx, planID)
}

func (s *planService) ensureCreateRazorpayPlans(req *models.CreatePlanRequest) error {
	if err := s.ensureCurrencyRazorpayPlan(
		billing.CurrencyUSD,
		firstNonZero(req.PriceUSD, req.Price),
		strings.TrimSpace(firstNonEmpty(req.RazorpayPlanIDUSD, req.RazorpayPlanID)),
		req.Name,
		req.PlanID,
		func(id string) {
			req.RazorpayPlanIDUSD = id
			if req.RazorpayPlanID == "" {
				req.RazorpayPlanID = id
			}
		},
	); err != nil {
		return err
	}

	return s.ensureCurrencyRazorpayPlan(
		billing.CurrencyINR,
		req.PriceINR,
		strings.TrimSpace(req.RazorpayPlanIDINR),
		req.Name,
		req.PlanID,
		func(id string) { req.RazorpayPlanIDINR = id },
	)
}

func (s *planService) resolveUpdatePricing(existing *models.Plan, req *models.UpdatePlanRequest) (map[string]models.PlanCurrencyPricing, error) {
	pricing := map[string]models.PlanCurrencyPricing{}
	if existing.Pricing != nil {
		for currency, item := range existing.Pricing {
			pricing[currency] = item
		}
	}
	if len(pricing) == 0 && existing.Price > 0 && existing.RazorpayPlanID != "" {
		pricing[billing.CurrencyUSD] = models.PlanCurrencyPricing{
			Amount:         existing.Price,
			RazorpayPlanID: existing.RazorpayPlanID,
		}
	}

	planName := existing.Name
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		planName = strings.TrimSpace(*req.Name)
	}

	usdAmount := pricingAmount(pricing, billing.CurrencyUSD, existing.Price)
	if req.PriceUSD != nil {
		usdAmount = *req.PriceUSD
	}
	usdID, err := s.resolveCurrencyRazorpayID(
		billing.CurrencyUSD,
		usdAmount,
		req.RazorpayPlanIDUSD,
		pricing[billing.CurrencyUSD],
		planName,
		existing.PlanID,
	)
	if err != nil {
		return nil, err
	}
	if usdAmount > 0 && usdID != "" {
		pricing[billing.CurrencyUSD] = models.PlanCurrencyPricing{Amount: usdAmount, RazorpayPlanID: usdID}
	}

	inrAmount := pricingAmount(pricing, billing.CurrencyINR, 0)
	if req.PriceINR != nil {
		inrAmount = *req.PriceINR
	}
	inrID, err := s.resolveCurrencyRazorpayID(
		billing.CurrencyINR,
		inrAmount,
		req.RazorpayPlanIDINR,
		pricing[billing.CurrencyINR],
		planName,
		existing.PlanID,
	)
	if err != nil {
		return nil, err
	}
	if inrAmount > 0 && inrID != "" {
		pricing[billing.CurrencyINR] = models.PlanCurrencyPricing{Amount: inrAmount, RazorpayPlanID: inrID}
	}

	if len(pricing) == 0 {
		return nil, nil
	}
	return pricing, nil
}

func (s *planService) resolveCurrencyRazorpayID(
	currency string,
	amount int,
	manualOverride *string,
	existing models.PlanCurrencyPricing,
	planName, automicaPlanID string,
) (string, error) {
	if amount <= 0 {
		return "", nil
	}
	if amount < billing.MinimumCharge(currency) {
		return "", apperrors.NewAppError(apperrors.ErrValidation, 400, fmt.Sprintf("%s price must be at least %s", currency, billing.FormatAmount(billing.MinimumCharge(currency), currency)), "")
	}

	if manualOverride != nil {
		manualID := strings.TrimSpace(*manualOverride)
		if manualID != "" {
			return manualID, nil
		}
	}

	if existing.RazorpayPlanID != "" && existing.Amount == amount {
		return existing.RazorpayPlanID, nil
	}

	return s.createRazorpayPlan(currency, amount, planName, automicaPlanID)
}

func (s *planService) ensureCurrencyRazorpayPlan(
	currency string,
	amount int,
	manualID, planName, automicaPlanID string,
	assign func(string),
) error {
	if amount <= 0 {
		return nil
	}
	if amount < billing.MinimumCharge(currency) {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, fmt.Sprintf("%s price must be at least %s", currency, billing.FormatAmount(billing.MinimumCharge(currency), currency)), "")
	}
	if manualID != "" {
		assign(manualID)
		return nil
	}

	id, err := s.createRazorpayPlan(currency, amount, planName, automicaPlanID)
	if err != nil {
		return err
	}
	assign(id)
	return nil
}

func (s *planService) createRazorpayPlan(currency string, amount int, planName, automicaPlanID string) (string, error) {
	if s.razorpayProvisioner == nil {
		return "", apperrors.NewAppError(apperrors.ErrValidation, 400, "razorpay plan id is required when auto-provisioning is unavailable", "")
	}
	id, err := s.razorpayProvisioner.CreatePlan(currency, amount, planName, automicaPlanID)
	if err != nil {
		return "", apperrors.NewAppError(apperrors.ErrInternalServer, 502, "failed to create razorpay plan", err.Error())
	}
	return id, nil
}

func pricingAmount(pricing map[string]models.PlanCurrencyPricing, currency string, fallback int) int {
	if item, ok := pricing[currency]; ok && item.Amount > 0 {
		return item.Amount
	}
	return fallback
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
