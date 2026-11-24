package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrDirectoryInvalid identifies invalid hierarchy or personnel input.
	ErrDirectoryInvalid = errors.New("invalid department or personnel request")
	// ErrDirectoryConflict rejects stale versions or duplicate department identifiers.
	ErrDirectoryConflict = errors.New("organization changed; reload before saving")
	// ErrDepartmentOccupied protects nonempty departments from deletion.
	ErrDepartmentOccupied = errors.New("department has members, children or a leader; move them first")
	// ErrDepartmentLeader requires an active direct member and explicit leader reassignment.
	ErrDepartmentLeader = errors.New("reassign the department leader before moving or disabling this user")
)

// CorporateDirectoryRepository persists platform personnel, independently from resource grants.
type CorporateDirectoryRepository struct{ db *gorm.DB }

// NewCorporateDirectoryRepository creates the personnel persistence adapter.
func NewCorporateDirectoryRepository(db *gorm.DB) *CorporateDirectoryRepository {
	return &CorporateDirectoryRepository{db: db}
}

// DepartmentNameForUser returns the current department name or an empty string
// for an unassigned user. The caller must authorize access to the user identity.
func (r *CorporateDirectoryRepository) DepartmentNameForUser(ctx context.Context, userID string) (string, error) {
	var result struct{ Name string }
	err := r.db.WithContext(ctx).Table("corporate_personnel AS p").
		Select("d.name").Joins("JOIN corporate_departments AS d ON d.id = p.department_id AND d.directory_id = p.directory_id").
		Where("p.user_id = ?", userID).Limit(1).Scan(&result).Error
	return result.Name, err
}

// directoryActor reloads platform authority; workspace roles and machine keys never suffice.
func directoryActor(ctx context.Context, tx *gorm.DB) (string, error) {
	id, _ := types.UserIDFromContext(ctx)
	if id == "" {
		id = types.CallerFromContext(ctx).UserID
	}
	if id == "" || types.IsSyntheticUserID(id) {
		return "", access.ErrForbidden
	}
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		return "", access.ErrForbidden
	}
	if p, ok := types.PrincipalFromContext(ctx); ok && p.Type != types.PrincipalWebUser {
		return "", access.ErrForbidden
	}
	var user types.User
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Select("id", "is_active", "is_system_admin").Where("id = ?", id).First(&user).Error; err != nil {
		return "", access.ErrForbidden
	}
	if !user.IsActive || !user.IsSystemAdmin {
		return "", access.ErrForbidden
	}
	return id, nil
}

// All directory mutations share one root lock; concurrent moves cannot create cycles or orphan leaders.
func (r *CorporateDirectoryRepository) mutate(ctx context.Context, fn func(*gorm.DB, string) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var root types.CorporateDirectory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&root, "id = ?", 1).Error; err != nil {
			return err
		}
		actor, err := directoryActor(ctx, tx)
		if err != nil {
			return err
		}
		if err = fn(tx, actor); err != nil {
			return err
		}
		return tx.Model(&root).Update("version", gorm.Expr("version + 1")).Error
	})
}

func directoryAudit(tx *gorm.DB, actor, action, target string, details map[string]any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return tx.Create(&types.AuditLog{
		TenantID: 0, ActorUserID: actor, ActorRole: "system_admin", Action: types.AuditAction(action),
		ScopeType: "system", ScopeID: "corporate_directory", TargetType: "department", TargetID: target,
		Outcome: types.AuditOutcomeSuccess, Details: types.JSON(data), CreatedAt: time.Now().UTC(),
	}).Error
}

func directoryDepartments(tx *gorm.DB) ([]types.Department, error) {
	rows := []types.Department{}
	err := tx.Where("directory_id = ?", 1).Order("sort_order ASC, name ASC, id ASC").Limit(2001).Find(&rows).Error
	if err == nil && len(rows) > 2000 {
		err = fmt.Errorf("department hierarchy exceeds supported limit")
	}
	return rows, err
}

// Tree returns the root, department hierarchy and direct member counts.
func (r *CorporateDirectoryRepository) Tree(
	ctx context.Context,
) (*types.CorporateDirectory, []types.Department, error) {
	var root types.CorporateDirectory
	var rows []types.Department
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := directoryActor(ctx, tx); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&root, "id = ?", 1).Error; err != nil {
			return err
		}
		var err error
		rows, err = directoryDepartments(tx)
		if err != nil {
			return err
		}
		type counter struct {
			DepartmentID string
			N            int64
		}
		var counts []counter
		if err := tx.Table("corporate_personnel p").Select("p.department_id, COUNT(*) AS n").
			Joins("JOIN users u ON u.id = p.user_id AND u.deleted_at IS NULL").Where("p.directory_id = ?", 1).
			Group("p.department_id").Scan(&counts).Error; err != nil {
			return err
		}
		byID := map[string]int64{}
		for _, c := range counts {
			byID[c.DepartmentID] = c.N
		}
		for i := range rows {
			rows[i].MemberCount = byID[rows[i].ID]
		}
		return nil
	})
	return &root, rows, err
}

