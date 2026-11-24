CREATE TEMP TABLE corporate_rollback_guard (safe INTEGER CHECK (safe=1));
INSERT INTO corporate_rollback_guard SELECT CASE WHEN EXISTS (SELECT 1 FROM corporate_departments)
 OR EXISTS (SELECT 1 FROM corporate_personnel) THEN 0 ELSE 1 END;
DROP TABLE corporate_rollback_guard;
DROP TRIGGER corporate_clear_inactive_lead;
DROP TABLE corporate_personnel;
DROP TABLE corporate_departments;
DROP TABLE corporate_directories;
