-- Legacy KBs have no policy row and continue to inherit workspace permissions.
CREATE TABLE knowledge_base_access_policies (
    knowledge_base_id VARCHAR(36) PRIMARY KEY REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    mode VARCHAR(16) NOT NULL CHECK (mode IN ('inherit', 'restricted')),
    version BIGINT NOT NULL CHECK (version > 0),
    updated_by VARCHAR(36) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_kb_access_policies_tenant ON knowledge_base_access_policies(tenant_id);

CREATE TABLE knowledge_base_member_grants (
    knowledge_base_id VARCHAR(36) NOT NULL REFERENCES knowledge_base_access_policies(knowledge_base_id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    user_id VARCHAR(36) NOT NULL,
    membership_id BIGINT NOT NULL,
    permission VARCHAR(16) NOT NULL CHECK (permission IN ('viewer', 'editor', 'admin')),
    PRIMARY KEY (knowledge_base_id, user_id)
);
CREATE INDEX idx_kb_member_grants_user ON knowledge_base_member_grants(tenant_id, user_id, membership_id);

-- No TTL: an interrupted external transfer requires explicit outcome recovery.
CREATE TABLE knowledge_base_transfer_fences (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
    source_id VARCHAR(36) NOT NULL, target_id VARCHAR(36) NOT NULL,
    task_id VARCHAR(128) NOT NULL, state VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (state IN ('active','failed')),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_kb_transfer_fences_tenant ON knowledge_base_transfer_fences(tenant_id);
