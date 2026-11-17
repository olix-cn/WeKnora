package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	filesvc "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type documentSessions struct {
	interfaces.SessionService
	allowed bool
}

func (s *documentSessions) GetOwnedSession(ctx context.Context, id string) (*types.Session, error) {
	c := types.CallerFromContext(ctx)
	if !s.allowed || id != "session" || c.UserID != "owner" || c.TenantID != 7 {
		return nil, errors.New("denied")
	}
	return &types.Session{ID: id, TenantID: 7, UserID: "owner"}, nil
}

type documentMessages struct {
	interfaces.MessageService
	message *types.Message
}

func (s *documentMessages) GetMessage(context.Context, string, string) (*types.Message, error) {
	return s.message, nil
}

func (s *documentMessages) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return []*types.Message{s.message}, nil
}

type documentStorage struct {
	interfaces.FileService
	interfaces.ResourceCatalog
	mu       sync.Mutex
	data     map[string][]byte
	saves    int
	failSave bool
}

func (s *documentStorage) SaveBytes(_ context.Context, b []byte, _ uint64, name string, _ bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSave {
		return "", errors.New("offline")
	}
	s.saves++
	ref := "resource://" + name
	s.data[ref] = append([]byte{}, b...)
	return ref, nil
}

func (s *documentStorage) GetFile(_ context.Context, ref string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[ref]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *documentStorage) DeleteFile(_ context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, ref)
	return nil
}
func (s *documentStorage) Bind(context.Context, string, string, string, string) error { return nil }
func (s *documentStorage) Release(context.Context, string, string, string) (int64, error) {
	return 0, nil
}

func (s *documentStorage) Resolve(_ context.Context, ref string) (*types.StoredResource, error) {
	return &types.StoredResource{TenantID: 7, PhysicalPath: ref}, nil
}

func generationFixture(t *testing.T) (*DocumentGenerationService, *documentStorage, *documentSessions, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "documents.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Execute only this feature's schema in an isolated temporary database.
	sql, err := os.ReadFile("../../../migrations/sqlite/000053_document_generation.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	meta := `"result_id":"snapshot","source_kind":"saved_file","expires_at":1000,`
	schema := `{` + meta + `"mode":"schema","page":{"total":3,"offset":0,"returned":3,"has_more":false},
 "records":[{"kind":"file","file_id":"f","source":"synthetic.dwg"},
 {"kind":"table","table_id":"t","file_id":"f","layout":"Model","handle":"H",
 "schema_complete":true,"physical_columns_enumerated":true,"row_count_known":true,"row_count":1},
 {"kind":"column","table_id":"t","column_id":"c","position":1,"raw":"图号","state":"confirmed"}]}`
	data := `{` + meta + `"mode":"data","status":"partial","complete":false,"row_count":1,"row_count_complete":true,
 "selection":{"complete":true,"scope_covers_all_files":true},
 "page":{"total":1,"offset":0,"returned":1,"has_more":false},
 "rows":[{"row_id":"r","table_id":"t","values":{"c":null},"states":{"c":"not_detected"}}]}`
	message := &types.Message{ID: "message", SessionID: "session", Role: "assistant", IsCompleted: true}
	for _, raw := range []string{schema, data} {
		var value map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &value))
		message.AgentSteps = append(message.AgentSteps, types.AgentStep{ToolCalls: []types.ToolCall{{
			Args:   map[string]any{"columns_mode": "all"},
			Name:   "mcp_cad_assistant_drawing_bom_extract_123",
			Result: &types.ToolResult{Success: true, Data: map[string]any{"structured_content": value}},
		}}})
	}
	sessions := &documentSessions{allowed: true}
	storage := &documentStorage{data: map[string][]byte{}}
	svc := NewDocumentGenerationService(repository.NewDocumentGenerationRepository(db), sessions,
		&documentMessages{message: message}, storage, storage, nil, nil)
	return svc, storage, sessions, db
}

