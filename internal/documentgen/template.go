package documentgen

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// TemplateField declares a destination column; source bindings belong to each generation.
type TemplateField struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Column string `json:"column"`
}

// TemplateLayout is a deliberately bounded layout: one empty row below a fixed header.
// Source evidence remains in the private dataset, not in added workbook sheets.
type TemplateLayout struct {
	Sheet     string          `json:"sheet"`
	HeaderRow int             `json:"header_row"`
	StartRow  int             `json:"start_row"`
	Fields    []TemplateField `json:"fields"`
}

// TemplateInspection provides a bounded configuration preview, not raw uploaded business rows.
type TemplateInspection struct {
	Sheets    []string `json:"sheets"`
	CellCount int      `json:"cell_count"`
	// Candidates describe literal cells, never inferred source bindings. They
	// are returned only to the draft's manager, who must confirm the header.
	HeaderCandidates []TemplateLayout `json:"header_candidates"`
}

const maxTemplateBytes = 10 << 20

// InspectTemplate validates OOXML before opening it. Unsupported active/external
// content is rejected instead of being silently stripped from the saved workbook.
func InspectTemplate(payload []byte) (TemplateInspection, error) {
	var result TemplateInspection
	if len(payload) == 0 || len(payload) > maxTemplateBytes {
		return result, errors.New("template must be a nonempty xlsx under 10 MiB")
	}
	z, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return result, errors.New("invalid or encrypted xlsx")
	}
	if len(z.File) > 500 {
		return result, errors.New("too many template parts")
	}
	var expanded uint64
	names := map[string]bool{}
	for _, part := range z.File {
		n := strings.ToLower(part.Name)
		isDir := strings.HasSuffix(n, "/")
		if isDir {
			n = strings.TrimSuffix(n, "/")
		}
		if n == "." || n == ".." || strings.HasPrefix(n, "../") ||
			path.Clean(n) != n || strings.HasPrefix(n, "/") || strings.Contains(n, "\\") || names[n] {
			return result, errors.New("invalid template archive")
		}
		names[n] = true
		// ZIP directory entries are optional. Excel writers may include them;
		// they carry no workbook content and must not be parsed as XML parts.
		if isDir {
			if part.UncompressedSize64 != 0 {
				return result, errors.New("template archive directory contains data")
			}
			continue
		}
		allowed := n == "[content_types].xml" || n == "_rels/.rels" ||
			n == "docprops/core.xml" || n == "docprops/app.xml" || n == "docprops/custom.xml" ||
			n == "xl/workbook.xml" || n == "xl/_rels/workbook.xml.rels" ||
			n == "xl/styles.xml" || n == "xl/sharedstrings.xml" ||
			strings.HasPrefix(n, "xl/theme/") || strings.HasPrefix(n, "xl/worksheets/")
		if !allowed {
			return result, fmt.Errorf("unsupported template package part: %.160s", part.Name)
		}
		if part.UncompressedSize64 > 10<<20 || expanded > (40<<20)-part.UncompressedSize64 {
			return result, errors.New("expanded template is too large")
		}
		expanded += part.UncompressedSize64
		for _, fragment := range []string{
			"vbaproject", "externallink", "connections", "querytables", "pivot", "embeddings/",
			"activex/", "ctrlprops/", "drawings/", "media/", "charts/", "comments", "vml", "customxml/",
			"macrosheets/", "dialogsheets/", "customui/", "printersettings/",
		} {
			if strings.Contains(n, fragment) {
				return result, fmt.Errorf("unsupported template feature: %s", fragment)
			}
		}
		if !strings.HasSuffix(n, ".xml") && !strings.HasSuffix(n, ".rels") {
			return result, errors.New("binary template parts are unsupported")
		}
		r, e := part.Open()
		if e != nil {
			return result, e
		}
		b, e := io.ReadAll(io.LimitReader(r, 10<<20+1))
		_ = r.Close()
		if e != nil {
			return result, e
		}
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			token, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return result, errors.New("invalid template XML")
			}
			if _, ok := token.(xml.Directive); ok {
				return result, errors.New("XML directives are not supported")
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			if strings.HasPrefix(n, "xl/worksheets/") && start.Name.Local == "c" {
				result.CellCount++
				for _, a := range start.Attr {
					if a.Name.Local == "r" {
						col, row, err := excelize.CellNameToCoordinates(a.Value)
						if err != nil || row > 20000 || col > 200 {
							return result, errors.New("template cell coordinates exceed budget")
						}
					}
				}
			}
			for _, a := range start.Attr {
				if (start.Name.Local == "dimension" || start.Name.Local == "mergeCell") && a.Name.Local == "ref" {
					for _, axis := range strings.Split(a.Value, ":") {
						col, row, err := excelize.CellNameToCoordinates(axis)
						if err != nil || row > 20000 || col > 200 {
							return result, errors.New("template range exceeds budget")
						}
					}
				}
				if start.Name.Local == "col" && (a.Name.Local == "min" || a.Name.Local == "max") {
					col, err := strconv.Atoi(a.Value)
					if err != nil || col < 1 || col > 200 {
						return result, errors.New("template column range exceeds budget")
					}
				}
				if a.Name.Local == "ContentType" && (strings.Contains(strings.ToLower(a.Value), "macro") ||
					strings.Contains(strings.ToLower(a.Value), "binary")) {
					return result, errors.New("active workbook content is unsupported")
				}
				if start.Name.Local == "row" && a.Name.Local == "r" {
					row, err := strconv.Atoi(a.Value)
					if err != nil || row < 1 || row > 20000 {
						return result, errors.New("template row exceeds budget")
					}
				}
				if (start.Name.Local == "row" || start.Name.Local == "col") &&
					a.Name.Local == "hidden" && (a.Value == "1" || a.Value == "true") {
					return result, errors.New("hidden rows or columns are unsupported")
				}
				if start.Name.Local == "sheet" && a.Name.Local == "state" && a.Value != "visible" {
					return result, errors.New("hidden sheets are unsupported")
				}
			}
			if result.CellCount > 20000 {
				return result, errors.New("template exceeds cell budget")
			}
			switch start.Name.Local {
			case "extLst":
				if start.Name.Space != spreadsheetNamespace {
					return result, errors.New("unsupported template XML feature: extLst")
				}
				var extensionErr error
				switch n {
				case "xl/workbook.xml":
					extensionErr = validateCalculationExtensions(d, start)
				case "xl/styles.xml":
					extensionErr = validateDefaultSlicerStyle(d, start)
				default:
					extensionErr = errors.New("unsupported template XML feature: extLst")
				}
				if extensionErr != nil {
					return result, extensionErr
				}
			case "f", "definedName", "sheetProtection", "workbookProtection", "oleObject", "legacyDrawing",
				"conditionalFormatting", "dataValidations", "autoFilter", "tableParts":
				return result, fmt.Errorf("unsupported template XML feature: %s", start.Name.Local)
			case "Relationship":
				for _, a := range start.Attr {
					if a.Name.Local == "Target" && (strings.Contains(a.Value, ":") ||
						strings.HasPrefix(a.Value, "//") || strings.Contains(a.Value, "\\")) {
						return result, errors.New("external relationship targets are not allowed")
					}
					if a.Name.Local == "TargetMode" && strings.EqualFold(a.Value, "external") {
						return result, errors.New("external relationships are not allowed")
					}
				}
			}
		}
	}
	f, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		return result, err
	}
	defer func() { _ = f.Close() }()
	result.Sheets = f.GetSheetList()
	if len(result.Sheets) == 0 || len(result.Sheets) > 20 {
		return result, errors.New("template requires 1–20 sheets")
	}
	for _, sheet := range result.Sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return result, err
		}
		candidates := []TemplateLayout{}
		for i, row := range rows {
			if i >= 999 {
				break
			}
			candidate := TemplateLayout{Sheet: sheet, HeaderRow: i + 1, StartRow: i + 2}
			unsupported := false
			for j, value := range row {
				if strings.TrimSpace(value) == "" {
					continue
				}
				if j >= 100 || !validText(value) {
					unsupported = true
					break
				}
				column, _ := excelize.ColumnNumberToName(j + 1)
				candidate.Fields = append(candidate.Fields, TemplateField{
					Key: "column_" + column, Label: value, Column: column,
				})
			}
			if unsupported || len(candidate.Fields) == 0 || len(candidate.Fields) > 50 {
				continue
			}
			candidates = append(candidates, candidate)
			// A bounded list of the last literal nonempty rows lets a user
			// distinguish a title from the actual header without typing coordinates.
			if len(candidates) > 20 {
				candidates = candidates[1:]
			}
		}
		result.HeaderCandidates = append(result.HeaderCandidates, candidates...)
	}
	return result, nil
}

