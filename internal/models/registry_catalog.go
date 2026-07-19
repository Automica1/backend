package models

import "time"

// RegistryImageTag is one mutable tag (or digest-less push) in a container registry.
type RegistryImageTag struct {
	Name      string     `json:"name"`
	PushedAt  *time.Time `json:"pushedAt,omitempty"`
	SizeBytes int64      `json:"sizeBytes,omitempty"`
	Digest    string     `json:"digest,omitempty"`
}

// RegistryImageCatalog is the live tag list for one repository under a service's registry settings.
type RegistryImageCatalog struct {
	Provider     string             `json:"provider"`
	Server       string             `json:"server,omitempty"`
	Namespace    string             `json:"namespace,omitempty"`
	Repository   string             `json:"repository"`
	Region       string             `json:"region,omitempty"`
	Tags         []RegistryImageTag `json:"tags"`
	RelatedRepos []string           `json:"relatedRepos,omitempty"`
	Source       string             `json:"source,omitempty"`
	Message      string             `json:"message,omitempty"`
}
