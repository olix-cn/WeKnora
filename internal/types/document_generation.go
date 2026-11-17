package types

import "time"

// DocumentDataset is a private, immutable import of a caller-owned MCP result.
type DocumentDataset struct {
	Issues      []string               `json:"issues" gorm:"-"`
	Fields      []DocumentDatasetField `json:"fields" gorm:"-"`
	ID          string                 `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64                 `json:"-"`
	UserID      string                 `json:"-"`
	SessionID   string                 `json:"session_id"`
	MessageID   string                 `json:"message_id"`
	ResultID    string                 `json:"result_id"`
	Payload     string                 `json:"-"`
	ContentHash string                 `json:"content_hash"`
	RowCount    int                    `json:"row_count"`
	ColumnCount int                    `json:"column_count"`
	Partial     bool                   `json:"partial"`
	CreatedAt   time.Time              `json:"created_at"`
	ExpiresAt   time.Time              `json:"expires_at"`
}

// DocumentDatasetField exposes source identifiers for explicit template bindings.
type DocumentDatasetField struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// TableName returns the private dataset table.
func (DocumentDataset) TableName() string { return "document_datasets" }

// DocumentGeneration is both the durable task record and the owner of its file.
// Storage bytes are held once by the existing file service, not in this table.
type DocumentGeneration struct {
	ID          string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64    `json:"-"`
	UserID      string    `json:"-"`
	RequestKey  string    `json:"-"`
	RequestHash string    `json:"-"`
	DatasetID   string    `json:"dataset_id"`
	TemplateID  string    `json:"template_id"`
	Status      string    `json:"status"`
	Partial     bool      `json:"partial"`
	FileName    string    `json:"file_name"`
	FileRef     string    `json:"-"`
	FileSize    int64     `json:"file_size"`
	ContentHash string    `json:"content_hash"`
	ErrorCode   string    `json:"error_code,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// TableName returns the generation task table.
func (DocumentGeneration) TableName() string { return "document_generations" }
