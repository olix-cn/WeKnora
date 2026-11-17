package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDocumentToolsReceiveEngineEvidenceNotModelCells(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	engine.toolRegistry = tools.NewToolRegistry()
	want := []types.AgentStep{{Iteration: 0, ToolCalls: []types.ToolCall{{Name: "synthetic_mcp"}}}}
	var got tools.DocumentTurn
	engine.toolRegistry.RegisterTool(tools.NewGenerateEBOMTool(func(
		ctx context.Context, _ json.RawMessage,
	) (*types.ToolResult, error) {
		var ok bool
		got, ok = tools.DocumentTurnFromContext(ctx)
		require.True(t, ok)
		return &types.ToolResult{Success: true}, nil
	}))
	state := &types.AgentState{RoundSteps: want, CurrentRound: 1}
	response := &types.ChatResponse{ToolCalls: []types.LLMToolCall{{ID: "generate", Function: types.FunctionCall{
		Name: tools.ToolGenerateEBOM, Arguments: `{"template_id":"template","result_id":"snapshot",
"production_count":2,"one_drawing_set":true,"quantity_per_parent":true}`,
	}}}}
	require.False(t, engine.runRoundTools(t.Context(), state, response, &types.AgentStep{}, "session", "real-message"))
	require.Equal(t, "real-message", got.MessageID)
	require.Equal(t, want, got.Steps)
	require.Contains(t, engine.buildSystemPrompt(t.Context()), "takes precedence over the xlsx skill")
}
