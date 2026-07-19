package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type GPUPoolState string

const (
	GPUPoolStateIdle         GPUPoolState = "idle"
	GPUPoolStateProvisioning GPUPoolState = "provisioning"
	GPUPoolStateReady        GPUPoolState = "ready"
	GPUPoolStateDraining     GPUPoolState = "draining"
	GPUPoolStateFailed       GPUPoolState = "failed"
)

// GPUPoolSession tracks one user's testing window on a shared GPU pool.
// Stop Testing ends the session; user-owned nodes get grace teardown when refCount hits 0.
type GPUPoolSession struct {
	UserID                string     `bson:"userId" json:"userId"`
	StartedAt             time.Time  `bson:"startedAt" json:"startedAt"`
	StoppedAt             *time.Time `bson:"stoppedAt,omitempty" json:"stoppedAt,omitempty"`
	CreditsMeterID        string     `bson:"creditsMeterId,omitempty" json:"creditsMeterId,omitempty"`
	BillingStartedAt      *time.Time `bson:"billingStartedAt,omitempty" json:"billingStartedAt,omitempty"`
	CreditsCharged        int        `bson:"creditsCharged" json:"creditsCharged"`
	CreditsStartupCharged int        `bson:"creditsStartupCharged,omitempty" json:"creditsStartupCharged,omitempty"`
	StartupRefunded       bool       `bson:"startupRefunded,omitempty" json:"startupRefunded,omitempty"`
	LastMeteredAt         *time.Time `bson:"lastMeteredAt,omitempty" json:"lastMeteredAt,omitempty"`
	EndReason             string     `bson:"endReason,omitempty" json:"endReason,omitempty"`
}

const GPUPoolSessionEndInsufficientCredits = "insufficient_credits"
const GPUPoolSessionEndProvisionFailed = "provision_failed"
const GPUPoolSessionEndPoolNotLive = "pool_not_live"

// GPUPoolNodeOwner records who provisioned the warm node (controls teardown on last Stop).
type GPUPoolNodeOwner string

const (
	// GPUPoolNodeOwnerUser: Try API Start Testing cold-provisioned the VM.
	GPUPoolNodeOwnerUser GPUPoolNodeOwner = "user"
	// GPUPoolNodeOwnerAdmin: Mac bootstrap or support/admin warm deploy (attribution only).
	GPUPoolNodeOwnerAdmin GPUPoolNodeOwner = "admin"
)

// TeardownOnUserStop is true when a live node exists and the last session ended (refCount=0).
// All warm nodes enter grace_destroy — zero GPU cost when idle.
func (p *GPUPool) TeardownOnUserStop() bool {
	if p == nil {
		return false
	}
	return p.NodeID != "" || p.PublicIP != ""
}

// GPUPoolDrainReason distinguishes scheduled grace teardown paths.
type GPUPoolDrainReason string

const (
	GPUPoolDrainReasonUserGrace       GPUPoolDrainReason = "user_grace"
	GPUPoolDrainReasonAdminGrace      GPUPoolDrainReason = "admin_grace"
	GPUPoolDrainReasonFailedBootstrap GPUPoolDrainReason = "failed_bootstrap"
)

func (p *GPUPool) HasScheduledGraceDestroy() bool {
	if p == nil {
		return false
	}
	return p.DrainReason == GPUPoolDrainReasonUserGrace ||
		p.DrainReason == GPUPoolDrainReasonAdminGrace ||
		p.DrainReason == GPUPoolDrainReasonFailedBootstrap
}

