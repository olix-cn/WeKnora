// Package documentgen renders structured data without model-generated code.
package documentgen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// BOMTemplateID identifies the immutable first built-in BOM layout.
const BOMTemplateID = "engineering_bom_v1"

// Cell retains the distinction between a confirmed blank and an unread value.
type Cell struct {
	Value string `json:"value"`
	State string `json:"state"` // value, blank, not_detected, ambiguous
}

// Column identifies a source column independently of its displayed title.
type Column struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	TableID   string `json:"table_id,omitempty"`
	Header    string `json:"header,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
}

// Row keeps source attribution alongside ordered cells.
type Row struct {
	Source  string `json:"source"`
	Values  []Cell `json:"values"`
	TableID string `json:"table_id,omitempty"`
	ID      string `json:"row_id,omitempty"`
}

// Dataset must be assembled by an authorized data adapter, not the model.
type Dataset struct {
	RootAssembly *AssemblyIdentity `json:"root_assembly,omitempty"`
	Drawings     []Drawing         `json:"drawings,omitempty"`
	Columns      []Column          `json:"columns"`
	Rows         []Row             `json:"rows"`
	Partial      bool              `json:"partial"`
	Issues       []string          `json:"issues"`
}

// AssemblyIdentity records the program-selected root for export naming.
type AssemblyIdentity struct {
	Number string `json:"number"`
	Name   string `json:"name"`
}

// Drawing retains source identities for deterministic assembly reconstruction.
type Drawing struct {
	Name          string   `json:"name,omitempty"`
	FileID        string   `json:"file_id"`
	Source        string   `json:"source"`
	Number        string   `json:"number"`
	Revision      string   `json:"revision,omitempty"`
	Kind          string   `json:"kind,omitempty"` // assembly or part, only when explicitly supplied
	QuantityBasis string   `json:"quantity_basis,omitempty"`
	Tables        []string `json:"tables"`
}

// Validate checks an imported dataset without rendering a workbook.
func Validate(data Dataset) error { return validate(data, true) }

// RenderBOM returns a verified workbook; it never writes a path supplied by a caller.
// The first version is a flat, source-preserving BOM, without quantity aggregation.
func RenderBOM(ctx context.Context, data Dataset, allowPartial bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validate(data, allowPartial); err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", "BOM明细"); err != nil {
		return nil, err
	}
	for _, sheet := range []string{"读取说明", "单元格状态"} {
		if _, err := f.NewSheet(sheet); err != nil {
			return nil, err
		}
	}
	set := func(sheet string, col, row int, value string) error {
		axis, err := excelize.CoordinatesToCellName(col, row)
		if err != nil {
			return err
		}
		// SetCellStr writes even =/+/-/@ prefixed source text as text, never a formula.
		return f.SetCellStr(sheet, axis, value)
	}
	for i, column := range data.Columns {
		if err := set("BOM明细", i+1, 1, column.Title); err != nil {
			return nil, err
		}
	}
	if err := set("BOM明细", len(data.Columns)+1, 1, "来源"); err != nil {
		return nil, err
	}
	for col, title := range []string{"明细行", "字段ID", "状态", "来源"} {
		if err := set("单元格状态", col+1, 1, title); err != nil {
			return nil, err
		}
	}
	stateRow := 2
	for i, row := range data.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for j, cell := range row.Values {
			value := display(cell)
			if err := set("BOM明细", j+1, i+2, value); err != nil {
				return nil, err
			}
			if cell.State != "value" {
				for col, v := range []string{fmt.Sprint(i + 2), data.Columns[j].ID, cell.State, row.Source} {
					if err := set("单元格状态", col+1, stateRow, v); err != nil {
						return nil, err
					}
				}
				stateRow++
			}
		}
		if err := set("BOM明细", len(data.Columns)+1, i+2, row.Source); err != nil {
			return nil, err
		}
	}
	status := "完整数据（仅限本次输入范围）"
	if data.Partial {
		status = "部分数据，不可视为完整 BOM"
	}
	notes := []string{
		status, "模板: " + BOMTemplateID,
		"仅核对导出与输入快照一致；不代表 CAD 或原生 BOM 数据库独立验证。",
	}
	notes = append(notes, data.Issues...)
	for i, note := range notes {
		if err := set("读取说明", 1, i+1, note); err != nil {
			return nil, err
		}
	}
	last, _ := excelize.ColumnNumberToName(len(data.Columns) + 1)
	if err := f.SetColWidth("BOM明细", "A", last, 24); err != nil {
		return nil, err
	}
	if err := f.SetPanes("BOM明细", &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2"}); err != nil {
		return nil, err
	}
	if err := f.AutoFilter("BOM明细", fmt.Sprintf("A1:%s%d", last, len(data.Rows)+1), nil); err != nil {
		return nil, err
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	if err := verify(ctx, buf.Bytes(), data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func display(cell Cell) string {
	switch cell.State {
	case "not_detected":
		return "未检测"
	case "ambiguous":
		return "待核实"
	case "not_applicable":
		return "不适用"
	default:
		return cell.Value
	}
}

func validText(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 32767 {
		return false
	}
	for _, r := range s {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func validate(data Dataset, allowPartial bool) error {
	if len(data.Columns) == 0 || len(data.Columns) > 100 || len(data.Rows) > 10000 || len(data.Issues) > 1000 {
		return errors.New("unsupported BOM dimensions")
	}
	if len(data.Rows)*len(data.Columns) > 100000 {
		return errors.New("too many BOM cells")
	}
	totalBytes := 0
	seen := map[string]bool{}
	for _, col := range data.Columns {
		if strings.TrimSpace(col.ID) == "" || seen[col.ID] || strings.TrimSpace(col.Title) == "" ||
			!validText(col.ID) || !validText(col.Title) {
			return errors.New("invalid or duplicate BOM column")
		}
		seen[col.ID] = true
		totalBytes += len(col.ID) + len(col.Title)
	}
	for _, row := range data.Rows {
		if len(row.Values) != len(data.Columns) || strings.TrimSpace(row.Source) == "" || !validText(row.Source) {
			return errors.New("BOM row shape or source is invalid")
		}
		for _, cell := range row.Values {
			totalBytes += len(cell.Value) + len(row.Source) + len(cell.State)
			if totalBytes > 16<<20 {
				return errors.New("BOM text exceeds export budget")
			}
			if !validText(cell.Value) {
				return errors.New("invalid cell text")
			}
			switch cell.State {
			case "value":
			case "blank", "not_applicable":
				if cell.Value != "" {
					return errors.New("confirmed blank cannot contain a value")
				}
			case "not_detected", "ambiguous":
				if !data.Partial || cell.Value != "" {
					return errors.New("unresolved cells require partial data and no inferred value")
				}
			default:
				return errors.New("unknown cell state")
			}
		}
	}
	for _, issue := range data.Issues {
		totalBytes += len(issue)
		if !validText(issue) {
			return errors.New("invalid issue text")
		}
	}
	if totalBytes > 16<<20 {
		return errors.New("BOM text exceeds export budget")
	}
	if data.Partial && (!allowPartial || len(data.Issues) == 0) {
		return errors.New("partial data requires explicit acceptance and an issue description")
	}
	return nil
}

func verify(ctx context.Context, workbook []byte, data Dataset) error {
	f, err := excelize.OpenReader(bytes.NewReader(workbook))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	header := make([]string, 0, len(data.Columns)+1)
	for _, column := range data.Columns {
		header = append(header, column.Title)
	}
	header = append(header, "来源")
	for i, title := range header {
		axis, _ := excelize.CoordinatesToCellName(i+1, 1)
		actual, err := f.GetCellValue("BOM明细", axis)
		if err != nil || actual != title {
			return errors.New("exported header differs from source")
		}
	}
	for i, row := range data.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		values := make([]string, 0, len(row.Values)+1)
		for _, cell := range row.Values {
			values = append(values, display(cell))
		}
		values = append(values, row.Source)
		for j, value := range values {
			axis, _ := excelize.CoordinatesToCellName(j+1, i+2)
			actual, err := f.GetCellValue("BOM明细", axis)
			if err != nil || actual != value {
				return fmt.Errorf("export verification failed at %s", axis)
			}
			formula, err := f.GetCellFormula("BOM明细", axis)
			if err != nil || formula != "" {
				return errors.New("unexpected formula in exported data")
			}
		}
	}
	return nil
}
