package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type restrictedTransferShares struct{ interfaces.KBShareService }

func (*restrictedTransferShares) CheckKBMemberAccess(
	context.Context, string, uint64, types.Caller,
) (types.OrgMemberRole, bool, error) {
	return types.OrgRoleAdmin, true, nil
}

func (*restrictedTransferShares) CheckKBTransferPolicy(context.Context, string, uint64) error {
	return access.ErrForbidden
}

func TestRestrictedSourceCannotBeClonedIntoDefaultPublicDestination(t *testing.T) {
	q := &documentDeleteEnqueuer{}
	h := &KnowledgeBaseHandler{
		cfg: transferHandlerConfig(), service: transferHandlerKB(),
		knowledgeService: &transferHandlerKnowledge{}, asynqClient: q, kbShareService: &restrictedTransferShares{},
	}
	r := transferHandlerRouter(nil)
	r.POST("/transfer", h.CopyKnowledgeBase)
	w := transferRequest(r, `{"source_id":"source"}`)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Nil(t, q.task, "read/admin access is not authorization to declassify the source")
}

func TestRestrictedSourceCannotBeMovedIntoDifferentAudience(t *testing.T) {
	q := &documentDeleteEnqueuer{}
	h := &KnowledgeHandler{
		cfg: transferHandlerConfig(), kbService: transferHandlerKB(), kgService: &transferHandlerKnowledge{},
		asynqClient: q, kbShareService: &restrictedTransferShares{},
	}
	r := transferHandlerRouter(nil)
	r.POST("/transfer", h.MoveKnowledge)
	w := transferRequest(r,
		`{"source_kb_id":"source","target_kb_id":"target","knowledge_ids":["doc"],"mode":"reuse_vectors"}`)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Nil(t, q.task)
}
