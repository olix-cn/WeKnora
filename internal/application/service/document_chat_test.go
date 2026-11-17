package service

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func chatEBOMSteps(t *testing.T) []types.AgentStep {
	t.Helper()
	meta := `"result_id":"ebom-snapshot","source_kind":"saved_file","expires_at":1000,`
	schema := `{` + meta + `"mode":"schema","page":{"total":10,"offset":0,"returned":10,"has_more":false},"records":[
{"kind":"file","file_id":"root","source":"ROOT synthetic.dwg","drawing_number":"ROOT","drawing_name":"合成总装配",
"document_kind":"assembly","quantity_basis":"per_parent"},
{"kind":"file","file_id":"sub","source":"SUB synthetic.dwg","drawing_number":"SUB",
"document_kind":"assembly","quantity_basis":"per_parent"},
{"kind":"file","file_id":"part","source":"PART synthetic.dwg","drawing_number":"PART",
"document_kind":"part","revision":"A"},
{"kind":"table","table_id":"r","file_id":"root","layout":"Model","handle":"R",
"schema_complete":true,"physical_columns_enumerated":true,"row_count_known":true,"row_count":1},
{"kind":"table","table_id":"s","file_id":"sub","layout":"Model","handle":"S",
"schema_complete":true,"physical_columns_enumerated":true,"row_count_known":true,"row_count":1},
{"kind":"column","table_id":"r","column_id":"rn","position":1,"raw":"代号","state":"confirmed"},
{"kind":"column","table_id":"r","column_id":"rq","position":2,"raw":"数量","state":"confirmed"},
{"kind":"column","table_id":"s","column_id":"sn","position":1,"raw":"代号","state":"confirmed"},
{"kind":"column","table_id":"s","column_id":"sq","position":2,"raw":"数量","state":"confirmed"},
{"kind":"header","table_id":"r","raw":"synthetic"}]}`
	data := `{` + meta + `"mode":"data","status":"complete","complete":true,"row_count":2,"row_count_complete":true,
"selection":{"complete":true,"scope_covers_all_files":true},"page":{"total":2,"offset":0,"returned":2,"has_more":false},
"rows":[{"row_id":"r1","table_id":"r","values":{"rn":"SUB","rq":"2"},"states":{"rn":"value","rq":"value"}},
{"row_id":"s1","table_id":"s","values":{"sn":"PART","sq":"1.25"},"states":{"sn":"value","sq":"value"}}]}`
	steps := []types.AgentStep{}
	for _, text := range []string{schema, data} {
		var structured map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &structured))
		steps = append(steps, types.AgentStep{ToolCalls: []types.ToolCall{{
			Name: "mcp_cad_assistant_drawing_bom_extract_test", Args: map[string]any{"columns_mode": "all"},
			Result: &types.ToolResult{Success: true, Data: map[string]any{"structured_content": structured}},
		}}})
	}
	return steps
}

