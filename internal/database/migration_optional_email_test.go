package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteOptionalEmailMigrationPreservesUsersAndEnforcesRealEmailUniqueness(t *testing.T) {
	db := openSQLiteDB(t, filepath.Join(t.TempDir(), "email.db"))
	_, err := db.Exec(`CREATE TABLE users (
id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, email TEXT NOT NULL UNIQUE,
password_hash TEXT NOT NULL, avatar TEXT, tenant_id INTEGER, is_active BOOLEAN,
can_access_all_tenants BOOLEAN, preferences TEXT, created_at DATETIME, updated_at DATETIME,
deleted_at DATETIME, is_system_admin BOOLEAN);
INSERT INTO users VALUES ('original','alice','alice@example.com','unchanged-hash',NULL,7,1,0,'{}',NULL,NULL,NULL,1);`)
	require.NoError(t, err)
	apply := func(direction string) error {
		data, err := os.ReadFile(filepath.Join(sqliteRepoRoot(t), "migrations/sqlite/000055_optional_user_email."+direction+".sql"))
		require.NoError(t, err)
		tx, err := db.Begin()
		require.NoError(t, err)
		if _, err = tx.Exec(string(data)); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	require.NoError(t, apply("up"))
	var hash string
	var tenant int
	var admin bool
	require.NoError(t, db.QueryRow("SELECT password_hash, tenant_id, is_system_admin FROM users WHERE id='original'").Scan(&hash, &tenant, &admin))
	require.Equal(t, "unchanged-hash", hash)
	require.Equal(t, 7, tenant)
	require.True(t, admin)
	insert := func(id, email string) error {
		_, err := db.Exec("INSERT INTO users(id,username,email,password_hash) VALUES (?,?,?,'hash')", id, id, email)
		return err
	}
	require.NoError(t, insert("one", ""))
	require.NoError(t, insert("two", ""))
	require.Error(t, insert("duplicate", "alice@example.com"))
	require.Error(t, apply("down"), "rollback must refuse duplicate empty emails without deleting users")
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM users").Scan(&count))
	require.Equal(t, 3, count)
	_, err = db.Exec("DELETE FROM users WHERE id='two'")
	require.NoError(t, err)
	require.NoError(t, apply("down"))
	require.Error(t, insert("three", ""))
}