func validateDepartment(input *types.DepartmentUpdate) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Code = strings.TrimSpace(input.Code)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 ||
		utf8.RuneCountInString(input.Code) > 64 || input.SortOrder < 0 || input.SortOrder > 1000000 {
		return ErrDirectoryInvalid
	}
	for _, s := range []string{input.Name, input.Code} {
		for _, c := range s {
			if c < 32 {
				return ErrDirectoryInvalid
			}
		}
	}
	for _, id := range []*string{input.ParentID, input.LeaderID} {
		if id != nil && (*id == "" || len(*id) > 36) {
			return ErrDirectoryInvalid
		}
	}
	return nil
}

// SaveDepartment atomically creates or version-updates a department.
func (r *CorporateDirectoryRepository) SaveDepartment(
	ctx context.Context, id string, input types.DepartmentUpdate,
) (*types.Department, error) {
	if err := validateDepartment(&input); err != nil {
		return nil, err
	}
	var saved types.Department
	err := r.mutate(ctx, func(tx *gorm.DB, actor string) error {
		rows, err := directoryDepartments(tx)
		if err != nil {
			return err
		}
		byID := map[string]types.Department{}
		for _, row := range rows {
			byID[row.ID] = row
		}
		if id == "" {
			if len(rows) >= 2000 {
				return ErrDirectoryInvalid
			}
			saved = types.Department{ID: uuid.NewString(), DirectoryID: 1, Version: 1, CreatedAt: time.Now().UTC()}
			if input.ExpectedVersion != 0 || input.LeaderID != nil {
				return ErrDirectoryInvalid
			}
		} else {
			var ok bool
			saved, ok = byID[id]
			if !ok {
				return gorm.ErrRecordNotFound
			}
			if saved.Version != input.ExpectedVersion {
				return ErrDirectoryConflict
			}
			saved.Version++
		}
		// Codes are server owned. Preserve legacy codes, fill previously empty codes
		// on the next explicit edit, and never accept a client replacement.
		if saved.Code == "" {
			saved.Code = "DEP-" + strings.ToUpper(strings.ReplaceAll(saved.ID, "-", ""))
		}
		if input.Code != "" && input.Code != saved.Code {
			return ErrDirectoryInvalid
		}
		if input.ParentID != nil {
			seen := map[string]bool{saved.ID: true}
			parent := input.ParentID
			for parent != nil {
				if seen[*parent] {
					return ErrDirectoryInvalid
				}
				seen[*parent] = true
				p, ok := byID[*parent]
				if !ok {
					return ErrDirectoryInvalid
				}
				parent = p.ParentID
				if len(seen) > 33 {
					return ErrDirectoryInvalid
				}
			}
		}
		parentMap := make(map[string]*string, len(rows)+1)
		for _, row := range rows {
			parentMap[row.ID] = row.ParentID
		}
		parentMap[saved.ID] = input.ParentID
		for node := range parentMap {
			seen := map[string]bool{}
			next := &node
			for next != nil {
				if seen[*next] || len(seen) >= 32 {
					return ErrDirectoryInvalid
				}
				seen[*next] = true
				next = parentMap[*next]
			}
		}
		for _, row := range rows {
			if row.ID == saved.ID {
				continue
			}
			sameParent := equalDirectoryID(row.ParentID, input.ParentID)
			if (sameParent && strings.EqualFold(row.Name, input.Name)) ||
				strings.EqualFold(row.Code, saved.Code) {
				return ErrDirectoryConflict
			}
		}
		if input.LeaderID != nil {
			var leader types.User
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").
				Where("id = ? AND is_active = ?", *input.LeaderID, true).First(&leader).Error; err != nil {
				return ErrDepartmentLeader
			}
			var n int64
			if err := tx.Table("corporate_personnel p").
				Joins("JOIN users u ON u.id=p.user_id AND u.deleted_at IS NULL AND u.is_active = ?", true).
				Where("p.user_id = ? AND p.department_id = ? AND p.directory_id = ?",
					*input.LeaderID, saved.ID, 1).Count(&n).Error; err != nil {
				return err
			}
			if n != 1 {
				return ErrDepartmentLeader
			}
		}
		saved.Name = input.Name
		saved.ParentID = input.ParentID
		saved.LeaderID = input.LeaderID
		saved.SortOrder = input.SortOrder
		saved.UpdatedAt = time.Now().UTC()
		action := "directory.department_created"
		if id != "" {
			action = "directory.department_updated"
		}
		if id == "" {
			err = tx.Create(&saved).Error
		} else {
			result := tx.Model(&types.Department{}).Where("id = ? AND version = ?", id, input.ExpectedVersion).
				Select("name", "code", "parent_id", "leader_id", "sort_order", "version", "updated_at").Updates(&saved)
			err = result.Error
			if err == nil && result.RowsAffected != 1 {
				return ErrDirectoryConflict
			}
		}
		if err != nil {
			return err
		}
		return directoryAudit(tx, actor, action, saved.ID,
			map[string]any{"version": saved.Version, "parent_id": saved.ParentID})
	})
	return &saved, err
}

