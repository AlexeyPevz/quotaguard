package cleanup

import (
	"fmt"
	"time"
)

// RetentionPolicy defines the retention policy for a specific table.
type RetentionPolicy struct {
	TableName       string        `json:"table_name"`
	RetentionPeriod time.Duration `json:"retention_period"`
	Enabled         bool          `json:"enabled"`
	UseSoftDelete   bool          `json:"use_soft_delete"`
}

// Validate validates the retention policy configuration.
func (p *RetentionPolicy) Validate() error {
	if p.TableName == "" {
		return fmt.Errorf("table_name is required")
	}
	if p.RetentionPeriod <= 0 {
		return fmt.Errorf("retention_period must be positive")
	}
	return nil
}

// DefaultPolicies returns the default retention policies for all tables.
var DefaultPolicies = []RetentionPolicy{
	{
		TableName:       "quota_history",
		RetentionPeriod: 7 * 24 * time.Hour, // 7 days
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "routing_events",
		RetentionPeriod: 7 * 24 * time.Hour, // 7 days
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "health_history",
		RetentionPeriod: 24 * time.Hour, // 24 hours
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "alerts",
		RetentionPeriod: 30 * 24 * time.Hour, // 30 days
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "quota_events",
		RetentionPeriod: 7 * 24 * time.Hour, // 7 days
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "reservations",
		RetentionPeriod: 24 * time.Hour, // 24 hours for expired reservations
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "soft_deleted_records",
		RetentionPeriod: 30 * 24 * time.Hour, // 30 days before hard delete
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "schema_migrations",
		RetentionPeriod: 365 * 24 * time.Hour, // Keep forever (1 year minimum)
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "accounts",
		RetentionPeriod: 365 * 24 * time.Hour, // Keep forever (1 year minimum)
		Enabled:         true,
		UseSoftDelete:   false,
	},
	{
		TableName:       "quotas",
		RetentionPeriod: 365 * 24 * time.Hour, // Keep forever (1 year minimum)
		Enabled:         true,
		UseSoftDelete:   false,
	},
}

// PolicyProvider interface for getting retention policies.
type PolicyProvider interface {
	GetPolicy(tableName string) *RetentionPolicy
	GetAllPolicies() []RetentionPolicy
}

// InMemoryPolicyProvider provides retention policies from memory.
type InMemoryPolicyProvider struct {
	policies map[string]RetentionPolicy
}

// NewInMemoryPolicyProvider creates a new policy provider with the given policies.
func NewInMemoryPolicyProvider(policies []RetentionPolicy) *InMemoryPolicyProvider {
	p := make(map[string]RetentionPolicy)
	for _, policy := range policies {
		p[policy.TableName] = policy
	}
	return &InMemoryPolicyProvider{policies: p}
}

// GetPolicy returns the retention policy for the given table.
func (p *InMemoryPolicyProvider) GetPolicy(tableName string) *RetentionPolicy {
	if policy, ok := p.policies[tableName]; ok {
		return &policy
	}
	return nil
}

// GetAllPolicies returns all configured retention policies.
func (p *InMemoryPolicyProvider) GetAllPolicies() []RetentionPolicy {
	policies := make([]RetentionPolicy, 0, len(p.policies))
	for _, policy := range p.policies {
		policies = append(policies, policy)
	}
	return policies
}

// GetPolicy returns the retention policy for the given table using default policies.
func GetPolicy(tableName string) *RetentionPolicy {
	for _, policy := range DefaultPolicies {
		if policy.TableName == tableName {
			return &policy
		}
	}
	return nil
}

// GetAllPolicies returns all default retention policies.
func GetAllPolicies() []RetentionPolicy {
	return DefaultPolicies
}
