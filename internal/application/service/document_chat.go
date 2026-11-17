package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// documentOwnedSessions avoids AgentService -> SessionService -> AgentService
// dependency cycles and preserves the exact owner-scoped repository lookup.
type documentOwnedSessions struct {
	interfaces.SessionService
	repo interfaces.SessionRepository
}

func (s documentOwnedSessions) GetOwnedSession(ctx context.Context, id string) (*types.Session, error) {
	c := types.CallerFromContext(ctx)
	return s.repo.Get(ctx, c.TenantID, c.UserID, id)
}

// NewDocumentGenerationRuntime wires owner authorization without depending on
// the session orchestration service, which itself needs the agent runtime.
func NewDocumentGenerationRuntime(
	repo *repository.DocumentGenerationRepository, sessions interfaces.SessionRepository,
	messages interfaces.MessageService, files interfaces.FileService, catalog interfaces.ResourceCatalog,
	resolver interfaces.StorageBackendResolver, tenants interfaces.TenantService,
) *DocumentGenerationService {
	return NewDocumentGenerationService(
		repo, documentOwnedSessions{repo: sessions}, messages, files, catalog, resolver, tenants)
}

// ChatEBOMRequest selects trusted evidence and an immutable template, not cell values.
type ChatEBOMRequest struct {
	TemplateID string `json:"template_id"`
	ResultID   string `json:"result_id"`
	documentgen.EBOMOptions
	AcceptPartial bool `json:"accept_partial"`
}

