-- Derived metadata only. A revision is invalidated by EVERY message write,
-- including direct repository updates, forks and checkpoint repairs.
CREATE TABLE session_knowledge_revisions (
    session_id VARCHAR(36) PRIMARY KEY, revision BIGINT NOT NULL DEFAULT 1,
    indexed_revision BIGINT NOT NULL DEFAULT 0,
    legacy_unverified BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE session_knowledge_sources (
    session_id VARCHAR(36) NOT NULL, knowledge_base_id VARCHAR(36) NOT NULL,
    PRIMARY KEY (session_id, knowledge_base_id)
);
CREATE INDEX idx_session_knowledge_sources_kb ON session_knowledge_sources(knowledge_base_id, session_id);
-- Preserve source metadata BEFORE it is overwritten/deleted, even if no read
-- has rebuilt the index yet. Never store answer/content or user credentials.
CREATE TABLE session_knowledge_evidence (
    id BIGSERIAL PRIMARY KEY, session_id VARCHAR(36) NOT NULL,
    knowledge_references TEXT, execution_context TEXT, mentioned_items TEXT, agent_steps TEXT
);
CREATE INDEX idx_session_knowledge_evidence_session ON session_knowledge_evidence(session_id);
-- Existing transcripts cannot prove complete pre-migration lineage. Never
-- silently treat an empty parsed source set as declassified historical text.
INSERT INTO session_knowledge_revisions(session_id, legacy_unverified)
SELECT DISTINCT session_id, TRUE FROM messages;
CREATE FUNCTION invalidate_session_knowledge_revision() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        IF TG_OP = 'DELETE' OR OLD.session_id IS DISTINCT FROM NEW.session_id
            OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
            OR OLD.knowledge_references::text IS DISTINCT FROM NEW.knowledge_references::text
            OR OLD.execution_context::text IS DISTINCT FROM NEW.execution_context::text
            OR OLD.mentioned_items::text IS DISTINCT FROM NEW.mentioned_items::text
            OR OLD.agent_steps::text IS DISTINCT FROM NEW.agent_steps::text THEN
            INSERT INTO session_knowledge_evidence(session_id, knowledge_references, execution_context, mentioned_items, agent_steps)
            VALUES (OLD.session_id, OLD.knowledge_references::text, OLD.execution_context::text,
                OLD.mentioned_items::text, OLD.agent_steps::text);
        END IF;
        INSERT INTO session_knowledge_revisions(session_id) VALUES (OLD.session_id)
        ON CONFLICT(session_id) DO UPDATE SET revision = session_knowledge_revisions.revision + 1;
    END IF;
    IF TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND NEW.session_id IS DISTINCT FROM OLD.session_id) THEN
        INSERT INTO session_knowledge_revisions(session_id) VALUES (NEW.session_id)
        ON CONFLICT(session_id) DO UPDATE SET revision = session_knowledge_revisions.revision + 1;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER messages_knowledge_revision AFTER INSERT OR UPDATE OR DELETE ON messages
FOR EACH ROW EXECUTE FUNCTION invalidate_session_knowledge_revision();
