DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM corporate_departments) OR EXISTS (SELECT 1 FROM corporate_personnel) THEN
  RAISE EXCEPTION 'Corporate directory is in use; preserve personnel data before rollback';
 END IF;
END $$;
DROP TRIGGER corporate_clear_inactive_lead ON users;
DROP FUNCTION clear_inactive_department_lead();
DROP TABLE corporate_personnel;
DROP TABLE corporate_departments;
DROP TABLE corporate_directories;
