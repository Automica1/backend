package services

import (
	"context"
	"testing"

	"chi-mongo-backend/internal/models"
)

type fakeFacadeAdmin struct {
	actions []models.AdminAuditLog
}

func (f *fakeFacadeAdmin) RecordAction(ctx context.Context, log *models.AdminAuditLog) error {
	f.actions = append(f.actions, *log)
	return nil
}

func (f *fakeFacadeAdmin) GetRecentAuditLogs(ctx context.Context, limit int) (*models.AdminAuditLogListResponse, error) {
	return nil, nil
}
func (f *fakeFacadeAdmin) GetLogs(ctx context.Context, query models.AdminLogQuery) (*models.AdminLogListResponse, error) {
	return nil, nil
}
func (f *fakeFacadeAdmin) Search(ctx context.Context, query string) (*models.AdminSearchResponse, error) {
	return nil, nil
}
func (f *fakeFacadeAdmin) GetSummary(ctx context.Context) (*models.AdminSummaryResponse, error) {
	return nil, nil
}

type fakePoolGetter struct {
	pool *models.GPUPool
}

func (f *fakePoolGetter) GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return f.pool, nil
}

func TestAIServicesFacadeDestroyRequiresConfirm(t *testing.T) {
	admin := &fakeFacadeAdmin{}
	facade := &aiServicesFacade{
		resolver:    NewAISaaSIdentityResolver(),
		admin:       admin,
		gpuPoolRepo: &fakePoolGetter{pool: &models.GPUPool{ServiceTag: "ocr-gpu", RefCount: 0}},
	}
	_, err := facade.RuntimeAction(context.Background(), "admin@automica.ai", "ocr", AIServicesRuntimeActionRequest{
		Action: "destroy",
	})
	if err == nil {
		t.Fatal("expected confirm error")
	}
	if len(admin.actions) != 0 {
		t.Fatalf("expected no audit on validation failure, got %d", len(admin.actions))
	}
}

func TestAIServicesFacadeDestroyBlockedWithActiveSessions(t *testing.T) {
	admin := &fakeFacadeAdmin{}
	facade := &aiServicesFacade{
		resolver: NewAISaaSIdentityResolver(),
		admin:    admin,
		gpuPoolRepo: &fakePoolGetter{pool: &models.GPUPool{
			ServiceTag: "ocr-gpu",
			RefCount:   1,
			Sessions:   []models.GPUPoolSession{{UserID: "u1"}},
		}},
	}
	_, err := facade.RuntimeAction(context.Background(), "admin@automica.ai", "ocr", AIServicesRuntimeActionRequest{
		Action:  "destroy",
		Confirm: "DESTROY",
	})
	if err == nil {
		t.Fatal("expected active session conflict")
	}
	if len(admin.actions) != 1 || admin.actions[0].Outcome != "error" {
		t.Fatalf("expected error audit, got %+v", admin.actions)
	}
}

func TestAIServicesFacadeUnknownAPIID(t *testing.T) {
	facade := &aiServicesFacade{resolver: NewAISaaSIdentityResolver()}
	_, _, err := facade.resolveSingleBetaTag("not-a-real-api")
	if err == nil {
		t.Fatal("expected unknown apiId error")
	}
}

func TestAIServicesFacadeResolveOCRBetaTag(t *testing.T) {
	facade := &aiServicesFacade{resolver: NewAISaaSIdentityResolver()}
	def, tag, err := facade.resolveSingleBetaTag("ocr")
	if err != nil {
		t.Fatal(err)
	}
	if def.APIID != "ocr" {
		t.Fatalf("apiId=%s", def.APIID)
	}
	if tag != "ocr-gpu" {
		t.Fatalf("tag=%s", tag)
	}
}
