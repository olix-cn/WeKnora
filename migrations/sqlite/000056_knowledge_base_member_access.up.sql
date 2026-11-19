CREATE TABLE knowledge_base_access_policies (
    knowledge_base_id TEXT PRIMARY KEY REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('inherit', 'restricted')),
    version INTEGER NOT NULL CHECK (version > 0),
    updated_by TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX idx_kb_access_policies_tenant ON knowledge_base_access_policies(tenant_id);

CREATE TABLE knowledge_base_member_grants (
    knowledge_base_id TEXT NOT NULL REFERENCES knowledge_base_access_policies(knowledge_base_id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    user_id TEXT NOT NULL,
    membership_id INTEGER NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('viewer', 'editor', 'admin')),
    PRIMARY KEY (knowledge_base_id, user_id)
);
CREATE INDEX idx_kb_member_grants_user ON knowledge_base_member_grants(tenant_id, user_id, membership_id);

CREATE TABLE knowledge_base_transfer_fences (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL,
    source_id TEXT NOT NULL, target_id TEXT NOT NULL,
    task_id TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','failed')),
    created_at DATETIME NOT NULL
);
CREATE INDEX idx_kb_transfer_fences_tenant ON knowledge_base_transfer_fences(tenant_id);
