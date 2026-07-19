package handlers

import "testing"

func TestShouldReuseProvisionNode(t *testing.T) {
	tests := []struct {
		name     string
		state    *provisionState
		nodeLive bool
		want     bool
	}{
		{
			name:     "running state survives list miss",
			state:    &provisionState{PublicIP: "164.52.195.35", Status: "Running"},
			nodeLive: false,
			want:     true,
		},
		{
			name:     "live node survives transient state",
			state:    &provisionState{PublicIP: "164.52.195.35", Status: "Creating"},
			nodeLive: true,
			want:     true,
		},
		{
			name:     "failed state without live node is not reused",
			state:    &provisionState{PublicIP: "164.52.195.35", Status: "Failed"},
			nodeLive: false,
			want:     false,
		},
		{
			name:     "missing state is not reused",
			state:    nil,
			nodeLive: false,
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReuseProvisionNode(tc.state, tc.nodeLive); got != tc.want {
				t.Fatalf("shouldReuseProvisionNode() = %v, want %v", got, tc.want)
			}
		})
	}
}