func generationContext(user string, tenant uint64) context.Context {
	return types.WithCaller(context.Background(), types.Caller{
		UserID: user, TenantID: tenant, Role: types.TenantRoleViewer,
	})
}

func TestDocumentGenerationImportGenerateDownloadDelete(t *testing.T) {
	s, files, sessions, _ := generationFixture(t)
	ctx := generationContext("owner", 7)
	dataset, err := s.Import(ctx, "session", "message", "snapshot")
	require.NoError(t, err)
	require.True(t, dataset.Partial)
	require.Equal(t, 1, dataset.RowCount)
	_, err = s.Generate(ctx, dataset.ID, "engineering_bom_v1", "request-key", false)
	require.ErrorIs(t, err, ErrDocumentInvalid)
	job, err := s.Generate(ctx, dataset.ID, "engineering_bom_v1", "request-key", true)
	require.NoError(t, err)
	require.Equal(t, "completed", job.Status)
	require.Equal(t, 1, files.saves)
	again, err := s.Generate(ctx, dataset.ID, "engineering_bom_v1", "request-key", true)
	require.NoError(t, err)
	require.Equal(t, job.ID, again.ID)
	require.Equal(t, 1, files.saves)
	reader, stored, err := s.Download(ctx, job.ID)
	require.NoError(t, err)
	b, err := io.ReadAll(reader)
	require.NoError(t, err)
	_ = reader.Close()
	require.Equal(t, int64(len(b)), stored.FileSize)
	_, _, err = s.Download(generationContext("colleague", 7), job.ID)
	require.Error(t, err)
	_, _, err = s.Download(generationContext("owner", 8), job.ID)
	require.Error(t, err)
	sessions.allowed = false
	_, _, err = s.Download(ctx, job.ID)
	require.Error(t, err)
	sessions.allowed = true
	require.NoError(t, s.Delete(ctx, job.ID))
	_, _, err = s.Download(ctx, job.ID)
	require.Error(t, err)
	rows, total, err := s.List(ctx, 1, 30)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "file_deleted", rows[0].Status)
}

func TestDocumentGenerationFailureAndRequestKeyConflict(t *testing.T) {
	s, files, _, db := generationFixture(t)
	ctx := generationContext("owner", 7)
	d, err := s.Import(ctx, "session", "message", "snapshot")
	require.NoError(t, err)
	files.failSave = true
	job, err := s.Generate(ctx, d.ID, "engineering_bom_v1", "same-request", true)
	require.NoError(t, err)
	require.Equal(t, "failed", job.Status)
	require.Equal(t, "storage_failed", job.ErrorCode)
	files.failSave = false
	again, err := s.Generate(ctx, d.ID, "engineering_bom_v1", "same-request", true)
	require.NoError(t, err)
	require.Equal(t, job.ID, again.ID)
	require.Equal(t, 0, files.saves)
	d2, err := s.Import(ctx, "session", "message", "snapshot")
	require.NoError(t, err)
	_, err = s.Generate(ctx, d2.ID, "engineering_bom_v1", "same-request", true)
	require.ErrorIs(t, err, ErrDocumentConflict)
	require.NoError(t, db.Model(&types.DocumentDataset{}).Where("id = ?", d.ID).
		Update("expires_at", time.Now().Add(-time.Hour)).Error)
	_, err = s.Generate(ctx, d.ID, "engineering_bom_v1", "fresh-request", true)
	require.Error(t, err)
}

func TestDocumentGenerationRejectsInaccessibleAndIncompleteSources(t *testing.T) {
	s, _, _, _ := generationFixture(t)
	for _, ctx := range []context.Context{
		context.Background(), generationContext("other", 7), generationContext("owner", 8),
		types.WithTenantAPIKeyScope(generationContext("owner", 7), types.TenantAPIKeyScope{FullAccess: true}),
	} {
		_, err := s.Import(ctx, "session", "message", "snapshot")
		require.Error(t, err)
	}
	s.messages.(*documentMessages).message.AgentSteps = s.messages.(*documentMessages).message.AgentSteps[:1]
	_, err := s.Import(generationContext("owner", 7), "session", "message", "snapshot")
	require.ErrorIs(t, err, ErrDocumentInvalid)
}

