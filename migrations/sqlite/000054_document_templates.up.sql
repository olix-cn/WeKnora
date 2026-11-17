CREATE TABLE document_template_versions (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, created_by VARCHAR(128) NOT NULL,
    name VARCHAR(120) NOT NULL, family_id VARCHAR(36) NOT NULL, version INTEGER NOT NULL,
    status VARCHAR(16) NOT NULL, file_ref TEXT NOT NULL, content_hash VARCHAR(64) NOT NULL,
    layout TEXT NOT NULL DEFAULT '', test_hash VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
    UNIQUE(tenant_id, family_id, version)
);
CREATE INDEX idx_document_templates_tenant ON document_template_versions(tenant_id, status, created_at);
