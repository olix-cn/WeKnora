package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMemberExclusionsApplyBeforeSessionAndArtifactCounts(t *testing.T) {
	db := newArtifactTestDB(t)
	ctx := context.Background()
	var denied string
	for _, title := range []string{"public", "private"} {
		id := createSession(t, db, &types.Session{TenantID: 7, UserID: "user", Title: title})
		_, err := NewMessageRepository(db).CreateMessage(ctx, &types.Message{
			SessionID: id, Role: "assistant", IsCompleted: true,
			Artifacts: types.MessageArtifacts{testArtifact(title+".pptx", "/workspace/"+title, time.Now())},
		})
		require.NoError(t, err)
		if title == "private" {
			denied = id
		}
	}
	rows, total, err := NewSessionRepository(db).QueryPaged(ctx, &types.SessionListQuery{
		TenantID: 7, UserID: "user", Page: 1, PageSize: 1, ExcludedSessionIDs: []string{denied},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	require.Equal(t, "public", rows[0].Title)
	files, total, err := NewMessageRepository(db).ListArtifactLibrary(ctx, &types.ArtifactLibraryQuery{
		TenantID: 7, UserID: "user", Page: 1, PageSize: 1, ExcludedSessionIDs: []string{denied},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, files, 1)
	require.Equal(t, "public.pptx", files[0].FileName)
}
