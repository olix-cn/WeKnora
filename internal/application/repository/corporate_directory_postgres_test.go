package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCorporateDirectoryPostgresMigrationAndConcurrentSave(t *testing.T) {
	dsn := os.Getenv("WEKNORA_DIRECTORY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires explicitly supplied disposable PostgreSQL database")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlAdmin, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlAdmin.Close() })
	schema := "directory_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuditLog{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Synthetic", Status: "active"}).Error)
	require.NoError(t, db.Create(&types.User{
		ID: "admin", Username: "admin", TenantID: 1, IsActive: true, IsSystemAdmin: true,
	}).Error)
	up, err := os.ReadFile("../../../migrations/versioned/000142_corporate_directory.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	repo := NewCorporateDirectoryRepository(db)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "admin")
	d, err := repo.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "Engineering"})
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, name := range []string{"One", "Two"} {
		go func(name string) {
			<-start
			_, err := repo.SaveDepartment(ctx, d.ID, types.DepartmentUpdate{Name: name, ExpectedVersion: 1})
			results <- err
		}(name)
	}
	close(start)
	first, second := <-results, <-results
	oneSaved := (first == nil && second == ErrDirectoryConflict) || (second == nil && first == ErrDirectoryConflict)
	require.True(t, oneSaved)
	down, err := os.ReadFile("../../../migrations/versioned/000142_corporate_directory.down.sql")
	require.NoError(t, err)
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
	require.NoError(t, repo.MoveMembers(ctx, &d.ID, []types.PersonnelMove{{UserID: "admin"}}))
	var current types.Department
	require.NoError(t, db.First(&current, "id = ?", d.ID).Error)
	currentPtr := current.ID
	_, err = repo.SaveDepartment(ctx, current.ID, types.DepartmentUpdate{
		Name: current.Name, ExpectedVersion: current.Version, LeaderID: ptrString("admin"),
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "admin").Update("is_active", false).Error)
	var after types.Department
	require.NoError(t, db.First(&after, "id = ?", currentPtr).Error)
	require.Nil(t, after.LeaderID)
}
