package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestParseAISaaSDateRangeRequiresStrictDates(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/admin/ai-saas/services?start_date=2026/07/01", nil)
	if _, _, err := parseAISaaSDateRange(req); err == nil {
		t.Fatal("expected invalid start_date to fail")
	}
}

func TestParseAISaaSDateRangeRejectsInvertedRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/admin/ai-saas/services?start_date=2026-07-10&end_date=2026-07-01", nil)
	if _, _, err := parseAISaaSDateRange(req); err == nil {
		t.Fatal("expected inverted date range to fail")
	}
}

func TestParseAISaaSDateRangeAcceptsValidRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/admin/ai-saas/services?start_date=2026-07-01&end_date=2026-07-10", nil)
	startDate, endDate, err := parseAISaaSDateRange(req)
	if err != nil {
		t.Fatalf("expected valid range, got %v", err)
	}
	if startDate == nil || endDate == nil {
		t.Fatal("expected both dates to be parsed")
	}
	if !endDate.After(*startDate) {
		t.Fatalf("expected end date to include full day, got start=%v end=%v", startDate, endDate)
	}
}
