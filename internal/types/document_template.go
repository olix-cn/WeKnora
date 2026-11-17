package types

import "time"

// DocumentTemplateVersion is immutable after publication; changes require a new upload/version.
type DocumentTemplateVersion struct {
	CanManage   bool      `json:"can_manage" gorm:"-"`
	ID          string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64    `json:"-"`
	CreatedBy   string    `json:"-"`
	Name        string    `json:"name"`
	FamilyID    string    `json:"family_id"`
	Version     int       `json:"version"`
	Status      string    `json:"status"`
	FileRef     string    `json:"-"`
	ContentHash string    `json:"content_hash"`
	Layout      string    `json:"-"`
	TestHash    string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName returns the document template table.
func (DocumentTemplateVersion) TableName() string { return "document_template_versions" }