const (
	spreadsheetNamespace         = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	calculationFeaturesNamespace = "http://schemas.microsoft.com/office/spreadsheetml/2018/calcfeatures"
	calculationFeaturesExtension = "{B58B0392-4F1F-4190-BB64-5DF3571DCE5F}"
)

// validateDefaultSlicerStyle accepts only an empty built-in style preference,
// not slicer objects, caches, custom styles or other extension content.
func validateDefaultSlicerStyle(d *xml.Decoder, root xml.StartElement) error {
	invalid := errors.New("unsupported style extension: only an empty default slicer style is supported")
	depth, extensions, styles := 0, 0, 0
	for _, a := range root.Attr {
		if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
			return invalid
		}
	}
	for {
		token, err := d.Token()
		if err != nil {
			return invalid
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			var attribute string
			switch depth {
			case 1:
				extensions++
				if extensions != 1 || value.Name != (xml.Name{Space: spreadsheetNamespace, Local: "ext"}) {
					return invalid
				}
				attribute = "uri"
			case 2:
				styles++
				if styles != 1 || value.Name != (xml.Name{
					Space: "http://schemas.microsoft.com/office/spreadsheetml/2009/9/main", Local: "slicerStyles",
				}) {
					return invalid
				}
				attribute = "defaultSlicerStyle"
			default:
				return invalid
			}
			seen := false
			for _, a := range value.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				if seen || a.Name.Space != "" || a.Name.Local != attribute {
					return invalid
				}
				if depth == 1 && !strings.EqualFold(a.Value, "{EB79DEF2-80B8-43e5-95BD-54CBDDF9020C}") {
					return invalid
				}
				if depth == 2 {
					valid := false
					for _, family := range []string{"Light", "Dark"} {
						for i := 1; i <= 6; i++ {
							if a.Value == fmt.Sprintf("SlicerStyle%s%d", family, i) {
								valid = true
							}
						}
					}
					if !valid {
						return invalid
					}
				}
				seen = true
			}
			if !seen {
				return invalid
			}
		case xml.EndElement:
			if depth == 0 {
				if value.Name != root.Name || extensions != 1 || styles != 1 {
					return invalid
				}
				return nil
			}
			depth--
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return invalid
			}
		case xml.Comment:
		default:
			return invalid
		}
	}
}

