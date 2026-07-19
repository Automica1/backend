package models

import "time"

const AISaaSSchemaVersion = "ai-saas-full-1"

type AISaaSListResponse struct {
	SchemaVersion string                `json:"schemaVersion"`
	GeneratedAt   time.Time             `json:"generatedAt"`
	DateRange     AISaaSDateRange       `json:"dateRange"`
	Sources       []AISaaSSourceStatus  `json:"sources"`
	Warnings      []AISaaSWarning       `json:"warnings,omitempty"`
	Fleet         AISaaSFleetSummary    `json:"fleet"`
	Services      []AISaaSServiceRecord `json:"services"`
}

type AISaaSDateRange struct {
	StartDate *time.Time `json:"startDate,omitempty"`
	EndDate   *time.Time `json:"endDate,omitempty"`
}

type AISaaSSourceStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Count   int    `json:"count"`
	Warning string `json:"warning,omitempty"`
}

type AISaaSWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
	APIID    string `json:"apiId,omitempty"`
}

type AISaaSFleetSummary struct {
	TotalServices      int `json:"totalServices"`
	PublicServices     int `json:"publicServices"`
	BetaServices       int `json:"betaServices"`
	GPUBackedServices  int `json:"gpuBackedServices"`
	ReadyRuntimes      int `json:"readyRuntimes"`
	Provisioning       int `json:"provisioning"`
	FailedRuntimes     int `json:"failedRuntimes"`
	ActiveSessions     int `json:"activeSessions"`
	ActiveBetaKeys     int `json:"activeBetaKeys"`
	TotalCalls         int `json:"totalCalls"`
	TotalCredits       int `json:"totalCredits"`
	PartialSourceCount int `json:"partialSourceCount"`
}

type AISaaSAliases struct {
	CatalogSlugs         []string `json:"catalogSlugs"`
	UsageNames           []string `json:"usageNames"`
	BetaServiceNames     []string `json:"betaServiceNames"`
	BetaServiceTags      []string `json:"betaServiceTags"`
	GPUServiceTags       []string `json:"gpuServiceTags"`
	PipelineServices     []string `json:"pipelineServices"`
	FeedbackServiceNames []string `json:"feedbackServiceNames"`
}

type AISaaSServiceRecord struct {
	APIID          string                 `json:"apiId"`
	Slug           string                 `json:"slug"`
	DisplayName    string                 `json:"displayName"`
	Lifecycle      string                 `json:"lifecycle"`
	Readiness      string                 `json:"readiness"`
	NextSafeAction string                 `json:"nextSafeAction"`
	Public         AISaaSPublicSummary    `json:"public"`
	Aliases        AISaaSAliases          `json:"aliases"`
	Sources        []AISaaSSourceStatus   `json:"sources"`
	Warnings       []AISaaSWarning        `json:"warnings,omitempty"`
	Policy         AISaaSPolicySummary    `json:"policy"`
	Access         AISaaSAccessSummary    `json:"access"`
	Usage          AISaaSUsageSummary     `json:"usage"`
	Feedback       AISaaSFeedbackSummary  `json:"feedback"`
	Runtime        []AISaaSRuntimeProfile `json:"runtime"`
	Links          []AISaaSLink           `json:"links"`
}

type AISaaSPublicSummary struct {
	Status       string `json:"status"`
	Endpoint     string `json:"endpoint,omitempty"`
	DocsPath     string `json:"docsPath,omitempty"`
	TryAPIPath   string `json:"tryApiPath,omitempty"`
	Source       string `json:"source,omitempty"`
	Configurable bool   `json:"configurable"`
	BlockedBy    string `json:"blockedBy,omitempty"`
}

type AISaaSPolicySummary struct {
	Source           string   `json:"source,omitempty"`
	HasPolicy        bool     `json:"hasPolicy"`
	Summary          string   `json:"summary,omitempty"`
	MaxUploadSizeMB  *int     `json:"maxUploadSizeMB,omitempty"`
	MaxPages         *int     `json:"maxPages,omitempty"`
	MaxFiles         *int     `json:"maxFiles,omitempty"`
	AllowedFormats   []string `json:"allowedFormats,omitempty"`
	PricingMode      string   `json:"pricingMode,omitempty"`
	CreditsPerHit    *int     `json:"creditsPerHit,omitempty"`
	CreditsPerPage   *int     `json:"creditsPerPage,omitempty"`
	StartupCredits   *int     `json:"startupCredits,omitempty"`
	CreditsPerMinute *int     `json:"creditsPerMinute,omitempty"`
	Notes            string   `json:"notes,omitempty"`
	Configurable     bool     `json:"configurable"`
	BlockedBy        string   `json:"blockedBy,omitempty"`
}

