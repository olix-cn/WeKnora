package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/Tencent/WeKnora/internal/storageurl"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	// ErrDocumentUnavailable also covers inaccessible sources without revealing their identity.
	ErrDocumentUnavailable = errors.New("document source or task is unavailable")
	// ErrDocumentConflict means a request key was reused with different parameters.
	ErrDocumentConflict = errors.New("generation request key conflicts with another request")
	// ErrDocumentInvalid identifies a safe client validation error.
	ErrDocumentInvalid = errors.New("invalid generation request")
)

const documentFileOwner = "document_generation"

// DocumentSource identifies an importable result in an owned conversation.
type DocumentSource struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	ResultID  string `json:"result_id"`
	Rows      int    `json:"row_count"`
	Columns   int    `json:"column_count"`
	Partial   bool   `json:"partial"`
}

// Sources discovers complete stored schema/data page sets without exposing row payloads.
func (s *DocumentGenerationService) Sources(ctx context.Context, sessionID string) ([]DocumentSource, error) {
	c, err := documentCaller(ctx)
	if err != nil || !s.Ready(ctx) {
		return nil, ErrDocumentUnavailable
	}
	if err := s.authorizeSource(ctx, c, sessionID); err != nil {
		return nil, err
	}
	messages, err := s.messages.GetRecentMessagesBySession(types.WithExecutionTenant(ctx, c.TenantID), sessionID, 50)
	if err != nil {
		return nil, ErrDocumentUnavailable
	}
	out := []DocumentSource{}
	attempts := 0
	for _, m := range messages {
		if m == nil || !m.IsCompleted || m.Role != "assistant" || m.SessionID != sessionID {
			continue
		}
		ids := map[string]bool{}
		for _, step := range m.AgentSteps {
			for _, call := range step.ToolCalls {
				if call.Result == nil || !call.Result.Success {
					continue
				}
				v, ok := call.Result.Data["structured_content"].(map[string]interface{})
				if ok {
					if id, ok := v["result_id"].(string); ok && id != "" {
						ids[id] = true
					}
				}
			}
		}
		ordered := make([]string, 0, len(ids))
		for id := range ids {
			ordered = append(ordered, id)
		}
		sort.Strings(ordered)
		for _, id := range ordered {
			attempts++
			if attempts > 20 {
				return out, nil
			}
			data, err := documentgen.ImportBOM(m.AgentSteps, id)
			if err != nil {
				continue
			}
			out = append(out, DocumentSource{
				SessionID: sessionID, MessageID: m.ID, ResultID: id,
				Rows: len(data.Rows), Columns: len(data.Columns), Partial: data.Partial,
			})
		}
	}
	return out, nil
}

// DocumentGenerationService imports persisted MCP pages and renders immutable private tasks.
type DocumentGenerationService struct {
	repo        *repository.DocumentGenerationRepository
	sessions    interfaces.SessionService
	messages    interfaces.MessageService
	files       interfaces.FileService
	catalog     interfaces.ResourceCatalog
	resolver    interfaces.StorageBackendResolver
	tenants     interfaces.TenantService
	renderSlots chan struct{}
}

// NewDocumentGenerationService wires document generation to existing authorization and storage.
func NewDocumentGenerationService(
	repo *repository.DocumentGenerationRepository, sessions interfaces.SessionService,
	messages interfaces.MessageService, files interfaces.FileService, catalog interfaces.ResourceCatalog,
	resolver interfaces.StorageBackendResolver, tenants interfaces.TenantService,
) *DocumentGenerationService {
	return &DocumentGenerationService{
		repo: repo, sessions: sessions, messages: messages,
		files: files, catalog: catalog, resolver: resolver, tenants: tenants,
		renderSlots: make(chan struct{}, 2),
	}
}

// Ready never applies migrations; an old deployment remains preview-only.
func (s *DocumentGenerationService) Ready(ctx context.Context) bool {
	return s != nil && s.repo != nil && s.files != nil && s.catalog != nil && s.repo.Ready(ctx)
}

func documentCaller(ctx context.Context) (types.Caller, error) {
	c := types.CallerFromContext(ctx)
	if c.UserID == "" || c.TenantID == 0 {
		return c, ErrDocumentUnavailable
	}
	if _, key := types.TenantAPIKeyScopeFromContext(ctx); key {
		return c, ErrDocumentUnavailable
	}
	return c, nil
}

