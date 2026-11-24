-- Personnel hierarchy is independent from workspace and cross-workspace sharing.
CREATE TABLE corporate_directories (
 id INTEGER PRIMARY KEY CHECK (id = 1), name TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1
);
INSERT INTO corporate_directories(id,name) VALUES (1,'公司');
CREATE TABLE corporate_departments (
 id TEXT PRIMARY KEY, directory_id INTEGER NOT NULL REFERENCES corporate_directories(id),
 parent_id TEXT REFERENCES corporate_departments(id), name TEXT NOT NULL,
 code TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0,
 leader_id TEXT REFERENCES users(id), version INTEGER NOT NULL DEFAULT 1,
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE UNIQUE INDEX uniq_corporate_department_name ON corporate_departments(directory_id,COALESCE(parent_id,''),LOWER(name));
CREATE UNIQUE INDEX uniq_corporate_department_code ON corporate_departments(directory_id,LOWER(code)) WHERE code <> '';
CREATE INDEX idx_corporate_department_parent ON corporate_departments(parent_id);
CREATE TABLE corporate_personnel (
 user_id TEXT PRIMARY KEY REFERENCES users(id), directory_id INTEGER NOT NULL REFERENCES corporate_directories(id),
 department_id TEXT REFERENCES corporate_departments(id), display_name TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1, updated_at DATETIME NOT NULL
);
CREATE INDEX idx_corporate_personnel_department ON corporate_personnel(department_id);
-- Existing and future accounts without a profile are explicitly unassigned.

CREATE TRIGGER corporate_clear_inactive_lead AFTER UPDATE OF is_active,deleted_at ON users
WHEN NEW.is_active = 0 OR NEW.deleted_at IS NOT NULL BEGIN
 UPDATE corporate_departments SET leader_id=NULL,version=version+1,updated_at=CURRENT_TIMESTAMP
 WHERE leader_id=NEW.id;
END;
