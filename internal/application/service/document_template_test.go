package service

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func templateAdmin(user string, tenant uint64) context.Context {
	return types.WithCaller(context.Background(), types.Caller{
		UserID: user, TenantID: tenant, Role: types.TenantRoleAdmin,
	})
}

func templateWorkbook(t *testing.T) ([]byte, documentgen.TemplateLayout) {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	require.NoError(t, f.SetCellStr("Sheet1", "A1", "图号"))
	b, err := f.WriteToBuffer()
	require.NoError(t, err)
	return b.Bytes(), documentgen.TemplateLayout{
		Sheet: "Sheet1", HeaderRow: 1, StartRow: 2,
		Fields: []documentgen.TemplateField{{Key: "part", Label: "图号", Column: "A"}},
	}
}

func TestDocumentTemplateLifecycleAndAuthorizedGeneration(t *testing.T) {
	s, files, _, db := generationFixture(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/000054_document_templates.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	ctx := templateAdmin("owner", 7)
	viewer := generationContext("owner", 7)
	payload, layout := templateWorkbook(t)
	row, _, err := s.UploadTemplate(ctx, "合成模板", "", payload)
	require.NoError(t, err)
	inspection, err := s.InspectManagedTemplate(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, inspection.HeaderCandidates, 1)
	require.Equal(t, "图号", inspection.HeaderCandidates[0].Fields[0].Label)
	_, _, err = s.UploadTemplate(viewer, "denied", "", payload)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	for _, denied := range []context.Context{viewer, templateAdmin("other", 7), templateAdmin("owner", 8)} {
		_, err = s.InspectManagedTemplate(denied, row.ID)
		require.ErrorIs(t, err, ErrDocumentUnavailable)
		require.ErrorIs(t, s.ConfigureTemplate(denied, row.ID, layout), ErrDocumentUnavailable)
		_, err = s.TemplateLayout(denied, row.ID)
		require.Error(t, err)
	}
	listed, err := s.TemplateList(viewer)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.ErrorIs(t, s.PublishTemplate(ctx, row.ID, true), ErrDocumentConflict)
	require.NoError(t, s.ConfigureTemplate(ctx, row.ID, layout))
	output, err := s.TestTemplate(ctx, row.ID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(output))
	require.NoError(t, err)
	value, err := f.GetCellValue("Sheet1", "A31")
	require.NoError(t, err)
	require.Contains(t, value, "样例30")
	_ = f.Close()
	// Even an identical save invalidates the test, so stale approval cannot publish it.
	require.NoError(t, s.ConfigureTemplate(ctx, row.ID, layout))
	require.ErrorIs(t, s.PublishTemplate(ctx, row.ID, true), ErrDocumentConflict)
	_, err = s.TestTemplate(ctx, row.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.PublishTemplate(ctx, row.ID, false), ErrDocumentInvalid)
	require.NoError(t, s.PublishTemplate(ctx, row.ID, true))
	require.ErrorIs(t, s.ConfigureTemplate(ctx, row.ID, layout), ErrDocumentConflict)
	listed, err = s.TemplateList(viewer)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.False(t, listed[0].CanManage)
	d, err := s.Import(viewer, "session", "message", "snapshot")
	require.NoError(t, err)
	_, err = s.GenerateWithBindings(viewer, d.ID, row.ID, "bad-bindings", true, map[string]string{"part": "absent"})
	require.ErrorIs(t, err, ErrDocumentInvalid)
	var count int64
	require.NoError(t, db.Model(&types.DocumentGeneration{}).Count(&count).Error)
	require.Zero(t, count)
	bindings := map[string]string{"part": d.Fields[0].ID}
	job, err := s.GenerateWithBindings(viewer, d.ID, row.ID, "bad-bindings", true, bindings)
	require.NoError(t, err)
	require.Equal(t, "completed", job.Status)
	require.Equal(t, 2, files.saves)
	newVersion, _, err := s.UploadTemplate(ctx, "新版本", row.ID, payload)
	require.NoError(t, err)
	require.Equal(t, row.FamilyID, newVersion.FamilyID)
	require.Equal(t, 2, newVersion.Version)
	third, _, err := s.UploadTemplate(ctx, "另一版本", row.ID, payload)
	require.NoError(t, err)
	require.Equal(t, 3, third.Version)
	require.NoError(t, s.DisableTemplate(ctx, row.ID))
	_, err = s.repo.CreateTemplateTask(ctx, &types.DocumentGeneration{
		ID: "late-task", TenantID: 7, UserID: "owner", TemplateID: row.ID,
	})
	require.Error(t, err)
	_, err = s.GenerateWithBindings(viewer, d.ID, row.ID, "disabled-new", true, bindings)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	again, err := s.GenerateWithBindings(viewer, d.ID, row.ID, "bad-bindings", true, bindings)
	require.NoError(t, err)
	require.Equal(t, job.ID, again.ID)
	_, err = s.GenerateWithBindings(viewer, d.ID, row.ID, "bad-bindings", true, map[string]string{"part": "changed"})
	require.ErrorIs(t, err, ErrDocumentConflict)
	r, _, err := s.Download(viewer, job.ID)
	require.NoError(t, err)
	_ = r.Close()
}

func TestDocumentTemplateOptimisticTransitionRejectsStaleConfiguration(t *testing.T) {
	s, _, _, db := generationFixture(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/000054_document_templates.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	ctx := templateAdmin("owner", 7)
	payload, layout := templateWorkbook(t)
	row, _, err := s.UploadTemplate(ctx, "test", "", payload)
	require.NoError(t, err)
	require.NoError(t, s.ConfigureTemplate(ctx, row.ID, layout))
	stale, err := s.repo.Template(ctx, 7, row.ID)
	require.NoError(t, err)
	require.NoError(t, s.ConfigureTemplate(ctx, row.ID, layout))
	stale.Status = "tested"
	ok, err := s.repo.ChangeTemplate(ctx, stale, "configured", stale.TestHash)
	require.NoError(t, err)
	require.False(t, ok)
	// The storage integrity hash is rechecked before testing a configured draft.
	current, err := s.repo.Template(ctx, 7, row.ID)
	require.NoError(t, err)
	s.files.(*documentStorage).data[current.FileRef] = []byte("tampered")
	_, err = s.TestTemplate(ctx, row.ID)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
}