func (s *DocumentGenerationService) authorizeSource(ctx context.Context, c types.Caller, sessionID string) error {
	if s.sessions == nil {
		return ErrDocumentUnavailable
	}
	// Do not allow the legacy tenant-wide/session-admin read fallback for imports.
	session, err := s.sessions.GetOwnedSession(types.WithExecutionTenant(ctx, c.TenantID), sessionID)
	if err != nil || session == nil || session.TenantID != c.TenantID || session.UserID != c.UserID {
		return ErrDocumentUnavailable
	}
	return nil
}

// Import copies complete structured pages of an owned completed message, not browser-provided cells.
func (s *DocumentGenerationService) Import(ctx context.Context, sessionID, messageID, resultID string) (
	*types.DocumentDataset, error,
) {
	c, err := documentCaller(ctx)
	if err != nil {
		return nil, err
	}
	if !s.Ready(ctx) {
		return nil, ErrDocumentUnavailable
	}
	if err := s.authorizeSource(ctx, c, sessionID); err != nil {
		return nil, err
	}
	message, err := s.messages.GetMessage(types.WithExecutionTenant(ctx, c.TenantID), sessionID, messageID)
	if err != nil || message == nil || message.ID != messageID || message.SessionID != sessionID ||
		message.Role != "assistant" ||
		!message.IsCompleted || message.AgentSteps == nil {
		return nil, ErrDocumentUnavailable
	}
	data, err := documentgen.ImportBOM(message.AgentSteps, resultID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(payload)
	now := time.Now().UTC()
	row := &types.DocumentDataset{
		ID: uuid.NewString(), TenantID: c.TenantID, UserID: c.UserID,
		Issues:    data.Issues,
		SessionID: sessionID, MessageID: messageID, ResultID: resultID, Payload: string(payload),
		ContentHash: hex.EncodeToString(hash[:]), RowCount: len(data.Rows), ColumnCount: len(data.Columns),
		Partial: data.Partial, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	for _, column := range data.Columns {
		row.Fields = append(row.Fields, types.DocumentDatasetField{ID: column.ID, Title: column.Title})
	}
	if err := s.repo.SaveDataset(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *DocumentGenerationService) dataset(ctx context.Context, c types.Caller, id string) (
	*types.DocumentDataset, error,
) {
	row, err := s.repo.Dataset(ctx, c, id)
	if err != nil || !row.ExpiresAt.After(time.Now()) {
		return nil, ErrDocumentUnavailable
	}
	if err := s.authorizeSource(ctx, c, row.SessionID); err != nil {
		return nil, err
	}
	message, err := s.messages.GetMessage(types.WithExecutionTenant(ctx, c.TenantID), row.SessionID, row.MessageID)
	turn, active := tools.DocumentTurnFromContext(ctx)
	if err != nil || message == nil || message.ID != row.MessageID || message.SessionID != row.SessionID ||
		message.Role != "assistant" || (!message.IsCompleted && (!active || turn.MessageID != message.ID)) {
		return nil, ErrDocumentUnavailable
	}
	return row, nil
}

// Generate synchronously renders a bounded dataset; retries with the same key never run twice.
// A process crash leaves a durable generating task; the UI reports it as interrupted after its budget.
func (s *DocumentGenerationService) Generate(ctx context.Context, datasetID, templateID, key string, accept bool) (
	*types.DocumentGeneration, error,
) {
	return s.GenerateWithBindings(ctx, datasetID, templateID, key, accept, nil)
}

// GenerateWithBindings fills an immutable published custom template or the built-in layout.
func (s *DocumentGenerationService) GenerateWithBindings(ctx context.Context, datasetID, templateID, key string,
	accept bool, bindings map[string]string,
) (*types.DocumentGeneration, error) {
	c, err := documentCaller(ctx)
	if err != nil {
		return nil, err
	}
	if !s.Ready(ctx) {
		return nil, ErrDocumentUnavailable
	}
	if len(key) < 8 || len(key) > 128 || strings.TrimSpace(key) != key || len(bindings) > 50 {
		return nil, ErrDocumentInvalid
	}
	row, err := s.dataset(ctx, c, datasetID)
	if err != nil {
		return nil, err
	}
	if row.Partial && !accept {
		return nil, fmt.Errorf("%w: partial data requires acceptance", ErrDocumentInvalid)
	}
	requestPayload, _ := json.Marshal(struct {
		Dataset  string
		Template string
		Accept   bool
		Bindings map[string]string
	}{
		datasetID, templateID, accept, bindings,
	})
	requestHash := sha256.Sum256(requestPayload)
	if previous, err := s.repo.ByKey(ctx, c, key); err == nil {
		if previous.RequestHash != hex.EncodeToString(requestHash[:]) {
			return nil, ErrDocumentConflict
		}
		return previous, nil
	}
	var custom *types.DocumentTemplateVersion
	var templatePayload []byte
	var layout documentgen.TemplateLayout
	if templateID != documentgen.BOMTemplateID {
		if !s.TemplatesReady(ctx) {
			return nil, ErrDocumentUnavailable
		}
		custom, err = s.repo.Template(ctx, c.TenantID, templateID)
		if err != nil || custom.Status != "published" {
			return nil, ErrDocumentUnavailable
		}
		if err = json.Unmarshal([]byte(custom.Layout), &layout); err != nil {
			return nil, ErrDocumentInvalid
		}
		templatePayload, err = s.templateBytes(ctx, custom)
		if err != nil {
			return nil, err
		}
	} else if len(bindings) != 0 {
		return nil, ErrDocumentInvalid
	}
	if custom != nil {
		var data documentgen.Dataset
		if err := json.Unmarshal([]byte(row.Payload), &data); err != nil {
			return nil, ErrDocumentInvalid
		}
		if err := documentgen.ValidateTemplateBindings(layout, data, bindings, accept); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
		}
	}
	// Bound workbook memory/CPU independently of the model's concurrency gate.
	// Waiting still observes request cancellation; only reserved work survives a disconnect.
	select {
	case s.renderSlots <- struct{}{}:
		defer func() { <-s.renderSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	now := time.Now().UTC()
	name := "BOM_" + uuid.NewString() + ".xlsx"
	if row.Partial {
		name = "BOM_" + uuid.NewString() + ".partial.xlsx"
	}
	var namingData documentgen.Dataset
	payloadHash := sha256.Sum256([]byte(row.Payload))
	if hex.EncodeToString(payloadHash[:]) == row.ContentHash &&
		json.Unmarshal([]byte(row.Payload), &namingData) == nil && namingData.RootAssembly != nil {
		// Partial status remains on the task and in chat. Do not change the
		// user's filename pattern by appending .partial or random identifiers.
		name = documentgen.AssemblyFileName(*namingData.RootAssembly, now)
	}
	task := &types.DocumentGeneration{
		ID: uuid.NewString(), TenantID: c.TenantID, UserID: c.UserID,
		RequestKey: key, RequestHash: hex.EncodeToString(requestHash[:]), DatasetID: datasetID, TemplateID: templateID,
		Partial: row.Partial, Status: "generating", FileName: name, CreatedAt: now, UpdatedAt: now,
		ExpiresAt: row.ExpiresAt,
	}
	var created bool
	if custom != nil {
		created, err = s.repo.CreateTemplateTask(ctx, task)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDocumentUnavailable
		}
	} else {
		created, err = s.repo.CreateTask(ctx, task)
	}
	if err != nil {
		return nil, err
	}
	if !created {
		existing, err := s.repo.ByKey(ctx, c, key)
		if err != nil {
			return nil, err
		}
		if existing.RequestHash != task.RequestHash {
			return nil, ErrDocumentConflict
		}
		return existing, nil
	}
	// Finish persists even if the browser disconnects. This is bounded, not a detached worker.
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var data documentgen.Dataset
	datasetHash := sha256.Sum256([]byte(row.Payload))
	if hex.EncodeToString(datasetHash[:]) != row.ContentHash {
		return s.fail(run, task, "dataset_invalid")
	}
	if err := json.Unmarshal([]byte(row.Payload), &data); err != nil {
		return s.fail(run, task, "dataset_invalid")
	}
	var output []byte
	if custom != nil {
		output, err = documentgen.RenderTemplate(run, templatePayload, layout, data, bindings, accept)
	} else {
		output, err = documentgen.RenderBOM(run, data, accept)
	}
	if err != nil {
		return s.fail(run, task, "render_failed")
	}
	ref, err := s.files.SaveBytes(run, output, c.TenantID, task.FileName, false)
	if err != nil {
		return s.fail(run, task, "storage_failed")
	}
	if err := s.catalog.Bind(run, ref, documentFileOwner, task.ID, types.ResourceRelationArtifact); err != nil {
		_ = s.files.DeleteFile(run, ref)
		return s.fail(run, task, "file_binding_failed")
	}
	hash := sha256.Sum256(output)
	task.Status = "completed"
	task.FileRef = ref
	task.FileSize = int64(len(output))
	task.ContentHash = hex.EncodeToString(hash[:])
	if err := s.repo.Finish(run, task); err != nil {
		// A database timeout may occur after COMMIT. Keep the bound bytes for
		// reconciliation rather than deleting a possibly completed task's file.
		return nil, err
	}
	return task, nil
}

func (s *DocumentGenerationService) fail(ctx context.Context, row *types.DocumentGeneration, code string) (
	*types.DocumentGeneration, error,
) {
	// Persist the failure using a small independent budget even when rendering
	// exhausted its deadline; do not leave every timeout looking like a live task.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	row.Status = "failed"
	row.ErrorCode = code
	if err := s.repo.Finish(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// List returns private task metadata, after rechecking access to the source.
func (s *DocumentGenerationService) List(
	ctx context.Context, page, size int,
) ([]types.DocumentGeneration, int64, error) {
	c, err := documentCaller(ctx)
	if err != nil || !s.Ready(ctx) {
		return nil, 0, ErrDocumentUnavailable
	}
	page = max(1, page)
	size = min(100, max(1, size))
	rows, total, err := s.repo.List(ctx, c, page, size)
	if err != nil {
		return nil, 0, err
	}
	for i := range rows {
		if _, err := s.dataset(ctx, c, rows[i].DatasetID); err != nil {
			rows[i].Status = "unavailable"
		}
		if rows[i].Status == "generating" && time.Since(rows[i].CreatedAt) > time.Minute {
			rows[i].Status = "interrupted"
		}
	}
	return rows, total, nil
}

func (s *DocumentGenerationService) task(
	ctx context.Context, id string,
) (*types.DocumentGeneration, types.Caller, error) {
	c, err := documentCaller(ctx)
	if err != nil || !s.Ready(ctx) {
		return nil, c, ErrDocumentUnavailable
	}
	row, err := s.repo.Task(ctx, c, id)
	if err != nil || !row.ExpiresAt.After(time.Now()) {
		return nil, c, ErrDocumentUnavailable
	}
	if _, err := s.dataset(ctx, c, row.DatasetID); err != nil {
		return nil, c, err
	}
	return row, c, nil
}

func (s *DocumentGenerationService) storedFile(ctx context.Context, c types.Caller, ref string) (
	interfaces.FileService, error,
) {
	resource, err := s.catalog.Resolve(ctx, ref)
	if err != nil || resource == nil || resource.TenantID != c.TenantID {
		return nil, ErrDocumentUnavailable
	}
	if s.resolver == nil {
		return s.files, nil
	}
	tenant, err := s.tenants.GetTenantByID(ctx, c.TenantID)
	if err != nil || tenant == nil {
		return nil, ErrDocumentUnavailable
	}
	files, _, err := s.resolver.ResolveFileService(ctx, tenant, resource.StorageBackendID,
		resource.Provider, storageurl.LocalStorageBaseDir())
	return files, err
}

// Download rechecks task and source ownership; storage URLs never reach the client.
func (s *DocumentGenerationService) Download(
	ctx context.Context, id string,
) (io.ReadCloser, *types.DocumentGeneration, error) {
	row, c, err := s.task(ctx, id)
	if err != nil || row.Status != "completed" {
		return nil, nil, ErrDocumentUnavailable
	}
	files, err := s.storedFile(ctx, c, row.FileRef)
	if err != nil {
		return nil, nil, err
	}
	reader, err := files.GetFile(ctx, row.FileRef)
	return reader, row, err
}

// Delete marks the task file as deleted and releases its single storage claim.
func (s *DocumentGenerationService) Delete(ctx context.Context, id string) error {
	row, c, err := s.task(ctx, id)
	if err != nil {
		return err
	}
	if row.Status != "completed" {
		return ErrDocumentUnavailable
	}
	changed, err := s.repo.DeleteFile(ctx, c, id)
	if err != nil {
		return err
	}
	if !changed {
		return ErrDocumentUnavailable
	}
	remaining, err := s.catalog.Release(ctx, row.FileRef, documentFileOwner, row.ID)
	if err != nil || remaining != 0 {
		return err
	}
	files, err := s.storedFile(ctx, c, row.FileRef)
	if err != nil {
		return err
	}
	return files.DeleteFile(ctx, row.FileRef)
}