// GenerateChatEBOM imports only current-engine evidence or completed messages
// in this exact owned conversation. It never accepts values copied by an LLM.
func (s *DocumentGenerationService) GenerateChatEBOM(ctx context.Context, sessionID string, req ChatEBOMRequest) (
	*types.DocumentGeneration, documentgen.EBOMReport, error,
) {
	var report documentgen.EBOMReport
	c, err := documentCaller(ctx)
	if err != nil || !s.TemplatesReady(ctx) {
		return nil, report, ErrDocumentUnavailable
	}
	if err := s.authorizeSource(ctx, c, sessionID); err != nil {
		return nil, report, err
	}
	template, err := s.repo.Template(ctx, c.TenantID, req.TemplateID)
	if err != nil || template.Status != "published" {
		return nil, report, ErrDocumentUnavailable
	}
	var layout documentgen.TemplateLayout
	if err := json.Unmarshal([]byte(template.Layout), &layout); err != nil {
		return nil, report, ErrDocumentInvalid
	}
	bindings, err := documentgen.EBOMBindings(layout)
	if err != nil {
		return nil, report, fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
	}
	var source documentgen.Dataset
	messageID := ""
	turn, hasTurn := tools.DocumentTurnFromContext(ctx)
	if hasTurn && turn.MessageID != "" {
		// Validate the actual message identity, not an arbitrary engine/context ID.
		m, e := s.messages.GetMessage(types.WithExecutionTenant(ctx, c.TenantID), sessionID, turn.MessageID)
		if e != nil || m == nil || m.ID != turn.MessageID || m.SessionID != sessionID || m.Role != "assistant" {
			return nil, report, ErrDocumentUnavailable
		}
		if data, e := documentgen.ImportBOM(turn.Steps, req.ResultID); e == nil {
			source = data
			messageID = m.ID
		}
	}
	if messageID == "" {
		messages, e := s.messages.GetRecentMessagesBySession(types.WithExecutionTenant(ctx, c.TenantID), sessionID, 50)
		if e != nil {
			return nil, report, ErrDocumentUnavailable
		}
		for _, m := range messages {
			if m == nil || m.SessionID != sessionID || m.Role != "assistant" || !m.IsCompleted {
				continue
			}
			data, e := documentgen.ImportBOM(m.AgentSteps, req.ResultID)
			if e == nil {
				source = data
				messageID = m.ID
				break
			}
		}
	}
	if messageID == "" {
		return nil, report, fmt.Errorf("%w: read all schema/data pages with columns_mode=all for one result_id first",
			ErrDocumentInvalid)
	}
	data, report, err := documentgen.BuildEBOM(source, req.EBOMOptions)
	if err != nil {
		return nil, report, fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
	}
	// A field absent from a source table is not an actual blank value. If a
	// template asks for it, surface the unresolved field instead of silently
	// emitting a complete-looking empty column.
	requested := map[string]bool{}
	for _, id := range bindings {
		requested[id] = true
	}
	missing := false
	for i, column := range data.Columns {
		if !requested[column.ID] {
			continue
		}
		for j := range data.Rows {
			if data.Rows[j].Values[i].State == "not_applicable" {
				data.Rows[j].Values[i] = documentgen.Cell{State: "not_detected"}
				missing = true
			}
		}
	}
	if missing {
		data.Partial = true
		data.Issues = append(data.Issues, "模板要求的部分栏目在源表中不存在，按未检测保留，未推测或补零。")
		report.Partial = true
		report.Issues = data.Issues
	}
	if data.Partial && !req.AcceptPartial {
		return nil, report, fmt.Errorf("%w: EBOM remains partial; ask user to accept the reported limitations",
			ErrDocumentInvalid)
	}
	if err := documentgen.Validate(data); err != nil {
		return nil, report, fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, report, err
	}
	hash := sha256.Sum256(payload)
	identity := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s:%x", c.TenantID, c.UserID, sessionID, messageID, hash)))
	id := fmt.Sprintf("%x-%x-%x-%x-%x", identity[0:4], identity[4:6], identity[6:8], identity[8:10], identity[10:16])
	now := time.Now().UTC()
	row := &types.DocumentDataset{
		ID: id, TenantID: c.TenantID, UserID: c.UserID, SessionID: sessionID, MessageID: messageID,
		ResultID: req.ResultID, Payload: string(payload), ContentHash: hex.EncodeToString(hash[:]),
		RowCount: len(data.Rows), ColumnCount: len(data.Columns), Partial: data.Partial,
		CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if previous, e := s.repo.Dataset(ctx, c, id); e != nil {
		if err := s.repo.SaveDatasetOnce(ctx, row); err != nil {
			return nil, report, err
		}
	} else if previous.ContentHash != row.ContentHash {
		return nil, report, ErrDocumentConflict
	}
	keyHash := sha256.Sum256([]byte(id + ":" + req.TemplateID + fmt.Sprint(req.AcceptPartial)))
	task, err := s.GenerateWithBindings(
		ctx, id, req.TemplateID, "ebom-"+hex.EncodeToString(keyHash[:]), req.AcceptPartial, bindings)
	return task, report, err
}

// PreviewTemplate provides a private authenticated workbook for the preview
// drawer, never a public storage URL. Only published versions or owned drafts.
func (s *DocumentGenerationService) PreviewTemplate(ctx context.Context, id string) ([]byte, string, error) {
	c, err := documentCaller(ctx)
	if err != nil || !s.TemplatesReady(ctx) {
		return nil, "", ErrDocumentUnavailable
	}
	row, err := s.repo.Template(ctx, c.TenantID, id)
	if err != nil {
		return nil, "", ErrDocumentUnavailable
	}
	if row.Status != "published" && (row.CreatedBy != c.UserID || !c.Role.HasPermission(types.TenantRoleAdmin)) {
		return nil, "", ErrDocumentUnavailable
	}
	payload, err := s.templateBytes(ctx, row)
	return payload, row.Name, err
}

func (s *agentService) registerDocumentCenterTools(
	ctx context.Context, registry *tools.ToolRegistry, config *types.AgentConfig, sessionID string,
) {
	c, err := documentCaller(ctx)
	if err != nil || s.documents == nil || config.SharedAgentReadOnly || config.SkillInstallMode() ||
		sessionID == "" || !s.documents.TemplatesReady(ctx) {
		return
	}
	if err := s.documents.authorizeSource(ctx, c, sessionID); err != nil {
		return
	}
	registry.RegisterTool(tools.NewDocumentTemplatesTool(func(
		ctx context.Context, args json.RawMessage,
	) (*types.ToolResult, error) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Success: false, Error: "Invalid template query"}, nil
		}
		rows, err := s.documents.TemplateList(ctx)
		if err != nil {
			return &types.ToolResult{Success: false, Error: "Template library unavailable"}, nil
		}
		items := []map[string]any{}
		for _, row := range rows {
			if row.Status != "published" {
				continue
			}
			needle := strings.ToLower(strings.Join(strings.Fields(req.Query), ""))
			haystack := strings.ToLower(strings.Join(strings.Fields(row.Name+"v"+fmt.Sprint(row.Version)), ""))
			if needle != "" && !strings.Contains(haystack, needle) {
				continue
			}
			layout, err := s.documents.TemplateLayout(ctx, row.ID)
			if err != nil {
				continue
			}
			items = append(items, map[string]any{
				"id": row.ID, "name": row.Name, "version": row.Version,
				"format": "xlsx", "headings": layout.Fields,
			})
			if len(items) >= 30 {
				break
			}
		}
		output, _ := json.Marshal(map[string]any{
			"templates": items,
			"next_step": "Choose a supported workflow from the user's request; templates specify layout only. " +
				"Use generate_ebom only for an explicit EBOM task. " +
				"Other business generators are not provided by this catalog.",
		})
		return &types.ToolResult{Success: true, Output: string(output)}, nil
	}))
	registry.RegisterTool(tools.NewGenerateEBOMTool(func(
		ctx context.Context, args json.RawMessage,
	) (*types.ToolResult, error) {
		var req ChatEBOMRequest
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Success: false, Error: "Invalid EBOM request"}, nil
		}
		task, report, err := s.documents.GenerateChatEBOM(ctx, sessionID, req)
		if err != nil {
			output, _ := json.Marshal(map[string]any{"status": "blocked", "reason": err.Error(), "report": report})
			return &types.ToolResult{Success: false, Error: err.Error(), Output: string(output)}, nil
		}
		link := "/platform/documents?tab=tasks&task=" + task.ID
		output, _ := json.Marshal(map[string]any{
			"task_id": task.ID, "status": task.Status,
			"file_name": task.FileName, "document_link": link, "report": report,
		})
		return &types.ToolResult{
			Success: task.Status == "completed", Output: string(output),
			Data: map[string]any{"document_task_id": task.ID, "document_link": link},
		}, nil
	}))
}
