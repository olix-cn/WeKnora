package types

import "time"

// KBAccessMode controls workspace-member visibility, independently of org shares.
type KBAccessMode string

// Workspace-member access modes and bounded grant-list size.
const (
	KBAccessInherit    KBAccessMode = "inherit"
	KBAccessRestricted KBAccessMode = "restricted"
	MaxKBMemberGrants               = 500
)

// IsValid rejects unknown modes instead of treating them as inherit.
func (m KBAccessMode) IsValid() bool { return m == KBAccessInherit || m == KBAccessRestricted }

// KnowledgeBaseAccessPolicy is absent for legacy/default-inherit knowledge bases.
// Version is a compare-and-swap token, not an authorization credential.
type KnowledgeBaseAccessPolicy struct {
	KnowledgeBaseID string       `json:"knowledge_base_id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64       `json:"tenant_id" gorm:"not null;index"`
	Mode            KBAccessMode `json:"mode" gorm:"type:varchar(16);not null"`
	Version         uint64       `json:"version" gorm:"not null"`
	UpdatedBy       string       `json:"updated_by" gorm:"type:varchar(36);not null"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// TableName binds policies to their dedicated table.
func (KnowledgeBaseAccessPolicy) TableName() string { return "knowledge_base_access_policies" }

// KnowledgeBaseMemberGrant binds access to a particular membership incarnation.
// Removing and recreating a membership must not resurrect an earlier grant.
type KnowledgeBaseMemberGrant struct {
	KnowledgeBaseID string        `json:"knowledge_base_id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64        `json:"tenant_id" gorm:"not null;index"`
	UserID          string        `json:"user_id" gorm:"type:varchar(36);primaryKey"`
	MembershipID    uint64        `json:"membership_id" gorm:"not null"`
	Permission      OrgMemberRole `json:"permission" gorm:"type:varchar(16);not null"`
}

// TableName binds member grants to their dedicated table.
func (KnowledgeBaseMemberGrant) TableName() string { return "knowledge_base_member_grants" }

// KBMemberGrantInput deliberately excludes tenant, membership ID and actor.
// Those are loaded by the server, not accepted from a client's policy payload.
type KBMemberGrantInput struct {
	UserID     string        `json:"user_id"`
	Permission OrgMemberRole `json:"permission"`
}

// KBAccessPolicyUpdate replaces the mode and complete explicit member list.
type KBAccessPolicyUpdate struct {
	Mode            KBAccessMode         `json:"mode"`
	ExpectedVersion uint64               `json:"expected_version"`
	Members         []KBMemberGrantInput `json:"members"`
}

// KBAccessPolicyView is the management response. It deliberately excludes the
// knowledge-base configuration, user model and internal membership identifiers.
type KBAccessPolicyView struct {
	MemberDetails []KBAccessMemberCandidate `json:"member_details,omitempty"`
	Mode          KBAccessMode              `json:"mode"`
	Version       uint64                    `json:"version"`
	Members       []KBMemberGrantInput      `json:"members"`
	UpdatedAt     *time.Time                `json:"updated_at,omitempty"`
}

// KBAccessMemberCandidate is the minimal member-selector projection. Emails,
// password fields and internal membership IDs are intentionally not included.
type KBAccessMemberCandidate struct {
	UserID   string     `json:"user_id"`
	Username string     `json:"username"`
	Role     TenantRole `json:"role"`
}

// KBTransferFence records one executing transfer attempt. No TTL is safe: a
// crashed worker may have committed external writes. Failed attempts therefore
// keep their fence for explicit recovery rather than silently reopening policy
// changes while their outcome remains unknown.
type KBTransferFence struct {
	ID        string `gorm:"type:varchar(36);primaryKey"`
	TenantID  uint64 `gorm:"not null;index"`
	SourceID  string `gorm:"type:varchar(36);not null"`
	TargetID  string `gorm:"type:varchar(36);not null"`
	TaskID    string `gorm:"type:varchar(128);not null"`
	State     string `gorm:"type:varchar(16);not null;default:active"`
	CreatedAt time.Time
}

// KBTransferRecovery confirms a stopped, reconciled failed attempt. It never
// authorizes re-running a task, restoring data or clearing a live attempt.
type KBTransferRecovery struct {
	FenceID           string `json:"fence_id"`
	TaskID            string `json:"task_id"`
	SourceID          string `json:"source_id"`
	TargetID          string `json:"target_id"`
	WorkerStopped     bool   `json:"worker_stopped"`
	OutcomeReconciled bool   `json:"outcome_reconciled"`
	Reason            string `json:"reason"`
}

// KBTransferStatus exposes coordinates only, never task payloads or content.
type KBTransferStatus struct {
	FenceID   string    `json:"fence_id"`
	TaskID    string    `json:"task_id"`
	SourceID  string    `json:"source_id"`
	TargetID  string    `json:"target_id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName binds executing transfer attempts to their durable policy fences.
func (KBTransferFence) TableName() string { return "knowledge_base_transfer_fences" }
