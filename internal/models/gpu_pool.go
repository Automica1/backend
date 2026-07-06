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
	UserID           string     `bson:"userId" json:"userId"`
	StartedAt        time.Time  `bson:"startedAt" json:"startedAt"`
	StoppedAt        *time.Time `bson:"stoppedAt,omitempty" json:"stoppedAt,omitempty"`
	CreditsMeterID   string     `bson:"creditsMeterId,omitempty" json:"creditsMeterId,omitempty"`
	BillingStartedAt *time.Time `bson:"billingStartedAt,omitempty" json:"billingStartedAt,omitempty"`
	CreditsCharged         int        `bson:"creditsCharged" json:"creditsCharged"`
	CreditsStartupCharged  int        `bson:"creditsStartupCharged,omitempty" json:"creditsStartupCharged,omitempty"`
	StartupRefunded        bool       `bson:"startupRefunded,omitempty" json:"startupRefunded,omitempty"`
	LastMeteredAt          *time.Time `bson:"lastMeteredAt,omitempty" json:"lastMeteredAt,omitempty"`
	EndReason              string     `bson:"endReason,omitempty" json:"endReason,omitempty"`
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
	GPUPoolDrainReasonUserGrace  GPUPoolDrainReason = "user_grace"
	GPUPoolDrainReasonAdminGrace GPUPoolDrainReason = "admin_grace"
)

func (p *GPUPool) HasScheduledGraceDestroy() bool {
	if p == nil {
		return false
	}
	return p.DrainReason == GPUPoolDrainReasonUserGrace || p.DrainReason == GPUPoolDrainReasonAdminGrace
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
	ReadyAt          *time.Time         `bson:"readyAt,omitempty" json:"readyAt,omitempty"`
	DrainStartedAt   *time.Time         `bson:"drainStartedAt,omitempty" json:"drainStartedAt,omitempty"`
	DrainReason      GPUPoolDrainReason `bson:"drainReason,omitempty" json:"drainReason,omitempty"`
	AdminWarmHold    bool               `bson:"adminWarmHold,omitempty" json:"adminWarmHold,omitempty"`
	Sessions         []GPUPoolSession   `bson:"sessions" json:"sessions"`
	LastError        string             `bson:"lastError,omitempty" json:"lastError,omitempty"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
}

type GPUPoolStartRequest struct {
	ServiceTag string `json:"serviceTag"`
}

type GPUPoolStopRequest struct {
	ServiceTag string `json:"serviceTag"`
}

type GPUPoolStatusResponse struct {
	ServiceTag           string       `json:"serviceTag"`
	ServiceName          string       `json:"serviceName"`
	State                GPUPoolState `json:"state"`
	RefCount             int          `json:"refCount"`
	PublicIP             string       `json:"publicIp,omitempty"`
	NodeID               string       `json:"nodeId,omitempty"`
	ReadyAt              *time.Time   `json:"readyAt,omitempty"`
	DrainStartedAt       *time.Time           `json:"drainStartedAt,omitempty"`
	DrainReason          GPUPoolDrainReason   `json:"drainReason,omitempty"`
	DestroyAt            *time.Time           `json:"destroyAt,omitempty"`
	GracePeriodSec       int                  `json:"gracePeriodSec,omitempty"`
	LastError            string               `json:"lastError,omitempty"`
	PollURL              string       `json:"pollUrl"`
	UserActive           bool         `json:"userActive"`
	CreditsChargedSession        int `json:"creditsChargedSession"`
	CreditsStartupChargedSession int `json:"creditsStartupChargedSession"`
	CreditsGpuTimeSession        int `json:"creditsGpuTimeSession"`
	CreditsPerMinute             int `json:"creditsPerMinute"`
	StartupCredits        int         `json:"startupCredits"`
	MinCreditsToStart     int         `json:"minCreditsToStart"`
	MeterIntervalSec      int         `json:"meterIntervalSec"`
	NextMeterChargeAt     *time.Time  `json:"nextMeterChargeAt,omitempty"`
	BillingActive         bool        `json:"billingActive"`
	// ReattachedSession is true when the user already had an active session (page refresh,
	// status poll, or idempotent Start). False on Start immediately after creating a new session.
	ReattachedSession     bool        `json:"reattachedSession"`
	SessionEndReason      string      `json:"sessionEndReason,omitempty"`
	ReconnectEligible     bool        `json:"reconnectEligible"`
	ReconnectUntil        *time.Time  `json:"reconnectUntil,omitempty"`
}

// ServiceTagToPipelineService maps beta gateway tags to pipeline service names.
var ServiceTagToPipelineService = map[string]string{
	"vlm-e2e-gpu": "sign_verify_vlm_gpu",
}

// BetaServiceTagRequiresGPUPool is true when Try API must run gpu-pool start/stop for this beta tag.
func BetaServiceTagRequiresGPUPool(tag string) bool {
	tag = normalizeBetaServiceTag(tag)
	_, ok := ServiceTagToPipelineService[tag]
	return ok
}
