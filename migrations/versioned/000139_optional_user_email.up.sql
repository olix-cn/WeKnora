-- Empty email means "not supplied". Real emails retain unique login identity.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
DROP INDEX IF EXISTS idx_users_email;
ALTER TABLE users ALTER COLUMN email SET DEFAULT '';
CREATE UNIQUE INDEX idx_users_email ON users (email) WHERE email <> '';
