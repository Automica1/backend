package services

import (
	"testing"

	"chi-mongo-backend/internal/models"
)

func TestKeepTrackedPoolOnStaleRecreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pool *models.GPUPool
		want bool
	}{
		{
			name: "ready pool is preserved",
			pool: &models.GPUPool{State: models.GPUPoolStateReady},
			want: true,
		},
		{
			name: "provisioning pool is preserved",
			pool: &models.GPUPool{State: models.GPUPoolStateProvisioning},
			want: true,
		},
		{
			name: "tracked node id is preserved",
			pool: &models.GPUPool{State: models.GPUPoolStateIdle, NodeID: "123"},
			want: true,
		},
		{
			name: "tracked ip is preserved",
			pool: &models.GPUPool{State: models.GPUPoolStateIdle, PublicIP: "1.2.3.4"},
			want: true,
		},
		{
			name: "empty idle pool is not preserved",
			pool: &models.GPUPool{State: models.GPUPoolStateIdle},
			want: false,
		},
		{
			name: "nil pool is not preserved",
			pool: nil,
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := keepTrackedPoolOnStaleRecreate(tc.pool); got != tc.want {
				t.Fatalf("keepTrackedPoolOnStaleRecreate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPoolNeedsOrphanRecover(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pool *models.GPUPool
		want bool
	}{
		{name: "idle empty", pool: &models.GPUPool{State: models.GPUPoolStateIdle}, want: true},
		{name: "failed empty", pool: &models.GPUPool{State: models.GPUPoolStateFailed}, want: true},
		{name: "idle with node", pool: &models.GPUPool{State: models.GPUPoolStateIdle, NodeID: "i-1"}, want: false},
		{name: "idle with ip", pool: &models.GPUPool{State: models.GPUPoolStateIdle, PublicIP: "1.1.1.1"}, want: false},
		{name: "ready empty", pool: &models.GPUPool{State: models.GPUPoolStateReady}, want: false},
		{name: "idle with sessions", pool: &models.GPUPool{State: models.GPUPoolStateIdle, RefCount: 1}, want: false},
		{name: "nil", pool: nil, want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := poolNeedsOrphanRecover(tc.pool); got != tc.want {
				t.Fatalf("poolNeedsOrphanRecover() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecoverySSHHost(t *testing.T) {
	t.Setenv("GPU_SSH_HOST", "")
	t.Setenv("E2E_SSH_HOST", "")
	if got := recoverySSHHost("aws"); got != "vlm-aws" {
		t.Fatalf("aws host = %q, want vlm-aws", got)
	}
	if got := recoverySSHHost("e2e"); got != "e2e" {
		t.Fatalf("e2e host = %q, want e2e", got)
	}
	t.Setenv("GPU_SSH_HOST", "custom-gpu")
	if got := recoverySSHHost("aws"); got != "custom-gpu" {
		t.Fatalf("override host = %q, want custom-gpu", got)
	}
}

func TestFilterAdoptableNodes(t *testing.T) {
	t.Parallel()
	nodes := []e2eNodeSnapshot{
		{ID: "i-1", Status: "running", PublicIP: "1.1.1.1"},
		{ID: "i-2", Status: "stopped", PublicIP: "2.2.2.2"},
		{ID: "324", Status: "Running", PublicIP: "3.3.3.3"},
	}
	got := filterAdoptableNodes(nodes)
	if len(got) != 2 {
		t.Fatalf("adoptable count = %d, want 2", len(got))
	}
}

func TestParseListNodesAWSShape(t *testing.T) {
	t.Parallel()
	out := `AWS nodes (region=ap-south-1, pool=vlm-gpu, count=1)

  id=i-004a1891d1e2d2105  status=running       ip=65.0.127.72      name=automica-vlm-gpu-aws
`
	count, nodes := parseListNodesOutput(out)
	if count != 1 || len(nodes) != 1 {
		t.Fatalf("count=%d nodes=%d", count, len(nodes))
	}
	if nodes[0].ID != "i-004a1891d1e2d2105" || nodes[0].PublicIP != "65.0.127.72" {
		t.Fatalf("parsed %#v", nodes[0])
	}
}

func TestParseAdoptAWSShape(t *testing.T) {
	t.Parallel()
	out := "Adopted node into state: id=i-004a1891d1e2d2105 name=automica-vlm-gpu-aws ip=65.0.127.72 user=root\n"
	got := parseAdoptOutput(out)
	if got.ID != "i-004a1891d1e2d2105" || got.PublicIP != "65.0.127.72" {
		t.Fatalf("parsed %#v", got)
	}
}