// validateCalculationExtensions accepts only Excel's inert supported-calculation
// declarations. It does not allow formulas or arbitrary OOXML extension content.
func validateCalculationExtensions(d *xml.Decoder, root xml.StartElement) error {
	invalid := errors.New("unsupported workbook extension: only calculation capability declarations are supported")
	depth, extensions, groups, features := 0, 0, 0, 0
	known := map[string]bool{
		"microsoft.com:RD": true, "microsoft.com:Single": true, "microsoft.com:FV": true,
		"microsoft.com:CNMTM": true, "microsoft.com:LET_WF": true,
		"microsoft.com:LAMBDA_WF": true, "microsoft.com:ARRAYTEXT_WF": true,
	}
	for _, a := range root.Attr {
		if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
			return invalid
		}
	}
	for {
		token, err := d.Token()
		if err != nil {
			return invalid
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			attribute, expected := "", ""
			switch depth {
			case 1:
				extensions++
				if extensions != 1 || value.Name != (xml.Name{Space: spreadsheetNamespace, Local: "ext"}) {
					return invalid
				}
				attribute, expected = "uri", calculationFeaturesExtension
			case 2:
				groups++
				if groups != 1 || value.Name != (xml.Name{Space: calculationFeaturesNamespace, Local: "calcFeatures"}) {
					return invalid
				}
			case 3:
				features++
				if features > 32 || value.Name != (xml.Name{Space: calculationFeaturesNamespace, Local: "feature"}) {
					return invalid
				}
				attribute = "name"
			default:
				return invalid
			}
			seen := false
			for _, a := range value.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				if attribute == "" || seen || a.Name.Space != "" || a.Name.Local != attribute {
					return invalid
				}
				if (depth == 1 && !strings.EqualFold(a.Value, expected)) || (depth == 3 && !known[a.Value]) {
					return invalid
				}
				seen = true
			}
			if attribute != "" && !seen {
				return invalid
			}
		case xml.EndElement:
			if depth == 0 {
				if value.Name != root.Name || extensions != 1 || groups != 1 || features == 0 {
					return invalid
				}
				return nil
			}
			depth--
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return invalid
			}
		case xml.Comment:
		default:
			return invalid
		}
	}
}

