package documentgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

type bomPage struct {
	ResultID         string      `json:"result_id"`
	Mode             string      `json:"mode"`
	SourceKind       string      `json:"source_kind"`
	Native           bool        `json:"native_bom_database_read"`
	Expires          json.Number `json:"expires_at"`
	Status           string      `json:"status"`
	Complete         bool        `json:"complete"`
	RowCount         int         `json:"row_count"`
	RowCountComplete bool        `json:"row_count_complete"`
	Unread           int         `json:"unread_file_count"`
	Unresolved       int         `json:"unresolved_header_count"`
	FilesWithout     int         `json:"files_without_parts_list"`
	Selection        struct {
		Complete bool `json:"complete"`
		Covers   bool `json:"scope_covers_all_files"`
	} `json:"selection"`
	Page struct {
		Total    int  `json:"total"`
		Offset   int  `json:"offset"`
		Returned int  `json:"returned"`
		HasMore  bool `json:"has_more"`
		Next     *int `json:"next_offset"`
	} `json:"page"`
	Records []json.RawMessage `json:"records"`
	Rows    []struct {
		ID     string             `json:"row_id"`
		Table  string             `json:"table_id"`
		Values map[string]*string `json:"values"`
		States map[string]string  `json:"states"`
	} `json:"rows"`
}

type schemaRecord struct {
	DrawingName      string   `json:"drawing_name"`
	DrawingNumber    string   `json:"drawing_number"`
	Revision         string   `json:"revision"`
	DocumentKind     string   `json:"document_kind"`
	QuantityBasis    string   `json:"quantity_basis"`
	Kind             string   `json:"kind"`
	FileID           string   `json:"file_id"`
	Source           string   `json:"source"`
	TableID          string   `json:"table_id"`
	ColumnID         string   `json:"column_id"`
	Position         int      `json:"position"`
	Raw              string   `json:"raw"`
	HeaderPath       []string `json:"header_path"`
	State            string   `json:"state"`
	Status           string   `json:"status"`
	Layout           string   `json:"layout"`
	Handle           string   `json:"handle"`
	SchemaComplete   bool     `json:"schema_complete"`
	PhysicalComplete bool     `json:"physical_columns_enumerated"`
	RowCount         int      `json:"row_count"`
	RowCountKnown    bool     `json:"row_count_known"`
}

