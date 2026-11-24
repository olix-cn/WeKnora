package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func directoryFixture(t *testing.T) (*gorm.DB, *CorporateDirectoryRepository, context.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "directory.db")), &gorm.Config{})
	require.NoError(t, err)
	sq, err := db.DB()
	require.NoError(t, err)
	sq.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sq.Close() })
	require.NoError(t, db.Exec("PRAGMA foreign_keys=ON").Error)
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuditLog{}))
	script, err := os.ReadFile("../../../migrations/sqlite/000058_corporate_directory.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(script)).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Synthetic directory", Status: "active"}).Error)
	for _, u := range []types.User{
		{ID: "admin", Username: "admin", IsSystemAdmin: true, IsActive: true},
		{ID: "alice", Username: "alice", IsActive: true},
		{ID: "bob", Username: "bob", IsActive: true},
	} {
		require.NoError(t, func() error { u.TenantID = 1; return db.Create(&u).Error }())
	}
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "admin")
	return db, NewCorporateDirectoryRepository(db), ctx
}

func TestCorporateDirectoryHierarchyMembershipAndAtomicAudit(t *testing.T) {
	db, r, ctx := directoryFixture(t)
	parent, err := r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "技术部"})
	require.NoError(t, err)
	child, err := r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "研发组", ParentID: &parent.ID})
	require.NoError(t, err)
	_, err = r.SaveDepartment(ctx, parent.ID, types.DepartmentUpdate{
		Name: "技术部", ParentID: &child.ID, ExpectedVersion: 1,
	})
	require.ErrorIs(t, err, ErrDirectoryInvalid)
	_, err = r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "技术部"})
	require.ErrorIs(t, err, ErrDirectoryConflict)
	require.NoError(t, r.MoveMembers(ctx, &child.ID, []types.PersonnelMove{{UserID: "alice"}}))
	members, total, err := r.Members(ctx, types.PersonnelQuery{
		DepartmentID: parent.ID, IncludeDescendants: true, Limit: 20,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "alice", members[0].Username)
	_, total, err = r.Members(ctx, types.PersonnelQuery{DepartmentID: parent.ID, Limit: 20})
	require.NoError(t, err)
	require.Zero(t, total)
	require.ErrorIs(t, r.DeleteDepartment(ctx, parent.ID, 1), ErrDepartmentOccupied)
	require.ErrorIs(t, r.DeleteDepartment(ctx, child.ID, 1), ErrDepartmentOccupied)
	// A stale second row rolls the entire batch back, including its first row.
	err = r.MoveMembers(ctx, &parent.ID, []types.PersonnelMove{
		{UserID: "alice", ExpectedVersion: 1}, {UserID: "bob", ExpectedVersion: 99},
	})
	require.ErrorIs(t, err, ErrDirectoryConflict)
	var profile types.PersonnelProfile
	require.NoError(t, db.First(&profile, "user_id = ?", "alice").Error)
	require.Equal(t, child.ID, *profile.DepartmentID)
	child, err = r.SaveDepartment(ctx, child.ID, types.DepartmentUpdate{
		Name: "研发组", ParentID: &parent.ID, LeaderID: ptrString("alice"), ExpectedVersion: 1,
	})
	require.NoError(t, err)
	err = r.MoveMembers(ctx, &parent.ID, []types.PersonnelMove{{UserID: "alice", ExpectedVersion: 1}})
	require.ErrorIs(t, err, ErrDepartmentLeader)
	require.ErrorIs(t, r.DeleteDepartment(ctx, child.ID, 1), ErrDirectoryConflict)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_directory_audit BEFORE INSERT ON audit_logs
 BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`).Error)
	_, err = r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "质量部"})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.Department{}).Where("name = ?", "质量部").Count(&count).Error)
	require.Zero(t, count)
}
func ptrString(s string) *string { return &s }
func TestCorporateDirectoryRejectsRolesMachinesAndStaleAdmins(t *testing.T) {
	db, r, ctx := directoryFixture(t)
	for _, other := range []context.Context{
		context.Background(), context.WithValue(ctx, types.UserIDContextKey, "alice"),
		types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true}),
		types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalIMUser, ID: "admin"}),
	} {
		_, _, err := r.Tree(other)
		require.ErrorIs(t, err, access.ErrForbidden)
		_, err = r.SaveDepartment(other, "", types.DepartmentUpdate{Name: "forbidden"})
		require.ErrorIs(t, err, access.ErrForbidden)
	}
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "admin").Update("is_system_admin", false).Error)
	_, _, err := r.Tree(ctx)
	require.ErrorIs(t, err, access.ErrForbidden)
}

func TestCorporateDirectorySearchBeforePaginationAndRollbackGuard(t *testing.T) {
	db, r, ctx := directoryFixture(t)
	require.NoError(t, r.UpdatePersonnel(ctx, "alice", "张三", 0))
	rows, total, err := r.Members(ctx, types.PersonnelQuery{Keyword: "张三", Limit: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "alice", rows[0].ID)
	require.ErrorIs(t, r.UpdatePersonnel(ctx, "alice", "stale", 0), ErrDirectoryConflict)
	_, total, err = r.Members(ctx, types.PersonnelQuery{Keyword: "%", Limit: 20})
	require.NoError(t, err)
	require.Zero(t, total, "wildcard characters are literal")
	_, total, err = r.Members(ctx, types.PersonnelQuery{DepartmentID: "unassigned", Limit: 1})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	down, err := os.ReadFile("../../../migrations/sqlite/000058_corporate_directory.down.sql")
	require.NoError(t, err)
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(down)).Error }))
	require.True(t, db.Migrator().HasTable(&types.PersonnelProfile{}))
}

func TestCorporateDirectoryCreateUserProfileAtomicAndLeaderDisable(t *testing.T) {
	db, r, ctx := directoryFixture(t)
	require.NoError(t, db.AutoMigrate(&types.TenantMember{}, &types.AuthToken{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "workspace", Status: "active"}).Error)
	d, err := r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "Quality"})
	require.NoError(t, err)
	u := &types.User{ID: "new", Username: "new", PasswordHash: "synthetic", TenantID: 42, IsActive: true}
	repo := &userRepository{db: db}
	require.NoError(t, repo.CreateUserWithDirectory(ctx, u, types.TenantRoleViewer, "admin",
		types.DirectoryProfileInput{
			DepartmentID: &d.ID, DisplayName: "Synthetic",
		}))
	var p types.PersonnelProfile
	require.NoError(t, db.First(&p, "user_id = ?", u.ID).Error)
	require.Equal(t, d.ID, *p.DepartmentID)
	var m types.TenantMember
	require.NoError(t, db.First(&m, "user_id = ?", u.ID).Error)
	require.Equal(t, types.TenantRoleViewer, m.Role)
	d, err = r.SaveDepartment(ctx, d.ID, types.DepartmentUpdate{Name: "Quality", ExpectedVersion: 1, LeaderID: &u.ID})
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", u.ID).Update("is_active", false).Error)
	require.NoError(t, db.First(&d, "id = ?", d.ID).Error)
	require.Nil(t, d.LeaderID)
	require.EqualValues(t, 3, d.Version)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_new_profile BEFORE INSERT ON corporate_personnel
 BEGIN SELECT RAISE(ABORT,'synthetic profile failure'); END`).Error)
	failing := &types.User{ID: "failed", Username: "failed", PasswordHash: "synthetic", TenantID: 42, IsActive: true}
	err = repo.CreateUserWithDirectory(ctx, failing, types.TenantRoleViewer, "admin",
		types.DirectoryProfileInput{DepartmentID: &d.ID})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.User{}).Where("id = ?", "failed").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id = ?", "failed").Count(&count).Error)
	require.Zero(t, count)
}

