CREATE TABLE session_knowledge_revisions (
    session_id TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 1,
    indexed_revision INTEGER NOT NULL DEFAULT 0,
    legacy_unverified BOOLEAN NOT NULL DEFAULT 0
);
CREATE TABLE session_knowledge_sources (
    session_id TEXT NOT NULL, knowledge_base_id TEXT NOT NULL,
    PRIMARY KEY (session_id, knowledge_base_id)
);
CREATE INDEX idx_session_knowledge_sources_kb ON session_knowledge_sources(knowledge_base_id, session_id);
CREATE TABLE session_knowledge_evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL,
    knowledge_references TEXT, execution_context TEXT, mentioned_items TEXT, agent_steps TEXT
);
CREATE INDEX idx_session_knowledge_evidence_session ON session_knowledge_evidence(session_id);
INSERT INTO session_knowledge_revisions(session_id, legacy_unverified)
SELECT DISTINCT session_id, 1 FROM messages;
CREATE TRIGGER messages_knowledge_insert AFTER INSERT ON messages BEGIN
    INSERT INTO session_knowledge_revisions(session_id) VALUES (NEW.session_id)
    ON CONFLICT(session_id) DO UPDATE SET revision = revision + 1;
END;
CREATE TRIGGER messages_knowledge_update AFTER UPDATE ON messages BEGIN
    INSERT INTO session_knowledge_evidence(session_id, knowledge_references, execution_context, mentioned_items, agent_steps)
    SELECT OLD.session_id, OLD.knowledge_references, OLD.execution_context, OLD.mentioned_items, OLD.agent_steps
    WHERE OLD.session_id IS NOT NEW.session_id OR OLD.deleted_at IS NOT NEW.deleted_at
        OR OLD.knowledge_references IS NOT NEW.knowledge_references
        OR OLD.execution_context IS NOT NEW.execution_context OR OLD.mentioned_items IS NOT NEW.mentioned_items
        OR OLD.agent_steps IS NOT NEW.agent_steps;
    INSERT INTO session_knowledge_revisions(session_id) VALUES (OLD.session_id)
    ON CONFLICT(session_id) DO UPDATE SET revision = revision + 1;
    INSERT INTO session_knowledge_revisions(session_id) SELECT NEW.session_id WHERE NEW.session_id <> OLD.session_id
    ON CONFLICT(session_id) DO UPDATE SET revision = revision + 1;
END;
CREATE TRIGGER messages_knowledge_delete AFTER DELETE ON messages BEGIN
    INSERT INTO session_knowledge_evidence(session_id, knowledge_references, execution_context, mentioned_items, agent_steps)
    VALUES (OLD.session_id, OLD.knowledge_references, OLD.execution_context, OLD.mentioned_items, OLD.agent_steps);
    INSERT INTO session_knowledge_revisions(session_id) VALUES (OLD.session_id)
    ON CONFLICT(session_id) DO UPDATE SET revision = revision + 1;
END;