// ImportBOM imports only complete, persisted structured MCP pages. No tool is
// invoked and neither model text nor browser-submitted cells are trusted inputs.
// Separate table column IDs are intentionally not merged by matching titles.
func ImportBOM(steps []types.AgentStep, resultID string) (Dataset, error) {
	var empty Dataset
	if resultID == "" || len(resultID) > 128 {
		return empty, errors.New("invalid result ID")
	}
	pages := map[string]map[int]bomPage{"schema": {}, "data": {}}
	bytesTotal := 0
	origin := ""
	originSet := false
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			name := call.Name
			if call.Target != nil {
				name = call.Target.ToolName
			}
			if name != "drawing_bom_extract" && !strings.HasPrefix(name, "mcp_cad_assistant_drawing_bom_extract_") {
				continue
			}
			if call.Result == nil || !call.Result.Success {
				continue
			}
			if call.ExecutionArgs()["columns_mode"] != "all" {
				continue // A selected-column projection cannot stand in for the original table.
			}
			structured := call.Result.Data["structured_content"]
			if structured == nil {
				continue
			}
			b, err := json.Marshal(structured)
			if err != nil {
				return empty, errors.New("invalid structured BOM result")
			}
			var page bomPage
			if err := json.Unmarshal(b, &page); err != nil {
				continue
			}
			if page.ResultID != resultID || pages[page.Mode] == nil {
				continue
			}
			service := ""
			if call.Target != nil {
				service = call.Target.ServiceName
			}
			if originSet && service != origin {
				return empty, errors.New("mixed MCP service results")
			}
			origin = service
			originSet = true
			bytesTotal += len(b)
			if bytesTotal > 16<<20 {
				return empty, errors.New("BOM pages exceed import budget")
			}
			if page.SourceKind != "saved_file" || page.Native {
				return empty, errors.New("unsupported BOM source")
			}
			if old, exists := pages[page.Mode][page.Page.Offset]; exists && !reflect.DeepEqual(old, page) {
				return empty, errors.New("conflicting pages for the same snapshot")
			}
			pages[page.Mode][page.Page.Offset] = page
		}
	}
	schema, err := orderedPages(pages["schema"], true)
	if err != nil {
		return empty, err
	}
	data, err := orderedPages(pages["data"], false)
	if err != nil {
		return empty, err
	}
	first := data[0]
	if first.Expires == "" || len(schema) == 0 {
		return empty, errors.New("snapshot metadata is missing")
	}
	if !first.Selection.Complete || !first.Selection.Covers || !first.RowCountComplete || first.Unread != 0 {
		return empty, errors.New("file or row selection was not fully read")
	}
	if first.RowCount != first.Page.Total {
		return empty, errors.New("BOM row totals disagree")
	}
	if first.RowCount < 0 || first.RowCount > 10000 {
		return empty, errors.New("unsupported BOM row count")
	}
	files := map[string]string{}
	drawings := map[string]Drawing{}
	fileNotes := []string{}
	tables := map[string]schemaRecord{}
	columns := map[string]schemaRecord{}
	columnOrder := []string{}
	for _, page := range schema {
		if page.Expires != first.Expires {
			return empty, errors.New("snapshot identity differs across pages")
		}
		for _, record := range page.Records {
			var r schemaRecord
			if err := json.Unmarshal(record, &r); err != nil {
				return empty, err
			}
			switch r.Kind {
			case "file":
				if r.FileID == "" || r.Source == "" || files[r.FileID] != "" {
					return empty, errors.New("invalid file identity")
				}
				files[r.FileID] = r.Source
				drawings[r.FileID] = Drawing{
					Name:   r.DrawingName,
					FileID: r.FileID, Source: r.Source, Number: r.DrawingNumber,
					Revision: r.Revision, Kind: r.DocumentKind, QuantityBasis: r.QuantityBasis,
				}
				fileNotes = append(fileNotes, fmt.Sprintf("源文件：%s；读取状态：%s", r.Source, r.Status))
			case "table":
				if r.TableID == "" || !r.SchemaComplete || !r.PhysicalComplete || !r.RowCountKnown || r.RowCount < 0 {
					return empty, errors.New("table schema is incomplete")
				}
				if _, found := tables[r.TableID]; found {
					return empty, errors.New("duplicate table")
				}
				tables[r.TableID] = r
			case "column":
				if r.ColumnID == "" || r.TableID == "" || r.Position < 1 {
					return empty, errors.New("invalid physical column")
				}
				if _, found := columns[r.ColumnID]; found {
					return empty, errors.New("duplicate column")
				}
				columns[r.ColumnID] = r
				columnOrder = append(columnOrder, r.ColumnID)
			}
		}
	}
	// Stable per-table order without conflating homonymous columns across drawings.
	sort.SliceStable(columnOrder, func(i, j int) bool {
		a, b := columns[columnOrder[i]], columns[columnOrder[j]]
		if a.TableID != b.TableID {
			return a.TableID < b.TableID
		}
		return a.Position < b.Position
	})
	out := Dataset{Partial: !first.Complete || first.Status != "complete" ||
		first.FilesWithout > 0 || first.Unresolved > 0}
	fileIDs := make([]string, 0, len(drawings))
	for id := range drawings {
		fileIDs = append(fileIDs, id)
	}
	sort.Strings(fileIDs)
	for _, id := range fileIDs {
		d := drawings[id]
		for tid, t := range tables {
			if t.FileID == id {
				d.Tables = append(d.Tables, tid)
			}
		}
		sort.Strings(d.Tables)
		out.Drawings = append(out.Drawings, d)
	}
	index := map[string]int{}
	for _, id := range columnOrder {
		c := columns[id]
		table, found := tables[c.TableID]
		if !found || files[table.FileID] == "" {
			return empty, errors.New("column source is missing")
		}
		title := strings.Join(c.HeaderPath, " / ")
		if title == "" {
			title = c.Raw
		}
		if title == "" {
			title = "未命名列"
		}
		if c.State != "confirmed" {
			title += "（待核实）"
			out.Partial = true
		}
		index[id] = len(out.Columns)
		out.Columns = append(out.Columns, Column{
			ID: id, Title: fmt.Sprintf("%s · %s", table.Handle, title),
			TableID: c.TableID, Header: strings.Join(c.HeaderPath, " / "), Confirmed: c.State == "confirmed",
		})
		if out.Columns[len(out.Columns)-1].Header == "" {
			out.Columns[len(out.Columns)-1].Header = c.Raw
		}
	}
	if len(out.Columns) == 0 || len(out.Columns) > 100 {
		return empty, errors.New("unsupported physical column count")
	}
	if first.RowCount*len(out.Columns) > 100000 {
		return empty, errors.New("too many imported BOM cells")
	}
	positions := map[string]map[int]bool{}
	for _, column := range columns {
		if positions[column.TableID] == nil {
			positions[column.TableID] = map[int]bool{}
		}
		if positions[column.TableID][column.Position] {
			return empty, errors.New("duplicate physical column position")
		}
		positions[column.TableID][column.Position] = true
	}
	for id := range tables {
		if len(positions[id]) == 0 {
			return empty, errors.New("table columns are missing")
		}
		for position := 1; position <= len(positions[id]); position++ {
			if !positions[id][position] {
				return empty, errors.New("physical column gap")
			}
		}
	}
	seenRows := map[string]bool{}
	tableRows := map[string]int{}
	unknownCells := 0
	for _, page := range data {
		if page.Expires != first.Expires || page.RowCount != first.RowCount || page.Status != first.Status ||
			page.Complete != first.Complete || page.Unread != 0 || !page.Selection.Covers || !page.Selection.Complete {
			return empty, errors.New("inconsistent snapshot data pages")
		}
		for _, row := range page.Rows {
			table, found := tables[row.Table]
			if !found || files[table.FileID] == "" || row.ID == "" || seenRows[row.Table+":"+row.ID] {
				return empty, errors.New("invalid BOM row identity")
			}
			seenRows[row.Table+":"+row.ID] = true
			tableRows[row.Table]++
			r := Row{
				TableID: row.Table, ID: row.ID,
				Source: files[table.FileID] + " | " + table.Layout + " | " + table.Handle,
				Values: make([]Cell, len(out.Columns)),
			}
			// Other source tables' columns are explicitly not applicable, not unknown.
			for i := range r.Values {
				r.Values[i] = Cell{State: "not_applicable"}
			}
			for id, col := range columns {
				if col.TableID != row.Table {
					continue
				}
				v, exists := row.Values[id]
				if !exists {
					return empty, errors.New("physical BOM column is missing")
				}
				state := row.States[id]
				if state == "not_applicable" {
					return empty, errors.New("source cell is unexpectedly not applicable")
				}
				cell := Cell{State: state}
				if v != nil {
					cell.Value = *v
					if state == "" {
						cell.State = "value"
					}
				}
				if v == nil && state == "" {
					return empty, errors.New("null cell has no state")
				}
				if cell.State == "not_detected" || cell.State == "ambiguous" {
					out.Partial = true
					unknownCells++
				}
				r.Values[index[id]] = cell
			}
			for id := range row.Values {
				if columns[id].TableID != row.Table {
					return empty, errors.New("unrecognized row column")
				}
			}
			for id := range row.States {
				if columns[id].TableID != row.Table {
					return empty, errors.New("unrecognized cell state")
				}
			}
			out.Rows = append(out.Rows, r)
		}
	}
	for id, table := range tables {
		if tableRows[id] != table.RowCount {
			return empty, errors.New("table row count disagrees with schema")
		}
	}
	out.Issues = []string{
		"数据源为会话中保存的 MCP 显示表快照；不含原生 BOM 数据库或编辑器未保存状态。",
		"不同表的物理列保留独立身份，未合并同名列；其他来源表的列不适用。",
	}
	out.Issues = append(out.Issues, fileNotes...)
	if unknownCells > 0 {
		out.Issues = append(out.Issues, fmt.Sprintf("%d 个单元格未检测或有歧义，未补为零或真实空白。", unknownCells))
	}
	if first.FilesWithout > 0 {
		out.Issues = append(out.Issues, fmt.Sprintf("%d 个文件未发现已保存明细表，不代表原生数据库为空。", first.FilesWithout))
	}
	if first.Unresolved > 0 {
		out.Issues = append(out.Issues, fmt.Sprintf("%d 个列语义尚未解决。", first.Unresolved))
	}
	if out.Partial {
		out.Issues = append(out.Issues, "部分数据：保留未检测单元格、歧义表头及无明细表文件的原始限制。")
	}
	if err := Validate(out); err != nil {
		return empty, err
	}
	return out, nil
}