// ValidateLayout refuses ambiguous layouts and any old sample rows in the fill area.
func ValidateLayout(payload []byte, layout TemplateLayout) error {
	if _, err := InspectTemplate(payload); err != nil {
		return err
	}
	if layout.HeaderRow < 1 || layout.StartRow != layout.HeaderRow+1 || layout.StartRow > 1000 ||
		len(layout.Fields) == 0 || len(layout.Fields) > 50 {
		return errors.New("use a header and its immediately following empty detail row")
	}
	f, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	index, err := f.GetSheetIndex(layout.Sheet)
	if err != nil || index < 0 {
		return errors.New("unknown template sheet")
	}
	keys := map[string]bool{}
	cols := map[int]bool{}
	for _, field := range layout.Fields {
		col, err := excelize.ColumnNameToNumber(field.Column)
		if err != nil || col > 100 || col < 1 || keys[field.Key] || cols[col] || len(field.Key) > 64 ||
			strings.TrimSpace(field.Key) == "" || !validText(field.Key) ||
			field.Column != strings.ToUpper(field.Column) ||
			strings.TrimSpace(field.Label) == "" || !validText(field.Label) {
			return errors.New("invalid or duplicate template field")
		}
		keys[field.Key] = true
		cols[col] = true
		axis, _ := excelize.CoordinatesToCellName(col, layout.HeaderRow)
		value, err := f.GetCellValue(layout.Sheet, axis)
		if err != nil || value != field.Label {
			return errors.New("field label must exactly match the existing header cell")
		}
	}
	rows, err := f.GetRows(layout.Sheet)
	if err != nil {
		return err
	}
	for i := layout.StartRow - 1; i < len(rows); i++ {
		for _, v := range rows[i] {
			if v != "" {
				return errors.New("clear all sample data and footers below the header before configuring")
			}
		}
	}
	merges, err := f.GetMergeCells(layout.Sheet)
	if err != nil {
		return err
	}
	for _, merge := range merges {
		_, end, err := excelize.CellNameToCoordinates(merge.GetEndAxis())
		if err != nil || end >= layout.HeaderRow {
			return errors.New("merged header or detail cells are unsupported")
		}
	}
	tables, err := f.GetTables(layout.Sheet)
	if err != nil {
		return err
	}
	if len(tables) > 0 {
		return errors.New("remove existing structured tables from the fill sheet")
	}
	return nil
}