type AISaaSAccessSummary struct {
	Model           string   `json:"model"`
	BetaSupported   bool     `json:"betaSupported"`
	TotalBetaKeys   int      `json:"totalBetaKeys"`
	ActiveBetaKeys  int      `json:"activeBetaKeys"`
	RevokedBetaKeys int      `json:"revokedBetaKeys"`
	ExpiredBetaKeys int      `json:"expiredBetaKeys"`
	BetaServiceTags []string `json:"betaServiceTags,omitempty"`
	Configurable    bool     `json:"configurable"`
	BlockedBy       string   `json:"blockedBy,omitempty"`
}

type AISaaSUsageSummary struct {
	Source       string `json:"source,omitempty"`
	TotalCalls   int    `json:"totalCalls"`
	SuccessCalls int    `json:"successCalls"`
	FailedCalls  int    `json:"failedCalls"`
	TotalCredits int    `json:"totalCredits"`
}

type AISaaSFeedbackSummary struct {
	Source          string     `json:"source,omitempty"`
	RecentSessions  int        `json:"recentSessions"`
	PendingSessions int        `json:"pendingSessions"`
	RefundedCredits int        `json:"refundedCredits"`
	LastCreatedAt   *time.Time `json:"lastCreatedAt,omitempty"`
}

type AISaaSRuntimeProfile struct {
	RuntimeID      string                  `json:"runtimeId"`
	ServiceTag     string                  `json:"serviceTag,omitempty"`
	ServiceName    string                  `json:"serviceName,omitempty"`
	RouteAliases   []string                `json:"routeAliases,omitempty"` // legacy gateway tags sharing this policy
	State          string                  `json:"state"`
	Readiness      string                  `json:"readiness"`
	Provider       string                  `json:"provider,omitempty"`
	Region         string                  `json:"region,omitempty"`
	InstanceType   string                  `json:"instanceType,omitempty"`
	CapacityType   string                  `json:"capacityType,omitempty"`
	DeployVersion  string                  `json:"deployVersion,omitempty"`
	RefCount       int                     `json:"refCount"`
	ActiveSessions int                     `json:"activeSessions"`
	NodeOwner      string                  `json:"nodeOwner,omitempty"`
	NodeID         string                  `json:"nodeId,omitempty"`
	PublicIP       string                  `json:"publicIp,omitempty"`
	ReadyAt        *time.Time              `json:"readyAt,omitempty"`
	DestroyAt      *time.Time              `json:"destroyAt,omitempty"`
	UpdatedAt      *time.Time              `json:"updatedAt,omitempty"`
	LastError      string                  `json:"lastError,omitempty"`
	Registry       *AISaaSRegistrySummary  `json:"registry,omitempty"`
	Provision      *AISaaSProvisionSummary `json:"provision,omitempty"`
	Jobs           []AISaaSJobSummary      `json:"jobs,omitempty"`
}

type AISaaSRegistrySummary struct {
	Source             string `json:"source"`
	Provider           string `json:"provider,omitempty"`
	AuthMode           string `json:"authMode,omitempty"`
	Server             string `json:"server,omitempty"`
	Namespace          string `json:"namespace,omitempty"`
	Region             string `json:"region,omitempty"`
	ImageTag           string `json:"imageTag,omitempty"`
	PreferRegistryPull *bool  `json:"preferRegistryPull,omitempty"`
	LoginRequired      *bool  `json:"loginRequired,omitempty"`
}

type AISaaSProvisionSummary struct {
	Source               string `json:"source"`
	PrimaryProvider      string `json:"primaryProvider,omitempty"`
	FallbackProvider     string `json:"fallbackProvider,omitempty"`
	AWSRegion            string `json:"awsRegion,omitempty"`
	AWSInstanceType      string `json:"awsInstanceType,omitempty"`
	AWSCapacityType      string `json:"awsCapacityType,omitempty"`
	E2ELocation          string `json:"e2eLocation,omitempty"`
	E2EGPUCard           string `json:"e2eGpuCard,omitempty"`
	GCPRegion            string `json:"gcpRegion,omitempty"`
	GCPMachineType       string `json:"gcpMachineType,omitempty"`
	MaintenanceMode      bool   `json:"maintenanceMode"`
	BlockNewSessions     bool   `json:"blockNewSessions"`
	MaintenanceMessage   string `json:"maintenanceMessage,omitempty"`
	GracePeriodMin       int    `json:"gracePeriodMin,omitempty"`
	DeployHealthSec      int    `json:"deployHealthSec,omitempty"`
	ProvisionMaxAttempts int    `json:"provisionMaxAttempts,omitempty"`
}

type AISaaSJobSummary struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"maxAttempts"`
	RunAfter       time.Time  `json:"runAfter"`
	CreatedAt      time.Time  `json:"createdAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
	IdempotencyKey string     `json:"idempotencyKey,omitempty"`
}

type AISaaSLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
	Kind  string `json:"kind"`
}
