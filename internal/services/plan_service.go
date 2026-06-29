// internal/services/plan_service.go
package services

import (
	"context"
	"sort"

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
	planRepo repository.PlanRepository
}

func NewPlanService(planRepo repository.PlanRepository) PlanService {
	return &planService{
		planRepo: planRepo,
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

	if req.PriceUSD != nil || req.RazorpayPlanIDUSD != nil || req.PriceINR != nil || req.RazorpayPlanIDINR != nil {
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