// ValidateTemplateBindings checks source bindings before a durable generation is reserved.
func ValidateTemplateBindings(layout TemplateLayout, data Dataset, bindings map[string]string, accept bool) error {
	if err := validate(data, accept); err != nil {
		return err
	}
	if len(bindings) != len(layout.Fields) {
		return errors.New("all template fields require explicit source bindings")
	}
	if len(data.Rows)*(len(data.Columns)+len(layout.Fields)) > 200000 {
		return errors.New("custom output exceeds cell budget")
	}
	index := map[string]int{}
	for i, col := range data.Columns {
		index[col.ID] = i
	}
	for _, field := range layout.Fields {
		source, ok := bindings[field.Key]
		_, exists := index[source]
		if !ok || !exists {
			return errors.New("source binding is not in this dataset")
		}
	}
	// Repeating a long source column into many destinations must not multiply
	// a valid source's bounded text into an unbounded workbook.
	textBytes := 0
	for _, row := range data.Rows {
		for _, field := range layout.Fields {
			textBytes += len(display(row.Values[index[bindings[field.Key]]]))
			if textBytes > 16<<20 {
				return errors.New("custom output exceeds text budget")
			}
		}
	}
	return nil
}

// RenderTemplate fills exact, user-confirmed column bindings without adding sheets.
// It never inserts/deletes rows or recalculates formulas, nor executes template code.
func RenderTemplate(ctx context.Context, payload []byte, layout TemplateLayout, data Dataset,
	bindings map[string]string, accept bool,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateTemplateBindings(layout, data, bindings, accept); err != nil {
		return nil, err
	}
	if err := ValidateLayout(payload, layout); err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i, col := range data.Columns {
		index[col.ID] = i
	}
	f, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	styles := map[string]int{}
	for _, field := range layout.Fields {
		axis := fmt.Sprintf("%s%d", field.Column, layout.StartRow)
		style, err := f.GetCellStyle(layout.Sheet, axis)
		if err != nil {
			return nil, err
		}
		styles[field.Key] = style
	}
	height, err := f.GetRowHeight(layout.Sheet, layout.StartRow)
	if err != nil {
		return nil, err
	}
	for i, row := range data.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, field := range layout.Fields {
			axis := fmt.Sprintf("%s%d", field.Column, layout.StartRow+i)
			if err := f.SetCellStr(layout.Sheet, axis, display(row.Values[index[bindings[field.Key]]])); err != nil {
				return nil, err
			}
			if err := f.SetCellStyle(layout.Sheet, axis, axis, styles[field.Key]); err != nil {
				return nil, err
			}
		}
		if err := f.SetRowHeight(layout.Sheet, layout.StartRow+i, height); err != nil {
			return nil, err
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	v, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = v.Close() }()
	actualSheets := v.GetSheetList()
	if len(actualSheets) != len(sheets) {
		return nil, errors.New("template sheet count changed")
	}
	for i, sheet := range sheets {
		if actualSheets[i] != sheet {
			return nil, errors.New("template sheet order changed")
		}
	}
	check := func(sheet, axis, expected string) error {
		value, err := v.GetCellValue(sheet, axis)
		if err != nil || value != expected {
			return errors.New("template value verification failed")
		}
		formula, err := v.GetCellFormula(sheet, axis)
		if err != nil || formula != "" {
			return errors.New("unexpected output formula")
		}
		return nil
	}
	for i, row := range data.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, field := range layout.Fields {
			if err := check(layout.Sheet, fmt.Sprintf("%s%d", field.Column, layout.StartRow+i),
				display(row.Values[index[bindings[field.Key]]])); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}