func TestDepartmentCodesAreServerGeneratedAndImmutable(t *testing.T) {
	_, r, ctx := directoryFixture(t)
	d, err := r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "Generated"})
	require.NoError(t, err)
	require.NotEmpty(t, d.Code)
	require.Contains(t, d.Code, "DEP-")
	same, err := r.SaveDepartment(ctx, d.ID, types.DepartmentUpdate{Name: "Renamed", ExpectedVersion: 1})
	require.NoError(t, err)
	require.Equal(t, d.Code, same.Code)
	_, err = r.SaveDepartment(ctx, d.ID, types.DepartmentUpdate{Name: "Renamed", Code: "CLIENT", ExpectedVersion: 2})
	require.ErrorIs(t, err, ErrDirectoryInvalid)
	_, err = r.SaveDepartment(ctx, "", types.DepartmentUpdate{Name: "Invalid", Code: "CLIENT"})
	require.ErrorIs(t, err, ErrDirectoryInvalid)
}

func TestDepartmentNameForUserTracksAssignmentAndRename(t *testing.T) {
	_, repo, admin := directoryFixture(t)
	assertName := func(id, want string) {
		t.Helper()
		name, err := repo.DepartmentNameForUser(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, want, name)
	}
	assertName("alice", "")
	department, err := repo.SaveDepartment(admin, "", types.DepartmentUpdate{Name: "技术部"})
	require.NoError(t, err)
	require.NoError(t, repo.MoveMembers(admin, &department.ID, []types.PersonnelMove{{UserID: "alice"}}))
	assertName("alice", "技术部")
	assertName("bob", "")
	_, err = repo.SaveDepartment(admin, department.ID, types.DepartmentUpdate{Name: "研发部", ExpectedVersion: department.Version})
	require.NoError(t, err)
	assertName("alice", "研发部")
}