func TestDocumentSourcesDiscoverOnlyValidatedImports(t *testing.T) {
	s, _, sessions, _ := generationFixture(t)
	ctx := generationContext("owner", 7)
	rows, err := s.Sources(ctx, "session")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "snapshot", rows[0].ResultID)
	require.True(t, rows[0].Partial)
	sessions.allowed = false
	_, err = s.Sources(ctx, "session")
	require.Error(t, err)
}

func TestDocumentGenerationDeduplicatesConcurrentRequests(t *testing.T) {
	s, files, _, _ := generationFixture(t)
	ctx := generationContext("owner", 7)
	d, err := s.Import(ctx, "session", "message", "snapshot")
	require.NoError(t, err)
	type response struct {
		id  string
		err error
	}
	responses := make(chan response, 4)
	for i := 0; i < 4; i++ {
		go func() {
			task, err := s.Generate(ctx, d.ID, "engineering_bom_v1", "concurrent-key", true)
			id := ""
			if task != nil {
				id = task.ID
			}
			responses <- response{id, err}
		}()
	}
	ids := map[string]bool{}
	for i := 0; i < 4; i++ {
		r := <-responses
		require.NoError(t, r.err)
		ids[r.id] = true
	}
	require.Len(t, ids, 1)
	files.mu.Lock()
	require.Equal(t, 1, files.saves)
	files.mu.Unlock()
}

func TestDocumentGenerationRejectsCorruptPersistedDataset(t *testing.T) {
	s, files, _, db := generationFixture(t)
	ctx := generationContext("owner", 7)
	d, err := s.Import(ctx, "session", "message", "snapshot")
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.DocumentDataset{}).Where("id = ?", d.ID).Update("payload", "{}").Error)
	job, err := s.Generate(ctx, d.ID, "engineering_bom_v1", "corrupt-request", true)
	require.NoError(t, err)
	require.Equal(t, "failed", job.Status)
	require.Equal(t, "dataset_invalid", job.ErrorCode)
	require.Zero(t, files.saves)
}

func TestDocumentGenerationUsesRealFileServiceAndResourceBinding(t *testing.T) {
	s, _, _, db := generationFixture(t)
	require.NoError(t, db.AutoMigrate(&types.StoredResource{}, &types.ResourceBinding{}, &types.ResourceAccessGrant{}))
	s.catalog = NewResourceCatalog(repository.NewResourceRepository(db))
	s.files = filesvc.NewResourceCatalogFileService(filesvc.NewLocalFileService(t.TempDir(), ""), s.catalog)
	ctx := generationContext("owner", 7)
	dataset, err := s.Import(ctx, "session", "message", "snapshot")
	require.NoError(t, err)
	task, err := s.Generate(ctx, dataset.ID, "engineering_bom_v1", "real-storage-request", true)
	require.NoError(t, err)
	require.Equal(t, "completed", task.Status)
	_, valid := types.ParseResourcePath(task.FileRef)
	require.True(t, valid)
	resource, err := s.catalog.Resolve(ctx, task.FileRef)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&types.ResourceBinding{}).
		Where("resource_id = ? AND owner_type = ? AND owner_id = ?", resource.ID, documentFileOwner, task.ID).
		Count(&count).Error)
	require.EqualValues(t, 1, count)
	reader, _, err := s.Download(ctx, task.ID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(reader)
	_ = reader.Close()
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	value, err := f.GetCellValue("BOM明细", "A2")
	require.NoError(t, err)
	require.Equal(t, "未检测", value)
	require.NoError(t, s.Delete(ctx, task.ID))
	_, err = s.catalog.Resolve(ctx, task.FileRef)
	require.Error(t, err, "resource is tombstoned after its last owner releases it")
}
