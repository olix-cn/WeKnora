package tools

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

// Document Center tools follow workspace capability and owned-chat scope.
const (
	ToolDocumentTemplates = "document_templates"
	ToolGenerateEBOM      = "generate_ebom"
)

type documentTurnKey struct{}

// DocumentTurn is engine-provided evidence, never model-supplied table values.
type DocumentTurn struct {
	MessageID string
	Steps     []types.AgentStep
}

// WithDocumentTurn binds a completed-round snapshot before the next tool batch.
func WithDocumentTurn(ctx context.Context, messageID string, steps []types.AgentStep) context.Context {
	return context.WithValue(ctx, documentTurnKey{}, DocumentTurn{MessageID: messageID, Steps: steps})
}

// DocumentTurnFromContext reads only the engine's current-turn snapshot.
func DocumentTurnFromContext(ctx context.Context) (DocumentTurn, bool) {
	turn, ok := ctx.Value(documentTurnKey{}).(DocumentTurn)
	return turn, ok
}

type documentTool struct {
	BaseTool
	execute func(context.Context, json.RawMessage) (*types.ToolResult, error)
}

func (t *documentTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	return t.execute(ctx, args)
}

// NewDocumentTemplatesTool exposes only the authenticated workspace's published versions.
func NewDocumentTemplatesTool(execute func(context.Context, json.RawMessage) (*types.ToolResult, error)) types.Tool {
	return &documentTool{BaseTool: NewBaseTool(ToolDocumentTemplates,
		`Query published YHSJ Document Center Excel templates, not external MCP templates or sandbox files.
Use when a user refers to a published template. Query matches template name/version locally.
Returns immutable version IDs and literal layout fields; a template does not determine the user's business task.
Choose data sources and a supported business workflow from the user's request, not from the template name.
Listing a template is not proof that an arbitrary generation workflow is implemented. Never invent template IDs.
For an explicit EBOM request only, generate_ebom is the available fixed-program business workflow.`,
		json.RawMessage(`{"type":"object","properties":{
"query":{"type":"string","maxLength":120}},"additionalProperties":false}`)), execute: execute}
}

// NewGenerateEBOMTool delegates evidence validation and arithmetic to fixed Go code.
func NewGenerateEBOMTool(execute func(context.Context, json.RawMessage) (*types.ToolResult, error)) types.Tool {
	return &documentTool{BaseTool: NewBaseTool(ToolGenerateEBOM,
		`Generate hierarchical EBOM Excel with a published YHSJ template from MCP drawing_bom_extract schema/data.
Read all schema/data pages with columns_mode=all for the SAME result_id first. Do not copy cell values into arguments.
Folder must contain one drawing set. Ask user for production_count and confirm source quantity is per direct parent.
Program identifies a unique root by exact drawing-number references.
It rejects cycles, multiple roots and bad quantities.
single_quantity = product of quantities along assembly path; batch_quantity = single_quantity * production_count.
No filename-name fuzzy matching, no procurement aggregation. Missing identities/types/fields remain partial.
Only set accept_partial after explicit user acceptance. Never claim complete on partial output.
If blocked, report validation issues and request source evidence; do not silently use scripts or a different export.
Returns a Document Center task link; no sandbox is required.`,
		json.RawMessage(`{"type":"object","properties":{
"template_id":{"type":"string"},"result_id":{"type":"string"},
"production_count":{"type":"integer","minimum":1,"maximum":1000000},
"one_drawing_set":{"type":"boolean"},"quantity_per_parent":{"type":"boolean"},
"accept_partial":{"type":"boolean"}},
"required":["template_id","result_id","production_count","one_drawing_set","quantity_per_parent"],
"additionalProperties":false}`)), execute: execute}
}