// DeleteDepartment removes only an empty department at the expected version.
func (r *CorporateDirectoryRepository) DeleteDepartment(ctx context.Context, id string, version uint64) error {
	return r.mutate(ctx, func(tx *gorm.DB, actor string) error {
		var row types.Department
		if err := tx.First(&row, "id = ? AND directory_id = ?", id, 1).Error; err != nil {
			return err
		}
		if row.Version != version {
			return ErrDirectoryConflict
		}
		var n int64
		if err := tx.Model(&types.Department{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 || row.LeaderID != nil {
			return ErrDepartmentOccupied
		}
		// Include disabled and soft-deleted users; do not silently change historical assignment.
		if err := tx.Model(&types.PersonnelProfile{}).Where("department_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			return ErrDepartmentOccupied
		}
		result := tx.Where("version = ?", version).Delete(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDirectoryConflict
		}
		return directoryAudit(tx, actor, "directory.department_deleted", id, map[string]any{"version": version})
	})
}

// Members filters before counting and paging minimal personnel projections.
func (r *CorporateDirectoryRepository) Members(
	ctx context.Context, q types.PersonnelQuery,
) ([]types.PersonnelView, int64, error) {
	if q.Offset < 0 || q.Offset > 1000000 || q.Limit < 1 || q.Limit > 100 || len(q.Keyword) > 200 ||
		(q.Status != "" && q.Status != "active" && q.Status != "inactive") {
		return nil, 0, ErrDirectoryInvalid
	}
	rows := []types.PersonnelView{}
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := directoryActor(ctx, tx); err != nil {
			return err
		}
		var root types.CorporateDirectory
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&root, "id = ?", 1).Error; err != nil {
			return err
		}
		query := tx.Table("users u").Joins("LEFT JOIN corporate_personnel p ON p.user_id=u.id AND p.directory_id=1").
			Joins("LEFT JOIN corporate_departments d ON d.id=p.department_id").Where("u.deleted_at IS NULL")
		if q.DepartmentID == "unassigned" {
			query = query.Where("p.department_id IS NULL")
		} else if q.DepartmentID != "" {
			deps, err := directoryDepartments(tx)
			if err != nil {
				return err
			}
			ids := []string{q.DepartmentID}
			found := false
			for _, d := range deps {
				if d.ID == q.DepartmentID {
					found = true
				}
			}
			if !found {
				return gorm.ErrRecordNotFound
			}
			if q.IncludeDescendants {
				seen := map[string]bool{q.DepartmentID: true}
				for changed := true; changed; {
					changed = false
					for _, d := range deps {
						if d.ParentID != nil && seen[*d.ParentID] && !seen[d.ID] {
							seen[d.ID] = true
							ids = append(ids, d.ID)
							changed = true
						}
					}
				}
			}
			query = query.Where("p.department_id IN ?", ids)
		}
		if q.Status != "" {
			query = query.Where("u.is_active = ?", q.Status == "active")
		}
		if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
			keyword = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(keyword))
			pattern := "%" + keyword + "%"
			query = query.Where("(LOWER(u.username) LIKE ? ESCAPE '!' OR "+
				"LOWER(u.email) LIKE ? ESCAPE '!' OR LOWER(p.display_name) LIKE ? ESCAPE '!')",
				pattern, pattern, pattern)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select("u.id,u.username,u.email,u.is_active,u.is_system_admin,u.tenant_id,u.created_at," +
			" COALESCE(p.display_name,'') AS display_name,p.department_id," +
			"COALESCE(d.name,'') AS department_name,COALESCE(p.version,0) AS version").
			Order("u.created_at DESC,u.id ASC").Offset(q.Offset).Limit(q.Limit).Scan(&rows).Error
	})
	return rows, total, err
}

