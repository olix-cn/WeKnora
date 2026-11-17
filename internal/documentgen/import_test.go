package documentgen

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func importedSteps(t *testing.T) []types.AgentStep {
	t.Helper()
	meta := `"result_id":"snapshot","source_kind":"saved_file","expires_at":1000,`
	schema := `{` + meta + `"mode":"schema","page":{"total":3,"offset":0,"returned":3,"has_more":false},
 "records":[{"kind":"file","file_id":"f","source":"synthetic.dwg"},
 {"kind":"table","table_id":"t","file_id":"f","layout":"Model","handle":"H",
 "schema_complete":true,"physical_columns_enumerated":true,"row_count_known":true,"row_count":1},
 {"kind":"column","table_id":"t","column_id":"c","position":1,"raw":"图号","state":"confirmed"}]}`
	data := `{` + meta + `"mode":"data","status":"complete","complete":true,"row_count":1,"row_count_complete":true,
 "selection":{"complete":true,"scope_covers_all_files":true},
 "page":{"total":1,"offset":0,"returned":1,"has_more":false},
 "rows":[{"row_id":"r","table_id":"t","values":{"c":"0001"},"states":{}}]}`
	steps := []types.AgentStep{}
	for _, raw := range []string{schema, data} {
		var value map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &value))
		steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{{
			Args: map[string]any{"columns_mode": "all"},
			Name: "mcp_cad_assistant_drawing_bom_extract_123", Result: &types.ToolResult{
				Success: true, Data: map[string]any{"structured_content": value},
			},
		}}})
	}
	return steps
}

func TestImportBOMPreservesTextAndRejectsPageGaps(t *testing.T) {
	steps := importedSteps(t)
	data, err := ImportBOM(steps, "snapshot")
	require.NoError(t, err)
	require.Equal(t, "0001", data.Rows[0].Values[0].Value)
	require.False(t, data.Partial)
	_, err = ImportBOM(steps[:1], "snapshot")
	require.Error(t, err)
	_, err = ImportBOM(steps, "other-snapshot")
	require.Error(t, err)
	steps[0].ToolCalls[0].Name = "shell_exec"
	_, err = ImportBOM(steps, "snapshot")
	require.Error(t, err, "a model/script must not impersonate persisted MCP output")
}

func TestImportBOMRejectsMixedSnapshotsAndMissingPhysicalColumns(t *testing.T) {
	for _, modify := range []func(map[string]any){
		func(m map[string]any) { m["expires_at"] = float64(2000) },
		func(m map[string]any) { m["selection"] = map[string]any{"complete": false} },
		func(m map[string]any) { m["page"].(map[string]any)["returned"] = float64(2) },
		func(m map[string]any) { m["rows"].([]any)[0].(map[string]any)["values"] = map[string]any{} },
		func(m map[string]any) { m["rows"].([]any)[0].(map[string]any)["values"] = map[string]any{"c": nil} },
	} {
		steps := importedSteps(t)
		modify(steps[1].ToolCalls[0].Result.Data["structured_content"].(map[string]any))
		_, err := ImportBOM(steps, "snapshot")
		require.Error(t, err)
	}
}

func TestImportBOMPartialStates(t *testing.T) {
	steps := importedSteps(t)
	m := steps[1].ToolCalls[0].Result.Data["structured_content"].(map[string]any)
	m["status"] = "partial"
	m["complete"] = false
	row := m["rows"].([]any)[0].(map[string]any)
	row["values"] = map[string]any{"c": nil}
	row["states"] = map[string]any{"c": "not_detected"}
	data, err := ImportBOM(steps, "snapshot")
	require.NoError(t, err)
	require.True(t, data.Partial)
	require.Equal(t, "not_detected", data.Rows[0].Values[0].State)
	require.NotEmpty(t, data.Issues)
}

func TestImportBOMRejectsSelectedColumnsAndIncompleteSchema(t *testing.T) {
	steps := importedSteps(t)
	steps[1].ToolCalls[0].Args["columns_mode"] = "selected"
	_, err := ImportBOM(steps, "snapshot")
	require.Error(t, err)
	steps = importedSteps(t)
	m := steps[0].ToolCalls[0].Result.Data["structured_content"].(map[string]any)
	m["records"].([]any)[1].(map[string]any)["schema_complete"] = false
	_, err = ImportBOM(steps, "snapshot")
	require.Error(t, err)
}

func TestImportBOMSupportsPagedSchemaAndRejectsBrokenCursor(t *testing.T) {
	steps := importedSteps(t)
	m := steps[0].ToolCalls[0].Result.Data["structured_content"].(map[string]any)
	records := m["records"].([]any)
	first := map[string]any{}
	for key, value := range m {
		first[key] = value
	}
	first["records"] = records[:1]
	first["page"] = map[string]any{
		"total": float64(3), "offset": float64(0),
		"returned": float64(1), "has_more": true, "next_offset": float64(1),
	}
	m["records"] = records[1:]
	m["page"] = map[string]any{
		"total": float64(3), "offset": float64(1),
		"returned": float64(2), "has_more": false,
	}
	call := types.ToolCall{
		Name:   "mcp_cad_assistant_drawing_bom_extract_123",
		Args:   map[string]any{"columns_mode": "all"},
		Result: &types.ToolResult{Success: true, Data: map[string]any{"structured_content": first}},
	}
	steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{call}})
	_, err := ImportBOM(steps, "snapshot")
	require.NoError(t, err)
	first["page"].(map[string]any)["next_offset"] = float64(2)
	_, err = ImportBOM(steps, "snapshot")
	require.Error(t, err)
}
