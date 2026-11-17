package documentgen

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func templateFixture(t *testing.T, change func(*excelize.File)) ([]byte, TemplateLayout) {
	t.Helper()
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	require.NoError(t, f.SetCellStr("Sheet1", "A1", "合成模板"))
	require.NoError(t, f.SetCellStr("Sheet1", "A2", "图号"))
	require.NoError(t, f.SetCellStr("Sheet1", "C2", "名称"))
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 49})
	require.NoError(t, err)
	require.NoError(t, f.SetCellStyle("Sheet1", "A3", "C3", style))
	require.NoError(t, f.SetRowHeight("Sheet1", 3, 28))
	require.NoError(t, f.SetColWidth("Sheet1", "A", "C", 24))
	if change != nil {
		change(f)
	}
	b, err := f.WriteToBuffer()
	require.NoError(t, err)
	return b.Bytes(), TemplateLayout{Sheet: "Sheet1", HeaderRow: 2, StartRow: 3, Fields: []TemplateField{
		{Key: "part", Label: "图号", Column: "A"}, {Key: "name", Label: "名称", Column: "C"},
	}}
}

func TestCustomTemplateBindingsBoundRepeatedOutputText(t *testing.T) {
	l := TemplateLayout{Fields: []TemplateField{{Key: "a"}, {Key: "b"}}}
	d := Dataset{Columns: []Column{{ID: "source", Title: "合成字段"}}}
	for i := 0; i < 300; i++ {
		d.Rows = append(d.Rows, Row{
			Source: "synthetic", Values: []Cell{{Value: strings.Repeat("x", 30000), State: "value"}},
		})
	}
	require.ErrorContains(t, ValidateTemplateBindings(l, d, map[string]string{"a": "source", "b": "source"}, false),
		"output exceeds text budget")
}

func TestCustomTemplateRenderPreservesSourceStatesAndStyle(t *testing.T) {
	payload, layout := templateFixture(t, nil)
	d := Dataset{
		Partial: true, Issues: []string{"合成限制"},
		Columns: []Column{{ID: "a", Title: "图号"}, {ID: "b", Title: "名称"}, {ID: "c", Title: "名称"}},
		Rows: []Row{
			{Source: "synthetic.dwg/1", Values: []Cell{
				{Value: "=HYPERLINK(\"https://invalid.test\")", State: "value"},
				{State: "not_detected"},
				{Value: "独立来源列", State: "value"},
			}},
			{Source: "synthetic.dwg/2", Values: []Cell{
				{Value: "+cmd", State: "value"}, {State: "blank"}, {State: "not_applicable"},
			}},
		},
	}
	bindings := map[string]string{"part": "a", "name": "b"}
	b, err := RenderTemplate(context.Background(), payload, layout, d, bindings, true)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(b))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.Equal(t, []string{"Sheet1"}, f.GetSheetList())
	for sheet, cells := range map[string]map[string]string{
		"Sheet1": {
			"A1": "合成模板", "A3": d.Rows[0].Values[0].Value, "A4": "+cmd", "C3": display(d.Rows[0].Values[1]), "C4": "",
		},
	} {
		for axis, expected := range cells {
			value, err := f.GetCellValue(sheet, axis)
			require.NoError(t, err)
			require.Equal(t, expected, value)
			formula, err := f.GetCellFormula(sheet, axis)
			require.NoError(t, err)
			require.Empty(t, formula)
		}
	}
	s1, err := f.GetCellStyle("Sheet1", "A3")
	require.NoError(t, err)
	s2, err := f.GetCellStyle("Sheet1", "A4")
	require.NoError(t, err)
	require.Equal(t, s1, s2)
	h, err := f.GetRowHeight("Sheet1", 4)
	require.NoError(t, err)
	require.EqualValues(t, 28, h)
	w, err := f.GetColWidth("Sheet1", "A")
	require.NoError(t, err)
	require.EqualValues(t, 24, w)
	_, err = RenderTemplate(context.Background(), payload, layout, d, bindings, false)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = RenderTemplate(ctx, payload, layout, d, nil, true)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCustomTemplateAcceptsExplicitArchiveDirectories(t *testing.T) {
	payload, layout := templateFixture(t, nil)
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range []string{"_rels/", "docProps/", "xl/", "xl/_rels/", "xl/theme/", "xl/worksheets/"} {
		_, err := w.Create(name)
		require.NoError(t, err)
	}
	r, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)
	for _, part := range r.File {
		require.NoError(t, w.Copy(part))
	}
	require.NoError(t, w.Close())
	require.NoError(t, ValidateLayout(b.Bytes(), layout))
}

