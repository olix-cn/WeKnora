-- Sources are monotonic and may survive deleted citations. Reconstructing from
-- today's messages cannot prove it recovers the same dependency set.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM session_knowledge_sources) OR EXISTS (SELECT 1 FROM session_knowledge_evidence) THEN
        RAISE EXCEPTION 'Session knowledge provenance is in use; review retained sources before rollback';
    END IF;
END $$;
DROP TRIGGER messages_knowledge_revision ON messages;
DROP FUNCTION invalidate_session_knowledge_revision();
DROP TABLE session_knowledge_evidence;
DROP TABLE session_knowledge_sources;
DROP TABLE session_knowledge_revisions;
