-- Personnel hierarchy is independent from workspace and cross-workspace sharing.
CREATE TABLE corporate_directories (
 id BIGINT PRIMARY KEY CHECK (id = 1), name VARCHAR(100) NOT NULL, version BIGINT NOT NULL DEFAULT 1
);
INSERT INTO corporate_directories(id,name) VALUES (1,'公司');
CREATE TABLE corporate_departments (
 id VARCHAR(36) PRIMARY KEY, directory_id BIGINT NOT NULL REFERENCES corporate_directories(id),
 parent_id VARCHAR(36) REFERENCES corporate_departments(id), name VARCHAR(100) NOT NULL,
 code VARCHAR(64) NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0,
 leader_id VARCHAR(36) REFERENCES users(id), version BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
 CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE UNIQUE INDEX uniq_corporate_department_name ON corporate_departments(directory_id,COALESCE(parent_id,''),LOWER(name));
CREATE UNIQUE INDEX uniq_corporate_department_code ON corporate_departments(directory_id,LOWER(code)) WHERE code <> '';
CREATE INDEX idx_corporate_department_parent ON corporate_departments(parent_id);
CREATE TABLE corporate_personnel (
 user_id VARCHAR(36) PRIMARY KEY REFERENCES users(id), directory_id BIGINT NOT NULL REFERENCES corporate_directories(id),
 department_id VARCHAR(36) REFERENCES corporate_departments(id), display_name VARCHAR(100) NOT NULL DEFAULT '',
 version BIGINT NOT NULL DEFAULT 1, updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_corporate_personnel_department ON corporate_personnel(department_id);
-- Existing and future accounts without a profile are explicitly unassigned.

-- A disabled/deleted account must not remain a department lead. Resource grants are untouched.
CREATE FUNCTION clear_inactive_department_lead() RETURNS trigger AS $$
BEGIN
 IF NOT NEW.is_active OR NEW.deleted_at IS NOT NULL THEN
  UPDATE corporate_departments SET leader_id=NULL,version=version+1,updated_at=CURRENT_TIMESTAMP
  WHERE leader_id=NEW.id;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER corporate_clear_inactive_lead AFTER UPDATE OF is_active,deleted_at ON users
FOR EACH ROW EXECUTE FUNCTION clear_inactive_department_lead();
