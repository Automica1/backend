package models

import (
	"os"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type GPUProviderName string

const (
	GPUProviderE2E GPUProviderName = "e2e"
	GPUProviderAWS GPUProviderName = "aws"
	GPUProviderGCP GPUProviderName = "gcp"
)

type GPUE2EProviderConfig struct {
	Location string `bson:"location,omitempty" json:"location"`
	GPUCard  string `bson:"gpuCard,omitempty" json:"gpuCard"`
	Plan     string `bson:"plan,omitempty" json:"plan"`
}

type GPUAWSProviderConfig struct {
	Region           string `bson:"region,omitempty" json:"region"`
	InstanceType     string `bson:"instanceType,omitempty" json:"instanceType"`
	CapacityType     string `bson:"capacityType,omitempty" json:"capacityType"`
	CapacityFallback string `bson:"capacityFallback,omitempty" json:"capacityFallback"`
	FallbackCapacity string `bson:"fallbackCapacity,omitempty" json:"fallbackCapacity"`
	SubnetID         string `bson:"subnetId,omitempty" json:"subnetId"`
	SecurityGroupID  string `bson:"securityGroupId,omitempty" json:"securityGroupId"`
}

type GPUGCPProviderConfig struct {
	Enabled     bool   `bson:"enabled,omitempty" json:"enabled"`
	Region      string `bson:"region,omitempty" json:"region"`
	MachineType string `bson:"machineType,omitempty" json:"machineType"`
}

type GPUProvisionInfrastructure struct {
	PrimaryProvider  GPUProviderName        `bson:"primaryProvider" json:"primaryProvider"`
	FallbackProvider GPUProviderName        `bson:"fallbackProvider,omitempty" json:"fallbackProvider"`
	E2E              GPUE2EProviderConfig   `bson:"e2e,omitempty" json:"e2e"`
	AWS              GPUAWSProviderConfig   `bson:"aws,omitempty" json:"aws"`
	GCP              GPUGCPProviderConfig   `bson:"gcp,omitempty" json:"gcp"`
}

type GPUProvisionTimeouts struct {
	SSHReadyPrimarySec int `bson:"sshReadyPrimarySec,omitempty" json:"sshReadyPrimarySec"`
	SSHReadyFallbackSec int `bson:"sshReadyFallbackSec,omitempty" json:"sshReadyFallbackSec"`
	E2EWaitSec         int `bson:"e2eWaitSec,omitempty" json:"e2eWaitSec"`
	E2EStallSec        int `bson:"e2eStallSec,omitempty" json:"e2eStallSec"`
	E2EDestroyWaitSec  int `bson:"e2eDestroyWaitSec,omitempty" json:"e2eDestroyWaitSec"`
	DeployHealthSec    int `bson:"deployHealthSec,omitempty" json:"deployHealthSec"`
}

type GPUProvisionRetries struct {
	ProvisionMaxAttempts int  `bson:"provisionMaxAttempts,omitempty" json:"provisionMaxAttempts"`
	DestroyMaxAttempts   int  `bson:"destroyMaxAttempts,omitempty" json:"destroyMaxAttempts"`
	ReuseNodeOnRetry     bool `bson:"reuseNodeOnRetry,omitempty" json:"reuseNodeOnRetry"`
}

type GPUProvisionLifecycle struct {
	GracePeriodMin          int `bson:"gracePeriodMin,omitempty" json:"gracePeriodMin"`
	ReconnectCooldownSec    int `bson:"reconnectCooldownSec,omitempty" json:"reconnectCooldownSec"`
	StuckProvisionNoJobMin  int `bson:"stuckProvisionNoJobMin,omitempty" json:"stuckProvisionNoJobMin"`
	StuckProvisionZombieMin int `bson:"stuckProvisionZombieMin,omitempty" json:"stuckProvisionZombieMin"`
	UserRetryHintMin        int `bson:"userRetryHintMin,omitempty" json:"userRetryHintMin"`
}

type GPUProvisionFlags struct {
	MaintenanceMode    bool   `bson:"maintenanceMode,omitempty" json:"maintenanceMode"`
	BlockNewSessions   bool   `bson:"blockNewSessions,omitempty" json:"blockNewSessions"`
	MaintenanceMessage string `bson:"maintenanceMessage,omitempty" json:"maintenanceMessage"`
}

type GPUProvisionConfig struct {
	ID             primitive.ObjectID         `bson:"_id,omitempty" json:"id"`
	ServiceTag     string                     `bson:"serviceTag" json:"serviceTag"`
	ServiceName    string                     `bson:"serviceName,omitempty" json:"serviceName"`
	Infrastructure GPUProvisionInfrastructure `bson:"infrastructure" json:"infrastructure"`
	Timeouts       GPUProvisionTimeouts       `bson:"timeouts" json:"timeouts"`
	Retries        GPUProvisionRetries        `bson:"retries" json:"retries"`
	Lifecycle      GPUProvisionLifecycle      `bson:"lifecycle" json:"lifecycle"`
	Flags          GPUProvisionFlags          `bson:"flags" json:"flags"`
	UpdatedAt      time.Time                  `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy      string                     `bson:"updatedBy,omitempty" json:"updatedBy"`
	CreatedAt      time.Time                  `bson:"createdAt" json:"createdAt"`

	// Legacy flat fields (read compat from older Mongo docs).
	PrimaryProvider            GPUProviderName      `bson:"primaryProvider,omitempty" json:"-"`
	FallbackProvider           GPUProviderName      `bson:"fallbackProvider,omitempty" json:"-"`
	SSHReadyTimeoutSec         int                  `bson:"sshReadyTimeoutSec,omitempty" json:"-"`
	FallbackSSHReadyTimeoutSec int                  `bson:"fallbackSshReadyTimeoutSec,omitempty" json:"-"`
	E2E                        GPUE2EProviderConfig `bson:"e2e,omitempty" json:"-"`
	AWS                        GPUAWSProviderConfig `bson:"aws,omitempty" json:"-"`
	GCP                        GPUGCPProviderConfig `bson:"gcp,omitempty" json:"-"`
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func DefaultGPUProvisionConfig(serviceTag, serviceName string) *GPUProvisionConfig {
	now := time.Now().UTC()
	cfg := &GPUProvisionConfig{
		ServiceTag:  serviceTag,
		ServiceName: serviceName,
		Infrastructure: GPUProvisionInfrastructure{
			PrimaryProvider:  GPUProviderE2E,
			FallbackProvider: GPUProviderAWS,
			E2E: GPUE2EProviderConfig{
				Location: "Delhi",
				GPUCard:  "L4",
			},
			AWS: GPUAWSProviderConfig{
				Region:           "ap-south-1",
				InstanceType:     "g6.xlarge",
				CapacityType:     "spot",
				CapacityFallback: "on-demand",
				FallbackCapacity: "on-demand",
				SubnetID:         "subnet-0de6cd09d0922036c",
				SecurityGroupID:  "sg-07937c67021f78db5",
			},
			GCP: GPUGCPProviderConfig{
				Enabled:     false,
				Region:      "asia-south1",
				MachineType: "g2-standard-4",
			},
		},
		Timeouts: GPUProvisionTimeouts{
			SSHReadyPrimarySec:  600,
			SSHReadyFallbackSec: 300,
			E2EWaitSec:         600,
			E2EStallSec:        360,
			E2EDestroyWaitSec:  180,
			DeployHealthSec:    120,
		},
		Retries: GPUProvisionRetries{
			ProvisionMaxAttempts: 3,
			DestroyMaxAttempts:   3,
			ReuseNodeOnRetry:     true,
		},
		Lifecycle: GPUProvisionLifecycle{
			GracePeriodMin:          envInt("GPU_POOL_GRACE_MIN", 5),
			ReconnectCooldownSec:    envInt("GPU_POOL_RECONNECT_COOLDOWN_SEC", 300),
			StuckProvisionNoJobMin:  13,
			StuckProvisionZombieMin: 22,
			UserRetryHintMin:        15,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	cfg.Normalize()
	return cfg
}

// Normalize merges legacy flat BSON into nested policy fields and fills defaults.
func (c *GPUProvisionConfig) Normalize() {
	if c == nil {
		return
	}
	if c.Infrastructure.PrimaryProvider == "" && c.PrimaryProvider != "" {
		c.Infrastructure.PrimaryProvider = c.PrimaryProvider
	}
	if c.Infrastructure.FallbackProvider == "" && c.FallbackProvider != "" {
		c.Infrastructure.FallbackProvider = c.FallbackProvider
	}
	if c.Infrastructure.E2E.Location == "" && c.E2E.Location != "" {
		c.Infrastructure.E2E = c.E2E
	}
	if c.Infrastructure.AWS.Region == "" && c.AWS.Region != "" {
		c.Infrastructure.AWS = c.AWS
	}
	if c.Infrastructure.GCP.Region == "" && c.GCP.Region != "" {
		c.Infrastructure.GCP = c.GCP
	}
	if c.Timeouts.SSHReadyPrimarySec == 0 && c.SSHReadyTimeoutSec > 0 {
		c.Timeouts.SSHReadyPrimarySec = c.SSHReadyTimeoutSec
	}
	if c.Timeouts.SSHReadyFallbackSec == 0 && c.FallbackSSHReadyTimeoutSec > 0 {
		c.Timeouts.SSHReadyFallbackSec = c.FallbackSSHReadyTimeoutSec
	}
	if c.Infrastructure.PrimaryProvider == "" {
		c.Infrastructure.PrimaryProvider = GPUProviderE2E
	}
	if c.Infrastructure.FallbackProvider == "" {
		c.Infrastructure.FallbackProvider = GPUProviderAWS
	}
	if c.Timeouts.SSHReadyPrimarySec == 0 {
		c.Timeouts.SSHReadyPrimarySec = 600
	}
	if c.Timeouts.SSHReadyFallbackSec == 0 {
		c.Timeouts.SSHReadyFallbackSec = 300
	}
	if c.Timeouts.E2EWaitSec == 0 {
		c.Timeouts.E2EWaitSec = 600
	}
	if c.Timeouts.E2EStallSec == 0 {
		c.Timeouts.E2EStallSec = 360
	}
	if c.Timeouts.E2EDestroyWaitSec == 0 {
		c.Timeouts.E2EDestroyWaitSec = 180
	}
	if c.Timeouts.DeployHealthSec == 0 {
		c.Timeouts.DeployHealthSec = 120
	}
	if c.Retries.ProvisionMaxAttempts == 0 {
		c.Retries.ProvisionMaxAttempts = 3
	}
	if c.Retries.DestroyMaxAttempts == 0 {
		c.Retries.DestroyMaxAttempts = 3
	}
	if c.Lifecycle.GracePeriodMin == 0 {
		c.Lifecycle.GracePeriodMin = envInt("GPU_POOL_GRACE_MIN", 5)
	}
	if c.Lifecycle.ReconnectCooldownSec == 0 {
		c.Lifecycle.ReconnectCooldownSec = envInt("GPU_POOL_RECONNECT_COOLDOWN_SEC", 300)
	}
	if c.Lifecycle.StuckProvisionNoJobMin == 0 {
		c.Lifecycle.StuckProvisionNoJobMin = 13
	}
	if c.Lifecycle.StuckProvisionZombieMin == 0 {
		c.Lifecycle.StuckProvisionZombieMin = 22
	}
	if c.Lifecycle.UserRetryHintMin == 0 {
		c.Lifecycle.UserRetryHintMin = 15
	}
}

func (c *GPUProvisionConfig) BlocksNewSessions() bool {
	if c == nil {
		return false
	}
	return c.Flags.BlockNewSessions || c.Flags.MaintenanceMode
}

func (c *GPUProvisionConfig) MaintenanceUserMessage() string {
	if c == nil {
		return "GPU testing is temporarily unavailable. Please try again later."
	}
	if msg := c.Flags.MaintenanceMessage; msg != "" {
		return msg
	}
	return "GPU testing is temporarily unavailable. Please try again later."
}

func (c *GPUProvisionConfig) usesProvider(name GPUProviderName) bool {
	if c == nil {
		return false
	}
	c.Normalize()
	inf := c.Infrastructure
	if inf.PrimaryProvider == name {
		return true
	}
	return inf.FallbackProvider == name
}

// UsesE2ENetworks is true when E2E Networks is primary or fallback in the provision chain.
func (c *GPUProvisionConfig) UsesE2ENetworks() bool {
	return c.usesProvider(GPUProviderE2E)
}

// UsesAWS is true when AWS EC2 is primary or fallback in the provision chain.
func (c *GPUProvisionConfig) UsesAWS() bool {
	return c.usesProvider(GPUProviderAWS)
}

// MinStuckProvisionNoJobMin is the minimum stuck-provision threshold derived from active provider timeouts.
func (c *GPUProvisionConfig) MinStuckProvisionNoJobMin() int {
	if c == nil {
		return 13
	}
	c.Normalize()
	t := c.Timeouts
	minSec := t.SSHReadyPrimarySec
	if c.UsesE2ENetworks() && t.E2EWaitSec > minSec {
		minSec = t.E2EWaitSec
	}
	if minSec <= 0 {
		minSec = 600
	}
	return minSec/60 + 2
}
