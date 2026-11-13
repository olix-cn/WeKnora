-- Refuse rollback if multiple empty emails exist. Never invent addresses or delete users.
-- Creating the old constraint first ensures failure leaves the partial index intact.
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
DROP INDEX IF EXISTS idx_users_email;
CREATE INDEX idx_users_email ON users (email);
ALTER TABLE users ALTER COLUMN email DROP DEFAULT;
