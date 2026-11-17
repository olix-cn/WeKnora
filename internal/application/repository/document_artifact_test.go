package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDocumentGenerationsShareArtifactListingWithoutFakeMessages(t *testing.T) {
	db := newArtifactTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.DocumentDataset{}, &types.DocumentGeneration{}))
	now := time.Now().UTC()
	sourceID := createSession(t, db, &types.Session{TenantID: 7, UserID: "owner"})
	message, err := NewMessageRepository(db).CreateMessage(context.Background(), &types.Message{
		SessionID: sourceID, Role: "assistant", IsCompleted: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&types.DocumentDataset{
		ID: "data", TenantID: 7, UserID: "owner",
		SessionID: sourceID, MessageID: message.ID, ExpiresAt: now.Add(time.Hour),
	}).Error)
	for _, row := range []types.DocumentGeneration{
		{ID: "output1", TenantID: 7, UserID: "owner", DatasetID: "data", FileName: "same.xlsx", Status: "completed"},
		{ID: "output2", TenantID: 7, UserID: "owner", DatasetID: "data", FileName: "same.xlsx", Status: "completed"},
		{
			ID: "other-user", TenantID: 7, UserID: "colleague", DatasetID: "data",
			FileName: "secret.xlsx", Status: "completed",
		},
		{ID: "deleted", TenantID: 7, UserID: "owner", DatasetID: "data", Status: "file_deleted"},
	} {
		row.ExpiresAt = now.Add(time.Hour)
		row.CreatedAt = now
		require.NoError(t, db.Create(&row).Error)
	}
	repo := NewMessageRepository(db)
	q := &types.ArtifactLibraryQuery{TenantID: 7, UserID: "owner", Page: 1, PageSize: 30}
	rows, total, err := repo.ListArtifactLibrary(context.Background(), q)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.NotEmpty(t, row.TaskID)
		require.Empty(t, row.SessionID)
		require.Empty(t, row.MessageID)
	}
	q.FileTypes = []string{".pdf"}
	_, total, err = repo.ListArtifactLibrary(context.Background(), q)
	require.NoError(t, err)
	require.Zero(t, total)
	q.FileTypes = nil
	q.Keyword = "same"
	_, total, err = repo.ListArtifactLibrary(context.Background(), q)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.NoError(t, db.Where("id = ?", sourceID).Delete(&types.Session{}).Error)
	_, total, err = repo.ListArtifactLibrary(context.Background(), q)
	require.NoError(t, err)
	require.Zero(t, total)
}