// MoveMembers atomically updates a bounded versioned batch without resource grants.
func (r *CorporateDirectoryRepository) MoveMembers(
	ctx context.Context, departmentID *string, moves []types.PersonnelMove,
) error {
	if len(moves) == 0 || len(moves) > 100 || departmentID != nil && (*departmentID == "" || len(*departmentID) > 36) {
		return ErrDirectoryInvalid
	}
	moves = append([]types.PersonnelMove(nil), moves...)
	sort.Slice(moves, func(i, j int) bool { return moves[i].UserID < moves[j].UserID })
	for i, m := range moves {
		if m.UserID == "" || len(m.UserID) > 36 || i > 0 && moves[i-1].UserID == m.UserID {
			return ErrDirectoryInvalid
		}
	}
	return r.mutate(ctx, func(tx *gorm.DB, actor string) error {
		if departmentID != nil {
			var d types.Department
			if err := tx.First(&d, "id = ? AND directory_id = ?", *departmentID, 1).Error; err != nil {
				return err
			}
		}
		for _, m := range moves {
			var u types.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
				First(&u, "id = ?", m.UserID).Error; err != nil {
				return err
			}
			var p types.PersonnelProfile
			err := tx.First(&p, "user_id = ?", m.UserID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if p.Version != m.ExpectedVersion {
				return ErrDirectoryConflict
			}
			var leaders int64
			check := tx.Model(&types.Department{}).Where("leader_id = ?", m.UserID)
			if departmentID != nil {
				check = check.Where("id <> ?", *departmentID)
			}
			if err := check.Count(&leaders).Error; err != nil {
				return err
			}
			if leaders != 0 {
				return ErrDepartmentLeader
			}
			p.UserID = m.UserID
			p.DirectoryID = 1
			p.DepartmentID = departmentID
			p.Version++
			p.UpdatedAt = time.Now().UTC()
			if err := tx.Clauses(personnelConflict("department_id", "version", "updated_at")).
				Create(&p).Error; err != nil {
				return err
			}
		}
		return directoryAudit(tx, actor, "directory.members_moved", "",
			map[string]any{"department_id": departmentID, "count": len(moves)})
	})
}

// UpdatePersonnel changes display metadata only; it cannot edit login identity or resource roles.
func (r *CorporateDirectoryRepository) UpdatePersonnel(ctx context.Context, id, name string, version uint64) error {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 100 {
		return ErrDirectoryInvalid
	}
	for _, c := range name {
		if c < 32 {
			return ErrDirectoryInvalid
		}
	}
	return r.mutate(ctx, func(tx *gorm.DB, actor string) error {
		var user types.User
		if err := tx.Select("id").First(&user, "id = ?", id).Error; err != nil {
			return err
		}
		var p types.PersonnelProfile
		err := tx.First(&p, "user_id = ?", id).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if p.Version != version {
			return ErrDirectoryConflict
		}
		p.UserID = id
		p.DirectoryID = 1
		p.DisplayName = name
		p.Version++
		p.UpdatedAt = time.Now().UTC()
		if err := tx.Clauses(personnelConflict("display_name", "version", "updated_at")).Create(&p).Error; err != nil {
			return err
		}
		return directoryAudit(tx, actor, "directory.personnel_updated", id, map[string]any{"version": p.Version})
	})
}

// CreateUserWithDirectory commits account, workspace membership and department together.
func (r *userRepository) CreateUserWithDirectory(
	ctx context.Context, user *types.User, role types.TenantRole, actor string, profile types.DirectoryProfileInput,
) error {
	if strings.ContainsAny(profile.DisplayName, "\n\r\t") || utf8.RuneCountInString(profile.DisplayName) > 100 ||
		profile.DepartmentID != nil && (*profile.DepartmentID == "" || len(*profile.DepartmentID) > 36) {
		return ErrDirectoryInvalid
	}
	return (&CorporateDirectoryRepository{db: r.db}).mutate(ctx, func(tx *gorm.DB, actorID string) error {
		if profile.DepartmentID != nil {
			var d types.Department
			if err := tx.First(&d, "id = ? AND directory_id = ?", *profile.DepartmentID, 1).Error; err != nil {
				return ErrDirectoryInvalid
			}
		}
		if err := (&userRepository{db: tx}).CreateUserWithMembership(ctx, user, role, actor); err != nil {
			return err
		}
		personnel := &types.PersonnelProfile{
			UserID: user.ID, DirectoryID: 1, DepartmentID: profile.DepartmentID,
			DisplayName: strings.TrimSpace(profile.DisplayName), Version: 1, UpdatedAt: time.Now().UTC(),
		}
		if err := tx.Create(personnel).Error; err != nil {
			return err
		}
		return directoryAudit(tx, actorID, "directory.personnel_created", user.ID,
			map[string]any{"department_id": profile.DepartmentID})
	})
}

func (r *userRepository) MatchUserDirectory(
	ctx context.Context, id string, input types.DirectoryProfileInput,
) (bool, error) {
	var p types.PersonnelProfile
	if err := r.db.WithContext(ctx).First(&p, "user_id = ?", id).Error; err != nil {
		return false, err
	}
	same := equalDirectoryID(p.DepartmentID, input.DepartmentID)
	return same && p.DisplayName == strings.TrimSpace(input.DisplayName), nil
}

func equalDirectoryID(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func personnelConflict(columns ...string) clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns(columns)}
}
