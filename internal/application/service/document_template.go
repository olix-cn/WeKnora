package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/documentgen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

const templateFileOwner = "document_template"

// TemplatesReady advertises custom templates only after the feature migration.
func (s *DocumentGenerationService) TemplatesReady(ctx context.Context) bool {
	return s.Ready(ctx) && s.repo.TemplatesReady(ctx)
}

func templateManager(ctx context.Context) (types.Caller, error) {
	c, err := documentCaller(ctx)
	if err != nil || !c.Role.HasPermission(types.TenantRoleAdmin) {
		return c, ErrDocumentUnavailable
	}
	return c, nil
}

// TemplateList hides all draft metadata from ordinary users and other managers.
func (s *DocumentGenerationService) TemplateList(ctx context.Context) ([]types.DocumentTemplateVersion, error) {
	c, err := documentCaller(ctx)
	if err != nil || !s.TemplatesReady(ctx) {
		return nil, ErrDocumentUnavailable
	}
	rows, err := s.repo.Templates(ctx, c)
	for i := range rows {
		rows[i].CanManage = c.Role.HasPermission(types.TenantRoleAdmin) && rows[i].CreatedBy == c.UserID
	}
	return rows, err
}

// UploadTemplate stores one checked workbook. A new version references a published family.
func (s *DocumentGenerationService) UploadTemplate(ctx context.Context, name, family string, payload []byte) (
	*types.DocumentTemplateVersion, documentgen.TemplateInspection, error,
) {
	var inspection documentgen.TemplateInspection
	c, err := templateManager(ctx)
	if err != nil || !s.TemplatesReady(ctx) {
		return nil, inspection, ErrDocumentUnavailable
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return nil, inspection, ErrDocumentInvalid
	}
	inspection, err = documentgen.InspectTemplate(payload)
	if err != nil {
		return nil, inspection, fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
	}
	version := 1
	familyID := uuid.NewString()
	if family != "" {
		base, err := s.repo.Template(ctx, c.TenantID, family)
		if err != nil || base.CreatedBy != c.UserID || base.Status != "published" {
			return nil, inspection, ErrDocumentUnavailable
		}
		familyID = base.FamilyID
		version, err = s.repo.NextTemplateVersion(ctx, c.TenantID, familyID)
		if err != nil {
			return nil, inspection, err
		}
	}
	now := time.Now().UTC()
	sum := sha256.Sum256(payload)
	row := &types.DocumentTemplateVersion{
		ID: uuid.NewString(), TenantID: c.TenantID, CreatedBy: c.UserID,
		Name: name, FamilyID: familyID, Version: version, Status: "draft", ContentHash: hex.EncodeToString(sum[:]),
		CreatedAt: now, UpdatedAt: now,
	}
	ref, err := s.files.SaveBytes(ctx, payload, c.TenantID, "template-"+row.ID+".xlsx", false)
	if err != nil {
		return nil, inspection, err
	}
	row.FileRef = ref
	if err := s.catalog.Bind(ctx, ref, templateFileOwner, row.ID, types.ResourceRelationSourceFile); err != nil {
		_ = s.files.DeleteFile(ctx, ref)
		return nil, inspection, err
	}
	if err := s.repo.CreateTemplate(ctx, row); err != nil {
		// Keep the bound bytes on unknown commit outcome, as for generated files.
		return nil, inspection, err
	}
	return row, inspection, nil
}

func (s *DocumentGenerationService) managedTemplate(
	ctx context.Context, id string,
) (*types.DocumentTemplateVersion, error) {
	c, err := templateManager(ctx)
	if err != nil || !s.TemplatesReady(ctx) {
		return nil, ErrDocumentUnavailable
	}
	row, err := s.repo.Template(ctx, c.TenantID, id)
	if err != nil || row.CreatedBy != c.UserID {
		return nil, ErrDocumentUnavailable
	}
	return row, nil
}

// InspectManagedTemplate returns bounded literal header choices to the creator
// only. It does not publish the workbook or guess semantic source mappings.
func (s *DocumentGenerationService) InspectManagedTemplate(ctx context.Context, id string) (
	documentgen.TemplateInspection, error,
) {
	row, err := s.managedTemplate(ctx, id)
	if err != nil {
		return documentgen.TemplateInspection{}, err
	}
	payload, err := s.templateBytes(ctx, row)
	if err != nil {
		return documentgen.TemplateInspection{}, err
	}
	return documentgen.InspectTemplate(payload)
}

