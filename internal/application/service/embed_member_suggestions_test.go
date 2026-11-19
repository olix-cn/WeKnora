package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEmbedKnowledgeStartersDoNotQueryRestrictedKB(t *testing.T) {
	agent := newCuratedStarterAgent(4, nil)
	agent.Config.QuestionSuggestions.Starters.Mode = types.SuggestionModeKnowledge
	agent.Config.KBSelectionMode = "selected"
	agent.Config.KnowledgeBases = []string{"denied"}
	s := &customAgentService{
		repo: &suggestionLimitAgentRepo{agent: agent},
		kbService: &suggestionKBService{kbs: map[string]*types.KnowledgeBase{
			"denied": {ID: "denied", TenantID: 1},
		}},
		kbShareService: &fakeMemberFilterShareService{memberFilterLookup: &memberFilterLookup{}},
	}
	embed := &embedChannelService{agentService: s}
	ctx := types.WithExecutionTenant(types.WithCaller(context.Background(), types.Caller{
		TenantID: 1, UserID: "embed-channel", Role: types.TenantRoleViewer,
	}), 1)
	// chunkRepo is deliberately nil: the denied KB must not reach any content
	// query, including curated/knowledge hybrid preparation.
	questions, err := embed.SuggestedQuestions(ctx,
		&types.EmbedChannel{AgentID: agent.ID, TenantID: 1, ShowSuggestedQuestions: true}, 4)
	require.NoError(t, err)
	require.Empty(t, questions)
}