func TestCustomTemplatePreservesOriginalSheetsWithoutAddingAuditSheets(t *testing.T) {
	payload, layout := templateFixture(t, func(f *excelize.File) {
		for _, name := range []string{"封面", "生成说明", "源表明细", "源单元格状态"} {
			_, err := f.NewSheet(name)
			require.NoError(t, err)
			require.NoError(t, f.SetCellStr(name, "A1", "模板原有内容："+name))
		}
	})
	data := Dataset{Columns: []Column{{ID: "a", Title: "图号"}}, Rows: []Row{
		{Source: "synthetic", Values: []Cell{{Value: "001", State: "value"}}},
	}}
	output, err := RenderTemplate(context.Background(), payload, layout, data,
		map[string]string{"part": "a", "name": "a"}, false)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(output))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.Equal(t, []string{"Sheet1", "封面", "生成说明", "源表明细", "源单元格状态"}, f.GetSheetList())
	for _, name := range f.GetSheetList()[1:] {
		value, err := f.GetCellValue(name, "A1")
		require.NoError(t, err)
		require.Equal(t, "模板原有内容："+name, value)
	}
}

func TestCustomTemplateInspectionProvidesLiteralHeaderChoices(t *testing.T) {
	payload, _ := templateFixture(t, nil)
	inspection, err := InspectTemplate(payload)
	require.NoError(t, err)
	require.Len(t, inspection.HeaderCandidates, 2)
	header := inspection.HeaderCandidates[1]
	require.Equal(t, "Sheet1", header.Sheet)
	require.Equal(t, 2, header.HeaderRow)
	require.Equal(t, 3, header.StartRow)
	require.Equal(t, []TemplateField{
		{Key: "column_A", Label: "图号", Column: "A"},
		{Key: "column_C", Label: "名称", Column: "C"},
	}, header.Fields)
	require.NoError(t, ValidateLayout(payload, header))
	// A candidate is not permission to overwrite existing sample rows: the
	// confirmed layout is still checked by the existing backend validator.
	payload, _ = templateFixture(t, func(f *excelize.File) {
		require.NoError(t, f.SetCellStr("Sheet1", "A3", "synthetic old sample"))
	})
	require.Error(t, ValidateLayout(payload, header))
}

func TestCustomTemplateRejectsUnsafeArchiveDirectories(t *testing.T) {
	payload, _ := templateFixture(t, nil)
	for _, name := range []string{"../", "../xl/", "/xl/", "xl/../", "xl//", "xl\\worksheets/"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			_, err := w.Create(name)
			require.NoError(t, err)
			r, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
			require.NoError(t, err)
			for _, part := range r.File {
				require.NoError(t, w.Copy(part))
			}
			require.NoError(t, w.Close())
			_, err = InspectTemplate(b.Bytes())
			require.ErrorContains(t, err, "invalid template archive")
		})
	}
}

func withTemplateMetadata(t *testing.T, payload []byte, extension string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	r, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)
	for _, part := range r.File {
		if part.Name != "xl/workbook.xml" && part.Name != "_rels/.rels" && part.Name != "[Content_Types].xml" {
			require.NoError(t, w.Copy(part))
			continue
		}
		reader, err := part.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		text := string(content)
		switch part.Name {
		case "xl/workbook.xml":
			text = strings.Replace(text, "</workbook>", extension+"</workbook>", 1)
		case "_rels/.rels":
			custom := `<Relationship Id="rIdCustom" ` +
				`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/custom-properties" ` +
				`Target="docProps/custom.xml"/></Relationships>`
			text = strings.Replace(text, "</Relationships>", custom, 1)
		case "[Content_Types].xml":
			custom := `<Override PartName="/docProps/custom.xml" ` +
				`ContentType="application/vnd.openxmlformats-officedocument.custom-properties+xml"/></Types>`
			text = strings.Replace(text, "</Types>", custom, 1)
		}
		entry, err := w.Create(part.Name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(text))
		require.NoError(t, err)
	}
	entry, err := w.Create("docProps/custom.xml")
	require.NoError(t, err)
	custom := `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" ` +
		`xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
		`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="Synthetic">` +
		`<vt:lpwstr>synthetic-only</vt:lpwstr></property></Properties>`
	_, err = entry.Write([]byte(custom))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return b.Bytes()
}

func TestCustomTemplateAcceptsDocumentPropertiesAndCalculationCapabilities(t *testing.T) {
	payload, layout := templateFixture(t, nil)
	extension := `<extLst><ext uri="` + calculationFeaturesExtension + `"><calcFeatures xmlns="` +
		calculationFeaturesNamespace + `"><feature name="microsoft.com:RD"/>` +
		`<feature name="microsoft.com:LAMBDA_WF"/></calcFeatures></ext></extLst>`
	payload = withTemplateMetadata(t, payload, extension)
	require.NoError(t, ValidateLayout(payload, layout))
	data := Dataset{
		Columns: []Column{{ID: "a", Title: "图号"}, {ID: "b", Title: "名称"}},
		Rows: []Row{{Source: "synthetic", Values: []Cell{
			{Value: "S-001", State: "value"}, {Value: "Sample", State: "value"},
		}}},
	}
	bindings := map[string]string{"part": "a", "name": "b"}
	output, err := RenderTemplate(context.Background(), payload, layout, data, bindings, false)
	require.NoError(t, err)
	z, err := zip.NewReader(bytes.NewReader(output), int64(len(output)))
	require.NoError(t, err)
	var custom []byte
	for _, part := range z.File {
		if part.Name == "docProps/custom.xml" {
			r, err := part.Open()
			require.NoError(t, err)
			custom, err = io.ReadAll(r)
			require.NoError(t, err)
			require.NoError(t, r.Close())
		}
	}
	require.Contains(t, string(custom), "synthetic-only")
	for _, unsafe := range []string{
		strings.Replace(extension, calculationFeaturesExtension, "{UNKNOWN}", 1),
		strings.Replace(extension, "microsoft.com:RD", "unknown", 1),
		strings.Replace(extension, "</calcFeatures>", "<f>1+1</f></calcFeatures>", 1),
		strings.Replace(extension, "</calcFeatures>", "<unknown/></calcFeatures>", 1),
		strings.Replace(extension, `<feature name="microsoft.com:RD"/>`,
			`<feature name="microsoft.com:RD" extra="1"/>`, 1),
	} {
		_, err := InspectTemplate(withTemplateMetadata(t, mustTemplateBytes(t), unsafe))
		require.ErrorContains(t, err, "unsupported workbook extension")
	}
}

