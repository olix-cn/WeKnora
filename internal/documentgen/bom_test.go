package documentgen

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func sample() Dataset {
	return Dataset{
		Columns: []Column{{ID: "number", Title: "图号"}, {ID: "name", Title: "名称"}},
		Rows: []Row{{Source: "synthetic.dwg", Values: []Cell{
			{Value: "000123", State: "value"}, {Value: "=HYPERLINK(\"https://example.invalid\")", State: "value"},
		}}},
	}
}

func TestBOMPreservesTextAndNeverCreatesFormulas(t *testing.T) {
	data := sample()
	for _, text := range []string{"=1+1", "+123", "-123", "@SUM(A1)", "中文长名称"} {
		data.Rows = append(data.Rows, Row{Source: "synthetic.dwg", Values: []Cell{
			{Value: "0001", State: "value"}, {Value: text, State: "value"},
		}})
	}
	output, err := RenderBOM(context.Background(), data, false)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(output))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	value, err := f.GetCellValue("BOM明细", "A2")
	require.NoError(t, err)
	require.Equal(t, "000123", value)
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		require.NoError(t, err)
		for i, row := range rows {
			for j := range row {
				axis, _ := excelize.CoordinatesToCellName(j+1, i+1)
				formula, err := f.GetCellFormula(sheet, axis)
				require.NoError(t, err)
				require.Empty(t, formula)
			}
		}
	}
}

func TestBOMPartialRequiresAcceptanceAndRetainsState(t *testing.T) {
	data := sample()
	data.Partial = true
	data.Issues = []string{"一格未检测，一列语义待核实"}
	data.Rows[0].Values[0] = Cell{State: "not_detected"}
	data.Rows[0].Values[1] = Cell{State: "ambiguous"}
	_, err := RenderBOM(context.Background(), data, false)
	require.Error(t, err)
	output, err := RenderBOM(context.Background(), data, true)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(output))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows("单元格状态")
	require.NoError(t, err)
	require.Equal(t, "not_detected", rows[1][2])
	require.Equal(t, "ambiguous", rows[2][2])
	note, err := f.GetCellValue("读取说明", "A1")
	require.NoError(t, err)
	require.Contains(t, note, "部分数据")
}

func TestBOMRejectsInvalidInput(t *testing.T) {
	for _, mutate := range []func(*Dataset){
		func(d *Dataset) { d.Columns[1].ID = d.Columns[0].ID },
		func(d *Dataset) { d.Rows[0].Values = nil },
		func(d *Dataset) { d.Rows[0].Source = "" },
		func(d *Dataset) { d.Rows[0].Values[0] = Cell{State: "not_detected"} },
		func(d *Dataset) { d.Rows[0].Values[0] = Cell{State: "blank", Value: "0"} },
		func(d *Dataset) { d.Rows[0].Values[0].Value = "bad\x00text" },
		func(d *Dataset) { d.Rows[0].Values[0].State = "invented" },
		func(d *Dataset) { d.Rows[0].Values[0].Value = strings.Repeat("a", 32768) },
		func(d *Dataset) { d.Partial = true },
	} {
		data := sample()
		mutate(&data)
		_, err := RenderBOM(context.Background(), data, true)
		require.Error(t, err)
	}
}

func TestBOMRejectsExportBudgetOverflow(t *testing.T) {
	data := sample()
	data.Rows = make([]Row, 10001)
	_, err := RenderBOM(context.Background(), data, false)
	require.ErrorContains(t, err, "dimensions")
	data = sample()
	data.Issues = make([]string, 1000)
	for i := range data.Issues {
		data.Issues[i] = strings.Repeat("a", 32767)
	}
	_, err = RenderBOM(context.Background(), data, false)
	require.ErrorContains(t, err, "budget")
}

func TestBOMConfirmedBlankAndEmptyDataset(t *testing.T) {
	data := sample()
	data.Rows[0].Values[1] = Cell{State: "blank"}
	_, err := RenderBOM(context.Background(), data, false)
	require.NoError(t, err)
	data.Rows = nil
	_, err = RenderBOM(context.Background(), data, false)
	require.NoError(t, err)
}

func TestBOMHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RenderBOM(ctx, sample(), false)
	require.ErrorIs(t, err, context.Canceled)
}