func orderedPages(pages map[int]bomPage, schema bool) ([]bomPage, error) {
	if len(pages) == 0 || len(pages) > 1000 {
		return nil, errors.New("complete schema and data pages are required")
	}
	first, ok := pages[0]
	if !ok {
		return nil, errors.New("first BOM page is missing")
	}
	if first.Page.Total < 0 || first.Page.Total > 100000 {
		return nil, errors.New("invalid BOM page total")
	}
	out := []bomPage{}
	offset := 0
	for {
		p, found := pages[offset]
		if !found || p.Page.Total != first.Page.Total {
			return nil, errors.New("BOM page gap")
		}
		n := len(p.Rows)
		if schema {
			n = len(p.Records)
		}
		if p.Page.Returned != n || offset+n > p.Page.Total {
			return nil, errors.New("invalid page length")
		}
		out = append(out, p)
		if !p.Page.HasMore {
			if offset+n != p.Page.Total || p.Page.Next != nil {
				return nil, errors.New("incomplete terminal page")
			}
			break
		}
		if n == 0 || p.Page.Next == nil || *p.Page.Next != offset+n {
			return nil, errors.New("invalid page cursor")
		}
		offset = *p.Page.Next
	}
	if len(out) != len(pages) {
		return nil, errors.New("overlapping BOM pages")
	}
	return out, nil
}
