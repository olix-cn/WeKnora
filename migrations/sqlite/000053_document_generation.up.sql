CREATE TABLE document_datasets (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, user_id VARCHAR(128) NOT NULL,
    session_id VARCHAR(36) NOT NULL, message_id VARCHAR(36) NOT NULL, result_id VARCHAR(128) NOT NULL,
    payload TEXT NOT NULL, content_hash VARCHAR(64) NOT NULL,
    row_count INTEGER NOT NULL, column_count INTEGER NOT NULL, partial BOOLEAN NOT NULL,
    created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL
);
CREATE INDEX idx_document_datasets_owner ON document_datasets(tenant_id, user_id, id);
CREATE TABLE document_generations (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, user_id VARCHAR(128) NOT NULL,
    request_key VARCHAR(128) NOT NULL, request_hash VARCHAR(64) NOT NULL,
    dataset_id VARCHAR(36) NOT NULL, template_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL, partial BOOLEAN NOT NULL, file_name VARCHAR(255) NOT NULL,
    file_ref TEXT NOT NULL DEFAULT '', file_size BIGINT NOT NULL DEFAULT 0,
    content_hash VARCHAR(64) NOT NULL DEFAULT '', error_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, expires_at DATETIME NOT NULL,
    UNIQUE(tenant_id, user_id, request_key)
);
CREATE INDEX idx_document_generations_owner ON document_generations(tenant_id, user_id, created_at, id);
