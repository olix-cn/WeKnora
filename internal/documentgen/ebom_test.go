package documentgen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func ebomFixture() Dataset {
	d := Dataset{Drawings: []Drawing{
		{
			FileID: "root", Source: "root.dwg", Number: "ROOT", Kind: "assembly",
			QuantityBasis: "per_parent", Tables: []string{"r"},
		},
		{
			FileID: "sub", Source: "sub.dwg", Number: "SUB", Kind: "assembly",
			QuantityBasis: "per_parent", Tables: []string{"s"},
		},
		{FileID: "part", Source: "part.dwg", Number: "PART", Kind: "part", Revision: "A"},
	}}
	for _, table := range []string{"r", "s"} {
		for _, header := range []string{"代号", "名称", "数量"} {
			d.Columns = append(d.Columns, Column{
				ID: table + header, Title: table + header, Header: header, TableID: table, Confirmed: true,
			})
		}
	}
	for i, table := range []string{"r", "s"} {
		row := Row{Source: table, TableID: table, ID: table, Values: make([]Cell, 6)}
		for j := range row.Values {
			row.Values[j] = Cell{State: "not_applicable"}
		}
		code, qty := "SUB", "2"
		if i == 1 {
			code, qty = "PART", "1.25"
		}
		row.Values[i*3] = Cell{Value: code, State: "value"}
		row.Values[i*3+1] = Cell{Value: code, State: "value"}
		row.Values[i*3+2] = Cell{Value: qty, State: "value"}
		d.Rows = append(d.Rows, row)
	}
	return d
}

func TestEBOMReconstructsRootAndCalculatesExactQuantities(t *testing.T) {
	d := ebomFixture()
	output, report, err := BuildEBOM(d, EBOMOptions{ProductionCount: 3, OneDrawingSet: true, QuantityPerParent: true})
	require.NoError(t, err)
	require.Equal(t, "ROOT", report.RootNumber)
	require.Len(t, output.Rows, 2)
	get := func(row int, id string) string {
		for i, c := range output.Columns {
			if c.ID == id {
				return output.Rows[row].Values[i].Value
			}
		}
		return ""
	}
	require.Equal(t, "2", get(0, "ebom_single_quantity"))
	require.Equal(t, "6", get(0, "ebom_batch_quantity"))
	require.Equal(t, "2.5", get(1, "ebom_single_quantity"))
	require.Equal(t, "7.5", get(1, "ebom_batch_quantity"))
	require.Equal(t, "1.1", get(1, "ebom_sequence"))
	require.Equal(t, "SUB", get(1, "ebom_parent"))
	require.Equal(t, d.Rows[1].Values, output.Rows[1].Values[14:])
	require.False(t, output.Partial)
	bindings, err := EBOMBindings(TemplateLayout{Fields: []TemplateField{
		{Key: "single", Label: "单台数量"}, {Key: "batch", Label: "多台数量"},
	}})
	require.NoError(t, err)
	require.Equal(t, "ebom_single_quantity", bindings["single"])
	require.Equal(t, "ebom_batch_quantity", bindings["batch"])
	require.Equal(t, "single_quantity", EBOMField("数量"))
	_, err = EBOMBindings(TemplateLayout{Fields: []TemplateField{{Key: "unknown", Label: "任意运算"}}})
	require.Error(t, err)
}

func TestEBOMKeepsSharedPartsInSeparateAssemblyPaths(t *testing.T) {
	d := ebomFixture()
	// A second occurrence of SUB is a real engineering use, not a duplicate
	// drawing import. Its entire path must be expanded with its own multiplier.
	second := d.Rows[0]
	second.ID = "r2"
	second.Values = append([]Cell{}, second.Values...)
	second.Values[2].Value = "3"
	d.Rows = append(d.Rows, second)
	output, report, err := BuildEBOM(d, EBOMOptions{
		ProductionCount: 2, OneDrawingSet: true, QuantityPerParent: true,
	})
	require.NoError(t, err)
	require.Equal(t, 4, report.Occurrences)
	require.Equal(t, "2.5", output.Rows[1].Values[6].Value)
	require.Equal(t, "3.75", output.Rows[3].Values[6].Value)
	require.Equal(t, "7.5", output.Rows[3].Values[7].Value)
}