func (s *DocumentGenerationService) templateBytes(
	ctx context.Context, row *types.DocumentTemplateVersion,
) ([]byte, error) {
	c := types.CallerFromContext(ctx)
	files, err := s.storedFile(ctx, c, row.FileRef)
	if err != nil {
		return nil, err
	}
	r, err := files.GetFile(ctx, row.FileRef)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	payload, err := io.ReadAll(io.LimitReader(r, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if len(payload) > 10<<20 || hex.EncodeToString(sum[:]) != row.ContentHash {
		return nil, ErrDocumentUnavailable
	}
	return payload, nil
}

// ConfigureTemplate invalidates earlier tests whenever bindings/layout change.
func (s *DocumentGenerationService) ConfigureTemplate(
	ctx context.Context, id string, layout documentgen.TemplateLayout,
) error {
	row, err := s.managedTemplate(ctx, id)
	if err != nil {
		return err
	}
	if row.Status == "published" || row.Status == "disabled" {
		return ErrDocumentConflict
	}
	payload, err := s.templateBytes(ctx, row)
	if err != nil {
		return err
	}
	if err := documentgen.ValidateLayout(payload, layout); err != nil {
		return fmt.Errorf("%w: %s", ErrDocumentInvalid, err)
	}
	b, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	oldStatus, oldHash := row.Status, row.TestHash
	sum := sha256.Sum256(append([]byte(row.ContentHash), b...))
	row.Layout = string(b)
	row.Status = "configured"
	row.TestHash = hex.EncodeToString(sum[:])
	changed, err := s.repo.ChangeTemplate(ctx, row, oldStatus, oldHash)
	if err != nil {
		return err
	}
	if !changed {
		return ErrDocumentConflict
	}
	return nil
}

// TestTemplate renders synthetic rows only; the output remains private to the manager.
func (s *DocumentGenerationService) TestTemplate(ctx context.Context, id string) ([]byte, error) {
	select {
	case s.renderSlots <- struct{}{}:
		defer func() { <-s.renderSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	row, err := s.managedTemplate(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status != "configured" && row.Status != "tested" {
		return nil, ErrDocumentConflict
	}
	payload, err := s.templateBytes(ctx, row)
	if err != nil {
		return nil, err
	}
	var layout documentgen.TemplateLayout
	if err := json.Unmarshal([]byte(row.Layout), &layout); err != nil {
		return nil, ErrDocumentInvalid
	}
	data := documentgen.Dataset{Issues: []string{"仅为合成试填，请检查布局及其他工作表是否残留真实样例数据。"}}
	bindings := map[string]string{}
	for _, field := range layout.Fields {
		data.Columns = append(data.Columns, documentgen.Column{ID: field.Key, Title: field.Label})
		bindings[field.Key] = field.Key
	}
	for i := 0; i < 30; i++ {
		r := documentgen.Row{Source: "synthetic-only.dwg"}
		for _, field := range layout.Fields {
			r.Values = append(r.Values, documentgen.Cell{
				State: "value", Value: fmt.Sprintf("样例%02d-%s", i+1, field.Label),
			})
		}
		data.Rows = append(data.Rows, r)
	}
	output, err := documentgen.RenderTemplate(ctx, payload, layout, data, bindings, false)
	if err != nil {
		return nil, err
	}
	oldStatus := row.Status
	row.Status = "tested"
	changed, err := s.repo.ChangeTemplate(ctx, row, oldStatus, row.TestHash)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrDocumentConflict
	}
	return output, nil
}

// PublishTemplate requires tested configuration and explicit confirmation of sample-data cleanup.
func (s *DocumentGenerationService) PublishTemplate(ctx context.Context, id string, sampleCleared bool) error {
	if !sampleCleared {
		return ErrDocumentInvalid
	}
	row, err := s.managedTemplate(ctx, id)
	if err != nil {
		return err
	}
	if row.Status != "tested" {
		return ErrDocumentConflict
	}
	row.Status = "published"
	changed, err := s.repo.ChangeTemplate(ctx, row, "tested", row.TestHash)
	if err != nil {
		return err
	}
	if !changed {
		return ErrDocumentConflict
	}
	return nil
}

// DisableTemplate removes a version from new selections without changing its bytes or past outputs.
func (s *DocumentGenerationService) DisableTemplate(ctx context.Context, id string) error {
	row, err := s.managedTemplate(ctx, id)
	if err != nil {
		return err
	}
	if row.Status != "published" {
		return ErrDocumentConflict
	}
	row.Status = "disabled"
	changed, err := s.repo.ChangeTemplate(ctx, row, "published", row.TestHash)
	if err != nil {
		return err
	}
	if !changed {
		return ErrDocumentConflict
	}
	return nil
}

// TemplateLayout returns published layouts to viewers, and private drafts only to their manager.
func (s *DocumentGenerationService) TemplateLayout(ctx context.Context, id string) (documentgen.TemplateLayout, error) {
	var layout documentgen.TemplateLayout
	c, err := documentCaller(ctx)
	if err != nil || !s.TemplatesReady(ctx) {
		return layout, ErrDocumentUnavailable
	}
	row, err := s.repo.Template(ctx, c.TenantID, id)
	canManage := c.Role.HasPermission(types.TenantRoleAdmin) && row.CreatedBy == c.UserID
	if err != nil || (row.Status != "published" && !canManage) {
		return layout, ErrDocumentUnavailable
	}
	err = json.Unmarshal([]byte(row.Layout), &layout)
	return layout, err
}