type GPUPool struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ServiceTag       string             `bson:"serviceTag" json:"serviceTag"`
	ServiceName      string             `bson:"serviceName" json:"serviceName"`
	State            GPUPoolState       `bson:"state" json:"state"`
	RefCount         int                `bson:"refCount" json:"refCount"`
	NodeOwner        GPUPoolNodeOwner   `bson:"nodeOwner,omitempty" json:"nodeOwner,omitempty"`
	NodeID           string             `bson:"nodeId,omitempty" json:"nodeId,omitempty"`
	PublicIP         string             `bson:"publicIp,omitempty" json:"publicIp,omitempty"`
	PreviousPublicIP string             `bson:"previousPublicIp,omitempty" json:"previousPublicIp,omitempty"`
	Provider         string             `bson:"provider,omitempty" json:"provider,omitempty"`
	InstanceType     string             `bson:"instanceType,omitempty" json:"instanceType,omitempty"`
	CapacityType     string             `bson:"capacityType,omitempty" json:"capacityType,omitempty"`
	Region           string             `bson:"region,omitempty" json:"region,omitempty"`
	DeployVersion    string             `bson:"deployVersion,omitempty" json:"deployVersion,omitempty"`
	ReadyAt          *time.Time         `bson:"readyAt,omitempty" json:"readyAt,omitempty"`
	DrainStartedAt   *time.Time         `bson:"drainStartedAt,omitempty" json:"drainStartedAt,omitempty"`
	DestroyAt        *time.Time         `bson:"destroyAt,omitempty" json:"destroyAt,omitempty"`
	DrainReason      GPUPoolDrainReason `bson:"drainReason,omitempty" json:"drainReason,omitempty"`
	AdminWarmHold    bool               `bson:"adminWarmHold,omitempty" json:"adminWarmHold,omitempty"`
	Sessions         []GPUPoolSession   `bson:"sessions" json:"sessions"`
	LastError        string             `bson:"lastError,omitempty" json:"lastError,omitempty"`
	LastErrorRaw     string             `bson:"lastErrorRaw,omitempty" json:"lastErrorRaw,omitempty"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
}

// GPUPoolAdminEntry extends pool rows for the admin control plane list.
type GPUPoolAdminEntry struct {
	GPUPool
	GracePeriodSec int    `json:"gracePeriodSec"`
	PolicySummary  string `json:"policySummary,omitempty"`
	ActiveJob      *Job   `json:"activeJob,omitempty"`
}

// GPUPoolBillingInfo is read-only product billing shown in admin UI.
type GPUPoolBillingInfo struct {
	StartupCredits int  `json:"startupCredits"`
	CreditsPerMin  int  `json:"creditsPerMin"`
	ReadOnly       bool `json:"readOnly"`
}

// GPUPoolSupportSessionView is a sanitized session row for support.
type GPUPoolSupportSessionView struct {
	UserID                string     `json:"userId"`
	StartedAt             time.Time  `json:"startedAt"`
	StoppedAt             *time.Time `json:"stoppedAt,omitempty"`
	CreditsCharged        int        `json:"creditsCharged"`
	CreditsStartupCharged int        `json:"creditsStartupCharged,omitempty"`
	Active                bool       `json:"active"`
}

// GPUPoolSupportView is a sanitized pool status for testers/support.
type GPUPoolSupportView struct {
	ServiceTag       string                      `json:"serviceTag"`
	State            GPUPoolState                `json:"state"`
	Provider         string                      `json:"provider,omitempty"`
	ProviderLabel    string                      `json:"providerLabel,omitempty"`
	PublicIP         string                      `json:"publicIp,omitempty"`
	RefCount         int                         `json:"refCount"`
	CanStart         bool                        `json:"canStart"`
	UserFacingError  string                      `json:"userFacingError,omitempty"`
	LastErrorRaw     string                      `json:"lastErrorRaw,omitempty"`
	MaintenanceBlock bool                        `json:"maintenanceBlock"`
	Sessions         []GPUPoolSupportSessionView `json:"sessions,omitempty"`
}

type GPUPoolRecoveryNode struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Status   string `json:"status,omitempty"`
	PublicIP string `json:"publicIp,omitempty"`
}

type GPUPoolRecoveryReport struct {
	ServiceTag        string                `json:"serviceTag"`
	State             GPUPoolState          `json:"state"`
	Provider          string                `json:"provider,omitempty"`
	ProviderLabel     string                `json:"providerLabel,omitempty"`
	SSHReachable      bool                  `json:"sshReachable"`
	ProviderNodeCount int                   `json:"providerNodeCount"`
	ProviderNodes     []GPUPoolRecoveryNode `json:"providerNodes,omitempty"`
	Action            string                `json:"action"`
	Recovered         bool                  `json:"recovered"`
	Message           string                `json:"message,omitempty"`
	Notes             []string              `json:"notes,omitempty"`
	ProbedAt          string                `json:"probedAt"`
	Pool              *GPUPool              `json:"pool,omitempty"`
}

// GPUPoolInventory is live provider inventory (AWS/E2E list-nodes), not Mongo.
type GPUPoolInventory struct {
	ServiceTag        string                `json:"serviceTag"`
	Provider          string                `json:"provider"`
	ProviderLabel     string                `json:"providerLabel"`
	ProviderNodeCount int                   `json:"providerNodeCount"`
	ProviderNodes     []GPUPoolRecoveryNode `json:"providerNodes"`
	SSHHost           string                `json:"sshHost,omitempty"`
	SSHReachable      bool                  `json:"sshReachable"`
	MongoState        GPUPoolState          `json:"mongoState,omitempty"`
	MongoNodeID       string                `json:"mongoNodeId,omitempty"`
	MongoPublicIP     string                `json:"mongoPublicIp,omitempty"`
	Source            string                `json:"source"` // "provider-list-nodes"
	RawPreview        string                `json:"rawPreview,omitempty"`
	Notes             []string              `json:"notes,omitempty"`
	ProbedAt          string                `json:"probedAt"`
}

type GPUPoolStartRequest struct {
	ServiceTag string `json:"serviceTag"`
}

type GPUPoolStopRequest struct {
	ServiceTag string `json:"serviceTag"`
}

type GPUPoolStatusResponse struct {
	ServiceTag                   string             `json:"serviceTag"`
	ServiceName                  string             `json:"serviceName"`
	State                        GPUPoolState       `json:"state"`
	RefCount                     int                `json:"refCount"`
	PublicIP                     string             `json:"publicIp,omitempty"`
	NodeID                       string             `json:"nodeId,omitempty"`
	ReadyAt                      *time.Time         `json:"readyAt,omitempty"`
	DrainStartedAt               *time.Time         `json:"drainStartedAt,omitempty"`
	DrainReason                  GPUPoolDrainReason `json:"drainReason,omitempty"`
	DestroyAt                    *time.Time         `json:"destroyAt,omitempty"`
	GracePeriodSec               int                `json:"gracePeriodSec,omitempty"`
	LastError                    string             `json:"lastError,omitempty"`
	PollURL                      string             `json:"pollUrl"`
	UserActive                   bool               `json:"userActive"`
	CreditsChargedSession        int                `json:"creditsChargedSession"`
	CreditsStartupChargedSession int                `json:"creditsStartupChargedSession"`
	CreditsGpuTimeSession        int                `json:"creditsGpuTimeSession"`
	CreditsPerMinute             int                `json:"creditsPerMinute"`
	StartupCredits               int                `json:"startupCredits"`
	MinCreditsToStart            int                `json:"minCreditsToStart"`
	MeterIntervalSec             int                `json:"meterIntervalSec"`
	NextMeterChargeAt            *time.Time         `json:"nextMeterChargeAt,omitempty"`
	BillingActive                bool               `json:"billingActive"`
	// ReattachedSession is true when the user already had an active session (page refresh,
	// status poll, or idempotent Start). False on Start immediately after creating a new session.
	ReattachedSession bool       `json:"reattachedSession"`
	SessionEndReason  string     `json:"sessionEndReason,omitempty"`
	ReconnectEligible bool       `json:"reconnectEligible"`
	ReconnectUntil    *time.Time `json:"reconnectUntil,omitempty"`
}

// Canonical GPU / beta tags for signature-verification.
// vlm-gpu is infra-agnostic (E2E or AWS, GHCR or ECR). vlm-e2e-gpu is a legacy alias
// kept so an already-warm E2E pool and old beta keys keep working during cutover.
const (
	VLMGPUServiceTag       = "vlm-gpu"
	VLMGPUServiceTagLegacy = "vlm-e2e-gpu"
)

// ServiceTagToPipelineService maps beta gateway tags to pipeline service names.
var ServiceTagToPipelineService = map[string]string{
	VLMGPUServiceTag:       "sign_verify_vlm_gpu",
	VLMGPUServiceTagLegacy: "sign_verify_vlm_gpu",
	OCRGPUServiceTag:       "ocr",
}

// CanonicalGPUServiceTag maps legacy GPU tags onto the current pool identity.
// Pool documents and provision configs should be keyed by the canonical tag when
// creating new pools; legacy lookups still resolve for Start Testing.
func CanonicalGPUServiceTag(tag string) string {
	tag = normalizeBetaServiceTag(tag)
	if tag == VLMGPUServiceTagLegacy {
		return VLMGPUServiceTag
	}
	return tag
}

// BetaServiceTagRequiresGPUPool is true when Try API must run gpu-pool start/stop for this beta tag.
func BetaServiceTagRequiresGPUPool(tag string) bool {
	tag = normalizeBetaServiceTag(tag)
	_, ok := ServiceTagToPipelineService[tag]
	return ok
}
