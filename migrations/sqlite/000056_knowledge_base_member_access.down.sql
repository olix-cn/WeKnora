-- Abort before dropping either table if member permissions are still in use.
CREATE TEMP TABLE kb_access_rollback_guard (safe INTEGER CHECK (safe = 1));
INSERT INTO kb_access_rollback_guard (safe)
SELECT CASE WHEN EXISTS (SELECT 1 FROM knowledge_base_access_policies WHERE mode <> 'inherit')
    OR EXISTS (SELECT 1 FROM knowledge_base_member_grants)
    OR EXISTS (SELECT 1 FROM knowledge_base_transfer_fences) THEN 0 ELSE 1 END;
DROP TABLE kb_access_rollback_guard;
DROP TABLE knowledge_base_transfer_fences;
DROP TABLE knowledge_base_member_grants;
DROP TABLE knowledge_base_access_policies;
