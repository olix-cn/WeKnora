package types

import "time"

// CorporateDirectory is the installation's personnel directory, not an org share or tenant.
type CorporateDirectory struct {
	ID      uint64 `json:"id" gorm:"primaryKey"`
	Name    string `json:"name"`
	Version uint64 `json:"version"`
}

// TableName identifies the independent personnel directory table.
func (CorporateDirectory) TableName() string { return "corporate_directories" }

// Department never grants resource access. ParentID=nil denotes a top-level department.
type Department struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	DirectoryID uint64    `json:"-"`
	ParentID    *string   `json:"parent_id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	SortOrder   int       `json:"sort_order"`
	LeaderID    *string   `json:"leader_id"`
	Version     uint64    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	MemberCount int64     `json:"member_count" gorm:"-"`
}

// TableName identifies the independent personnel directory table.
func (Department) TableName() string { return "corporate_departments" }

// PersonnelProfile holds optional platform directory metadata for a user.
type PersonnelProfile struct {
	UserID       string    `json:"user_id" gorm:"primaryKey"`
	DirectoryID  uint64    `json:"-"`
	DepartmentID *string   `json:"department_id"`
	DisplayName  string    `json:"display_name"`
	Version      uint64    `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName identifies the independent personnel directory table.
func (PersonnelProfile) TableName() string { return "corporate_personnel" }

// DepartmentUpdate is a complete versioned department replacement.
type DepartmentUpdate struct {
	ExpectedVersion uint64  `json:"expected_version"`
	ParentID        *string `json:"parent_id"`
	Name            string  `json:"name"`
	Code            string  `json:"code"`
	SortOrder       int     `json:"sort_order"`
	LeaderID        *string `json:"leader_id"`
}

// PersonnelMove binds a member move to its current profile version.
type PersonnelMove struct {
	UserID          string `json:"user_id"`
	ExpectedVersion uint64 `json:"expected_version"`
}

// PersonnelQuery filters the platform directory before pagination.
type PersonnelQuery struct {
	Keyword            string
	DepartmentID       string // empty=all; unassigned=without a department
	IncludeDescendants bool
	Status             string
	Offset             int
	Limit              int
}

// PersonnelView intentionally excludes password hashes, preferences and credentials.
type PersonnelView struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name"`
	DepartmentID   *string   `json:"department_id"`
	DepartmentName string    `json:"department_name"`
	Version        uint64    `json:"version"`
	IsActive       bool      `json:"is_active"`
	IsSystemAdmin  bool      `json:"is_system_admin"`
	TenantID       uint64    `json:"tenant_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// DirectoryProfileInput optionally provisions personnel metadata with an account.
type DirectoryProfileInput struct {
	DepartmentID *string `json:"department_id"`
	DisplayName  string  `json:"display_name"`
}
