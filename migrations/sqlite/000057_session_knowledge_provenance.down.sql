CREATE TEMP TABLE session_provenance_rollback_guard (safe INTEGER CHECK (safe = 1));
INSERT INTO session_provenance_rollback_guard(safe)
SELECT CASE WHEN EXISTS (SELECT 1 FROM session_knowledge_sources)
    OR EXISTS (SELECT 1 FROM session_knowledge_evidence) THEN 0 ELSE 1 END;
DROP TABLE session_provenance_rollback_guard;
DROP TRIGGER messages_knowledge_insert;
DROP TRIGGER messages_knowledge_update;
DROP TRIGGER messages_knowledge_delete;
DROP TABLE session_knowledge_evidence;
DROP TABLE session_knowledge_sources;
DROP TABLE session_knowledge_revisions;
