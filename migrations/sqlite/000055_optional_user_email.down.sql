-- The INSERT fails rather than dropping or rewriting users when empty emails collide.
CREATE TABLE users_required_email (
    id VARCHAR(36) PRIMARY KEY,
    username VARCHAR(100) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    avatar VARCHAR(500),
    tenant_id INTEGER,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    can_access_all_tenants BOOLEAN NOT NULL DEFAULT 0,
    preferences TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME,
    is_system_admin BOOLEAN NOT NULL DEFAULT 0
);
INSERT INTO users_required_email SELECT id, username, email, password_hash, avatar,
    tenant_id, is_active, can_access_all_tenants, preferences, created_at,
    updated_at, deleted_at, is_system_admin FROM users;
DROP TABLE users;
ALTER TABLE users_required_email RENAME TO users;
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_tenant_id ON users(tenant_id);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);
CREATE INDEX idx_users_is_system_admin ON users(is_system_admin);
