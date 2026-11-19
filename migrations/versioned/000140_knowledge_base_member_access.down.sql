-- Never silently reopen restricted data or discard delegated member access.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM knowledge_base_access_policies WHERE mode <> 'inherit')
        OR EXISTS (SELECT 1 FROM knowledge_base_member_grants)
        OR EXISTS (SELECT 1 FROM knowledge_base_transfer_fences) THEN
        RAISE EXCEPTION 'Knowledge base member permissions are in use; explicitly review and clear policies before rollback';
    END IF;
END $$;
DROP TABLE knowledge_base_transfer_fences;
DROP TABLE knowledge_base_member_grants;
DROP TABLE knowledge_base_access_policies;