func TestCustomTemplateAcceptsOnlyInertDefaultSlicerStyle(t *testing.T) {
	extension := `<extLst xmlns="` + spreadsheetNamespace + `"><ext uri="{EB79DEF2-80B8-43e5-95BD-54CBDDF9020C}">` +
		`<slicerStyles xmlns="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main" ` +
		`defaultSlicerStyle="SlicerStyleLight1"/></ext></extLst>`
	check := func(text string) error {
		d := xml.NewDecoder(strings.NewReader(text))
		token, err := d.Token()
		require.NoError(t, err)
		return validateDefaultSlicerStyle(d, token.(xml.StartElement))
	}
	require.NoError(t, check(extension))
	for _, unsafe := range []string{
		strings.Replace(extension, "{EB79DEF2-80B8-43e5-95BD-54CBDDF9020C}", "{UNKNOWN}", 1),
		strings.Replace(extension, "SlicerStyleLight1", "CustomStyle", 1),
		strings.Replace(extension, `defaultSlicerStyle="SlicerStyleLight1"/>`,
			`defaultSlicerStyle="SlicerStyleLight1"><slicerStyle/></slicerStyles>`, 1),
		strings.Replace(extension, "</ext>", "<f>1+1</f></ext>", 1),
	} {
		require.Error(t, check(unsafe))
	}
}

func mustTemplateBytes(t *testing.T) []byte {
	payload, _ := templateFixture(t, nil)
	return payload
}

func TestCustomTemplateRejectsUnsafeAndUnsupportedWorkbooks(t *testing.T) {
	for name, change := range map[string]func(*excelize.File){
		"formula":     func(f *excelize.File) { require.NoError(t, f.SetCellFormula("Sheet1", "A1", "1+1")) },
		"sample rows": func(f *excelize.File) { require.NoError(t, f.SetCellStr("Sheet1", "A4", "old value")) },
		"footer":      func(f *excelize.File) { require.NoError(t, f.SetCellStr("Sheet1", "A100", "footer")) },
		"hidden sheet": func(f *excelize.File) {
			_, err := f.NewSheet("hidden")
			require.NoError(t, err)
			require.NoError(t, f.SetSheetVisible("hidden", false))
		},
		"hidden row": func(f *excelize.File) { require.NoError(t, f.SetRowVisible("Sheet1", 3, false)) },
		"external link": func(f *excelize.File) {
			require.NoError(t, f.SetCellHyperLink("Sheet1", "A1", "https://invalid.test", "External"))
		},
		"merged header":   func(f *excelize.File) { require.NoError(t, f.MergeCell("Sheet1", "A2", "B2")) },
		"sparse last row": func(f *excelize.File) { require.NoError(t, f.SetCellStr("Sheet1", "A1048576", "x")) },
	} {
		t.Run(name, func(t *testing.T) { p, l := templateFixture(t, change); require.Error(t, ValidateLayout(p, l)) })
	}
	p, l := templateFixture(t, nil)
	for _, edit := range []func(*TemplateLayout){
		func(l *TemplateLayout) { l.StartRow++ }, func(l *TemplateLayout) { l.Fields[1].Key = l.Fields[0].Key },
		func(l *TemplateLayout) { l.Fields[0].Label = "guess" }, func(l *TemplateLayout) { l.Fields[0].Column = "a" },
	} {
		cloned := l
		cloned.Fields = append([]TemplateField{}, l.Fields...)
		edit(&cloned)
		require.Error(t, ValidateLayout(p, cloned))
	}
	for _, part := range []string{
		"../evil.xml", "xl/vbaProject.bin", "customXml/item1.xml", "xl/worksheets/injected.xml",
	} {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		r, err := zip.NewReader(bytes.NewReader(p), int64(len(p)))
		require.NoError(t, err)
		for _, f := range r.File {
			require.NoError(t, w.Copy(f))
		}
		entry, err := w.Create(part)
		require.NoError(t, err)
		_, err = entry.Write([]byte(`<!DOCTYPE x [<!ENTITY a "boom">]><x/>`))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		_, err = InspectTemplate(b.Bytes())
		require.Error(t, err)
	}
}