func TestChatEBOMCurrentTurnGenerationAndTemplatePreview(t *testing.T) {
	s, storage, _, db := generationFixture(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/000054_document_templates.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	admin := templateAdmin("owner", 7)
	ctx := generationContext("owner", 7)
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	layout := documentgen.TemplateLayout{Sheet: "Sheet1", HeaderRow: 1, StartRow: 2}
	for i, label := range []string{"代号", "单台数量", "多台数量"} {
		col, _ := excelize.ColumnNumberToName(i + 1)
		require.NoError(t, f.SetCellStr("Sheet1", col+"1", label))
		layout.Fields = append(layout.Fields, documentgen.TemplateField{Key: col, Label: label, Column: col})
	}
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	template, _, err := s.UploadTemplate(admin, "合成EBOM", "", buf.Bytes())
	require.NoError(t, err)
	_, _, err = s.PreviewTemplate(ctx, template.ID)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	require.NoError(t, s.ConfigureTemplate(admin, template.ID, layout))
	_, err = s.TestTemplate(admin, template.ID)
	require.NoError(t, err)
	require.NoError(t, s.PublishTemplate(admin, template.ID, true))
	preview, _, err := s.PreviewTemplate(ctx, template.ID)
	require.NoError(t, err)
	require.Equal(t, buf.Bytes(), preview)
	_, _, err = s.PreviewTemplate(generationContext("owner", 8), template.ID)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	message := s.messages.(*documentMessages).message
	message.IsCompleted = false
	steps := chatEBOMSteps(t)
	ctx = tools.WithDocumentTurn(ctx, "message", steps)
	req := ChatEBOMRequest{
		TemplateID: template.ID, ResultID: "ebom-snapshot",
		EBOMOptions: documentgen.EBOMOptions{ProductionCount: 3, OneDrawingSet: true, QuantityPerParent: true},
	}
	job, report, err := s.GenerateChatEBOM(ctx, "session", req)
	require.NoError(t, err)
	require.False(t, report.Partial)
	require.Equal(t, "ROOT", report.RootNumber)
	require.Equal(t, "合成总装配", report.RootName)
	require.Equal(t, "completed", job.Status)
	require.Equal(t, documentgen.AssemblyFileName(documentgen.AssemblyIdentity{
		Number: "ROOT", Name: "合成总装配",
	}, job.CreatedAt), job.FileName)
	// Until the real assistant message completes, browser downloads are denied.
	_, _, err = s.Download(generationContext("owner", 7), job.ID)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	message.IsCompleted = true
	message.AgentSteps = steps
	again, _, err := s.GenerateChatEBOM(generationContext("owner", 7), "session", req)
	require.NoError(t, err)
	require.Equal(t, job.ID, again.ID)
	require.Equal(t, job.FileName, again.FileName)
	require.Equal(t, 2, storage.saves)
	r, _, err := s.Download(generationContext("owner", 7), job.ID)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	workbook, err := excelize.OpenReader(r)
	require.NoError(t, err)
	defer func() { _ = workbook.Close() }()
	require.Equal(t, []string{"Sheet1"}, workbook.GetSheetList())
	for axis, want := range map[string]string{
		"A2": "SUB", "B2": "2", "C2": "6", "A3": "PART", "B3": "2.5", "C3": "7.5",
	} {
		got, err := workbook.GetCellValue("Sheet1", axis)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, _, err = s.GenerateChatEBOM(generationContext("other", 7), "session", req)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	_, _, err = s.GenerateChatEBOM(generationContext("owner", 8), "session", req)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	require.NotEmpty(t, storage.data)
}

func TestChatDocumentToolsExposePublishedTemplatesOnly(t *testing.T) {
	s, _, _, db := generationFixture(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/000054_document_templates.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	admin := templateAdmin("owner", 7)
	payload, layout := templateWorkbook(t)
	row, _, err := s.UploadTemplate(admin, "测试", "", payload)
	require.NoError(t, err)
	require.NoError(t, s.ConfigureTemplate(admin, row.ID, layout))
	_, err = s.TestTemplate(admin, row.ID)
	require.NoError(t, err)
	require.NoError(t, s.PublishTemplate(admin, row.ID, true))
	_, _, err = s.UploadTemplate(admin, "私有草稿", "", payload)
	require.NoError(t, err)
	registry := tools.NewToolRegistry()
	agent := &agentService{documents: s}
	agent.registerDocumentCenterTools(generationContext("owner", 7), registry, &types.AgentConfig{}, "session")
	tool, err := registry.GetTool(tools.ToolDocumentTemplates)
	require.NoError(t, err)
	result, err := tool.Execute(generationContext("owner", 7), json.RawMessage(`{"query":"测试V1"}`))
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Contains(t, result.Output, row.ID)
	require.NotContains(t, result.Output, "私有草稿")
	require.NotContains(t, result.Output, "ebom_supported")
	for _, ctx := range []struct {
		user   string
		tenant uint64
	}{{"other", 7}, {"owner", 8}} {
		r := tools.NewToolRegistry()
		agent.registerDocumentCenterTools(generationContext(ctx.user, ctx.tenant), r, &types.AgentConfig{}, "session")
		_, err := r.GetTool(tools.ToolGenerateEBOM)
		require.Error(t, err)
	}
	for _, config := range []*types.AgentConfig{{SharedAgentReadOnly: true}} {
		r := tools.NewToolRegistry()
		agent.registerDocumentCenterTools(generationContext("owner", 7), r, config, "session")
		_, err := r.GetTool(tools.ToolGenerateEBOM)
		require.Error(t, err)
	}
	keyCtx := types.WithTenantAPIKeyScope(generationContext("owner", 7), types.TenantAPIKeyScope{FullAccess: true})
	r := tools.NewToolRegistry()
	agent.registerDocumentCenterTools(keyCtx, r, &types.AgentConfig{}, "session")
	_, err = r.GetTool(tools.ToolGenerateEBOM)
	require.Error(t, err)
}

func TestTemplateCatalogDoesNotClassifyTemplatesByEBOMHeadings(t *testing.T) {
	s, _, _, db := generationFixture(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/000054_document_templates.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	admin := templateAdmin("owner", 7)
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	require.NoError(t, f.SetCellStr("Sheet1", "A1", "客户名称"))
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	row, _, err := s.UploadTemplate(admin, "客户清单", "", buf.Bytes())
	require.NoError(t, err)
	layout := documentgen.TemplateLayout{
		Sheet: "Sheet1", HeaderRow: 1, StartRow: 2,
		Fields: []documentgen.TemplateField{{Key: "customer", Label: "客户名称", Column: "A"}},
	}
	require.NoError(t, s.ConfigureTemplate(admin, row.ID, layout))
	_, err = s.TestTemplate(admin, row.ID)
	require.NoError(t, err)
	require.NoError(t, s.PublishTemplate(admin, row.ID, true))
	r := tools.NewToolRegistry()
	(&agentService{documents: s}).registerDocumentCenterTools(
		generationContext("owner", 7), r, &types.AgentConfig{}, "session")
	tool, err := r.GetTool(tools.ToolDocumentTemplates)
	require.NoError(t, err)
	result, err := tool.Execute(generationContext("owner", 7), json.RawMessage(`{"query":"客户清单"}`))
	require.NoError(t, err)
	var catalog struct {
		Templates []map[string]any `json:"templates"`
		NextStep  string           `json:"next_step"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Output), &catalog))
	require.Len(t, catalog.Templates, 1)
	require.Equal(t, row.ID, catalog.Templates[0]["id"])
	require.Equal(t, "xlsx", catalog.Templates[0]["format"])
	require.NotContains(t, catalog.Templates[0], "ebom_supported")
	require.Contains(t, catalog.NextStep, "templates specify layout only")
	// A general template remains valid even when a specific business adapter
	// cannot interpret its columns. Catalog membership is not business capability.
	_, err = documentgen.EBOMBindings(layout)
	require.Error(t, err)
}

func TestChatEBOMPartialRequiresAcceptanceAndRevokedPreviewIsDenied(t *testing.T) {
	s, _, _, db := generationFixture(t)
	sql, err := os.ReadFile("../../../migrations/sqlite/000054_document_templates.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(sql)).Error)
	admin := templateAdmin("owner", 7)
	ctx := generationContext("owner", 7)
	payload, layout := templateWorkbook(t)
	row, _, err := s.UploadTemplate(admin, "partial", "", payload)
	require.NoError(t, err)
	require.NoError(t, s.ConfigureTemplate(admin, row.ID, layout))
	_, err = s.TestTemplate(admin, row.ID)
	require.NoError(t, err)
	require.NoError(t, s.PublishTemplate(admin, row.ID, true))
	steps := chatEBOMSteps(t)
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			sc := call.Result.Data["structured_content"].(map[string]any)
			if sc["mode"] != "schema" {
				continue
			}
			for _, raw := range sc["records"].([]any) {
				record := raw.(map[string]any)
				if record["kind"] == "file" {
					delete(record, "document_kind")
				}
			}
		}
	}
	message := s.messages.(*documentMessages).message
	message.IsCompleted = true
	message.AgentSteps = steps
	req := ChatEBOMRequest{
		TemplateID: row.ID, ResultID: "ebom-snapshot",
		EBOMOptions: documentgen.EBOMOptions{ProductionCount: 2, OneDrawingSet: true, QuantityPerParent: true},
	}
	_, report, err := s.GenerateChatEBOM(ctx, "session", req)
	require.ErrorIs(t, err, ErrDocumentInvalid)
	require.True(t, report.Partial)
	var count int64
	require.NoError(t, db.Model(&types.DocumentGeneration{}).Count(&count).Error)
	require.Zero(t, count)
	req.AcceptPartial = true
	job, report, err := s.GenerateChatEBOM(ctx, "session", req)
	require.NoError(t, err)
	require.True(t, job.Partial)
	require.True(t, report.Partial)
	require.Regexp(t, `^ROOT-合成总装配-\d{14}\.xlsx$`, job.FileName)
	require.NotContains(t, job.FileName, ".partial")
	require.NoError(t, s.DisableTemplate(admin, row.ID))
	_, _, err = s.PreviewTemplate(ctx, row.ID)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
	_, _, err = s.GenerateChatEBOM(ctx, "session", req)
	require.ErrorIs(t, err, ErrDocumentUnavailable)
}