func TestEBOMFilenameIdentityRemainsPartialAndDoesNotGuessRelationships(t *testing.T) {
	d := ebomFixture()
	for i := range d.Drawings {
		d.Drawings[i].Source = d.Drawings[i].Number + " synthetic name.dwg"
		d.Drawings[i].Number = ""
		d.Drawings[i].Kind = ""
	}
	output, report, err := BuildEBOM(d, EBOMOptions{
		ProductionCount: 1, OneDrawingSet: true, QuantityPerParent: true,
	})
	require.NoError(t, err)
	require.True(t, output.Partial)
	require.True(t, report.Partial)
	require.Equal(t, "synthetic name", report.RootName)
	require.Equal(t, &AssemblyIdentity{Number: "ROOT", Name: "synthetic name"}, output.RootAssembly)
	require.Contains(t, report.Issues[0], "图号仅取自文件名首段")
}

func TestEBOMRootNamingPrefersSourceDrawingName(t *testing.T) {
	d := ebomFixture()
	d.Drawings[0].Name = "权威总成名称"
	d.Drawings[0].Source = `D:\synthetic\ROOT 文件名名称.dwg`
	out, report, err := BuildEBOM(d, EBOMOptions{
		ProductionCount: 1, OneDrawingSet: true, QuantityPerParent: true,
	})
	require.NoError(t, err)
	require.Equal(t, "权威总成名称", report.RootName)
	require.Equal(t, "权威总成名称", out.RootAssembly.Name)
	d.Drawings[0].Name = ""
	out, _, err = BuildEBOM(d, EBOMOptions{ProductionCount: 1, OneDrawingSet: true, QuantityPerParent: true})
	require.NoError(t, err)
	require.Equal(t, "文件名名称", out.RootAssembly.Name)
}

func TestEBOMRejectsConflictingPartIdentity(t *testing.T) {
	d := ebomFixture()
	extra := d.Rows[0]
	extra.ID = "conflicting-use"
	extra.Values = append([]Cell{}, extra.Values...)
	extra.Values[1].Value = "different specification"
	d.Rows = append(d.Rows, extra)
	_, _, err := BuildEBOM(d, EBOMOptions{ProductionCount: 1, OneDrawingSet: true, QuantityPerParent: true})
	require.ErrorContains(t, err, "conflicting name")
}

func TestEBOMRejectsAmbiguousAndUnsafeAssemblySets(t *testing.T) {
	for name, change := range map[string]func(*Dataset){
		"duplicate revisions": func(d *Dataset) { d.Drawings = append(d.Drawings, d.Drawings[0]) },
		"cycle":               func(d *Dataset) { d.Rows[1].Values[3].Value = "ROOT" },
		"multiple roots":      func(d *Dataset) { d.Rows[0].Values[0].Value = "OTHER" },
		"unreachable part": func(d *Dataset) {
			d.Drawings = append(d.Drawings, Drawing{Number: "ORPHAN", Kind: "part"})
		},
		"invalid quantity": func(d *Dataset) { d.Rows[0].Values[2].Value = "2件" },
		"unknown quantity": func(d *Dataset) {
			d.Rows[0].Values[2] = Cell{State: "not_detected"}
			d.Partial = true
			d.Issues = []string{"missing"}
		},
		"wrong quantity basis": func(d *Dataset) { d.Drawings[0].QuantityBasis = "per_product" },
	} {
		t.Run(name, func(t *testing.T) {
			d := ebomFixture()
			change(&d)
			_, _, err := BuildEBOM(d, EBOMOptions{ProductionCount: 1, OneDrawingSet: true, QuantityPerParent: true})
			require.Error(t, err)
		})
	}
	_, _, err := BuildEBOM(ebomFixture(), EBOMOptions{ProductionCount: 1})
	require.Error(t, err)
}
