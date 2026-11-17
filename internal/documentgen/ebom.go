package documentgen

import (
	"errors"
	"fmt"
	"math/big"
	"path"
	"sort"
	"strings"
)

// EBOMOptions contains explicit user choices, not model-supplied cell values.
type EBOMOptions struct {
	ProductionCount   int  `json:"production_count"`
	OneDrawingSet     bool `json:"one_drawing_set"`
	QuantityPerParent bool `json:"quantity_per_parent"`
}

// EBOMReport records why a root and computed quantities were selected.
type EBOMReport struct {
	RootName        string   `json:"root_name"`
	RootNumber      string   `json:"root_number"`
	ProductionCount int      `json:"production_count"`
	Occurrences     int      `json:"occurrences"`
	Partial         bool     `json:"partial"`
	Issues          []string `json:"issues"`
}

var ebomHeaders = map[string][]string{
	"number":       {"图号", "代号", "零件代号", "物料编码", "物料代号", "编号", "partnumber", "drawingnumber"},
	"name":         {"名称", "零件名称", "物料名称", "name"},
	"quantity":     {"数量", "单套数量", "本级数量", "quantity", "qty"},
	"material":     {"材料", "材质", "material"},
	"unit_weight":  {"单重", "单件重量", "unitweight"},
	"total_weight": {"总重", "总重量", "totalweight"},
	"remark":       {"备注", "remark", "remarks"},
	"unit":         {"单位", "unit"},
}

func headerKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

// EBOMField maps a published template's literal header to a bounded output
// meaning. Unsupported or ambiguous headers are rejected, never guessed.
func EBOMField(label string) string {
	key := headerKey(label)
	for field, labels := range ebomHeaders {
		for _, label := range labels {
			if key == label {
				if field == "quantity" {
					return "single_quantity" // template quantity means per finished product, not local parent usage
				}
				return field
			}
		}
	}
	for field, labels := range map[string][]string{
		"sequence": {"序号", "序", "项次"}, "level": {"层级", "级别"}, "parent": {"父项", "父项代号", "上级图号"},
		"single_quantity": {"单台数量", "单台用量", "每台数量"},
		"batch_quantity":  {"多台数量", "总数量", "批量数量"},
		"revision":        {"版本", "版次"},
	} {
		for _, label := range labels {
			if key == label {
				return field
			}
		}
	}
	return ""
}

func decimalQuantity(value string) (*big.Rat, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 24 {
		return nil, errors.New("quantity must be a positive decimal")
	}
	dots, digits := 0, 0
	for _, r := range value {
		if r == '.' {
			dots++
		} else if r >= '0' && r <= '9' {
			digits++
		} else {
			return nil, errors.New("quantity must be a positive decimal without units")
		}
	}
	if dots > 1 || digits == 0 {
		return nil, errors.New("invalid decimal quantity")
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() <= 0 {
		return nil, errors.New("quantity must be positive")
	}
	return r, nil
}

func decimalText(value *big.Rat) (string, error) {
	if value.Num().BitLen() > 192 || value.Denom().BitLen() > 192 {
		return "", errors.New("EBOM calculation exceeds numeric budget")
	}
	den := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	digits := 0
	for _, factor := range []*big.Int{two, five} {
		count := 0
		for new(big.Int).Mod(den, factor).Sign() == 0 {
			den.Div(den, factor)
			count++
		}
		if count > digits {
			digits = count
		}
	}
	if den.Cmp(big.NewInt(1)) != 0 || digits > 24 {
		return "", errors.New("EBOM decimal precision exceeds budget")
	}
	text := value.FloatString(digits)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text, nil
}

// BuildEBOM reconstructs a hierarchical engineering BOM from exact identities.
// It never sums same-named parts or infers an assembly by its filename wording.
// Missing authoritative identity/type information is explicitly partial.
func BuildEBOM(data Dataset, options EBOMOptions) (Dataset, EBOMReport, error) {
	var output Dataset
	report := EBOMReport{ProductionCount: options.ProductionCount}
	if !options.OneDrawingSet || !options.QuantityPerParent ||
		options.ProductionCount < 1 || options.ProductionCount > 1000000 {
		return output, report, errors.New(
			"confirm one drawing set, per-parent source quantities and a production count from 1 to 1000000")
	}
	if err := validate(data, true); err != nil {
		return output, report, err
	}
	if len(data.Drawings) == 0 || len(data.Drawings) > 100 {
		return output, report, errors.New("drawing identities are unavailable; reread full MCP schema/data")
	}
	issues := append([]string{}, data.Issues...)
	partial := data.Partial
	drawings := map[string]Drawing{}
	tableOwner := map[string]string{}
	for _, drawing := range data.Drawings {
		number := strings.TrimSpace(drawing.Number)
		if number == "" {
			base := path.Base(strings.ReplaceAll(drawing.Source, "\\", "/"))
			base = strings.TrimSuffix(base, path.Ext(base))
			parts := strings.Fields(base)
			if len(parts) == 0 {
				return output, report, errors.New("drawing number is missing")
			}
			number = parts[0]
			partial = true
			issues = append(issues, "图号仅取自文件名首段，未经标题栏／原生 BOM 身份独立验证："+number)
		}
		if _, exists := drawings[number]; exists {
			return output, report, fmt.Errorf("duplicate drawing number or revision conflict: %s", number)
		}
		if len(drawing.Tables) > 1 {
			return output, report, fmt.Errorf("multiple detail tables require an explicit source selection: %s", number)
		}
		if drawing.Kind != "assembly" && drawing.Kind != "part" {
			partial = true
			issues = append(issues, "图纸未提供明确的总成／零件类型，无法证明无缺失子图："+number)
		}
		if drawing.QuantityBasis != "" && drawing.QuantityBasis != "per_parent" {
			return output, report, errors.New("source quantities are not per-parent quantities")
		}
		if len(drawing.Tables) > 0 && drawing.QuantityBasis == "" {
			partial = true
			issues = append(issues, "源表缺少数量口径声明，仅按用户确认的每直接父项用量计算："+number)
		}
		if drawing.Kind == "part" && len(drawing.Tables) > 0 {
			return output, report, errors.New("part identity conflicts with assembly detail table")
		}
		if drawing.Kind == "assembly" && len(drawing.Tables) == 0 {
			return output, report, fmt.Errorf("assembly drawing has no readable detail table: %s", number)
		}
		drawing.Number = number
		drawings[number] = drawing
		for _, table := range drawing.Tables {
			if table == "" {
				return output, report, errors.New("drawing table identity is empty")
			}
			if tableOwner[table] != "" {
				return output, report, errors.New("duplicate table owner")
			}
			tableOwner[table] = number
		}
	}
	fields := map[string]map[string]int{}
	for i, column := range data.Columns {
		if column.TableID == "" || tableOwner[column.TableID] == "" {
			return output, report, errors.New("source column lacks table identity")
		}
		if fields[column.TableID] == nil {
			fields[column.TableID] = map[string]int{}
		}
		if !column.Confirmed {
			continue
		}
		meaning := ""
		for field, aliases := range ebomHeaders {
			for _, alias := range aliases {
				if headerKey(column.Header) == alias {
					meaning = field
				}
			}
		}
		if meaning == "" {
			continue
		}
		if _, exists := fields[column.TableID][meaning]; exists {
			return output, report, fmt.Errorf("ambiguous source field: %s", meaning)
		}
		fields[column.TableID][meaning] = i
	}
	get := func(row Row, field string) Cell {
		if index, ok := fields[row.TableID][field]; ok {
			return row.Values[index]
		}
		return Cell{State: "not_applicable"}
	}
	children := map[string][]Row{}
	parents := map[string]int{}
	identityAttributes := map[string]map[string]string{}
	for _, row := range data.Rows {
		owner := tableOwner[row.TableID]
		if owner == "" {
			return output, report, errors.New("row table identity is missing")
		}
		code, qty := get(row, "number"), get(row, "quantity")
		if code.State != "value" || strings.TrimSpace(code.Value) == "" || qty.State != "value" {
			return output, report, errors.New("confirmed part number and quantity are required for every detail row")
		}
		if _, err := decimalQuantity(qty.Value); err != nil {
			return output, report, fmt.Errorf("invalid quantity in %s: %w", owner, err)
		}
		partNumber := strings.TrimSpace(code.Value)
		if identityAttributes[partNumber] == nil {
			identityAttributes[partNumber] = map[string]string{}
		}
		for _, field := range []string{"name", "material", "unit"} {
			cell := get(row, field)
			if cell.State != "value" || strings.TrimSpace(cell.Value) == "" {
				continue
			}
			if before := identityAttributes[partNumber][field]; before != "" && before != cell.Value {
				return output, report, fmt.Errorf("same part number has conflicting %s: %s", field, partNumber)
			}
			identityAttributes[partNumber][field] = cell.Value
		}
		children[owner] = append(children[owner], row)
		if _, exists := drawings[strings.TrimSpace(code.Value)]; exists {
			parents[strings.TrimSpace(code.Value)]++
		}
	}
	// Detect cycles globally before choosing a root, including disconnected cycles.
	visiting, visited := map[string]bool{}, map[string]bool{}
	var checkCycle func(string) error
	checkCycle = func(number string) error {
		if visiting[number] {
			return errors.New("assembly cycle detected")
		}
		if visited[number] {
			return nil
		}
		visiting[number] = true
		for _, row := range children[number] {
			code := strings.TrimSpace(get(row, "number").Value)
			if _, ok := drawings[code]; ok {
				if err := checkCycle(code); err != nil {
					return err
				}
			}
		}
		visiting[number] = false
		visited[number] = true
		return nil
	}
	numbers := make([]string, 0, len(drawings))
	for number := range drawings {
		numbers = append(numbers, number)
	}
	sort.Strings(numbers)
	for _, number := range numbers {
		if err := checkCycle(number); err != nil {
			return output, report, err
		}
	}
	roots := []string{}
	for _, number := range numbers {
		if parents[number] == 0 && len(drawings[number].Tables) > 0 {
			roots = append(roots, number)
		}
	}
	if len(roots) != 1 {
		return output, report, errors.New("cannot determine one root assembly: drawing set has zero or multiple roots")
	}
	report.RootNumber = roots[0]
	root := drawings[report.RootNumber]
	report.RootName = strings.TrimSpace(root.Name)
	if report.RootName == "" {
		base := path.Base(strings.ReplaceAll(root.Source, "\\", "/"))
		base = strings.TrimSuffix(base, path.Ext(base))
		// Only remove the exact root identity followed by a delimiter; do not
		// guess a name by substituting a child assembly's name.
		if tail, ok := strings.CutPrefix(base, report.RootNumber); ok &&
			(len(tail) == 0 || strings.ContainsAny(tail[:1], " -_\t")) {
			report.RootName = strings.TrimSpace(strings.TrimLeft(tail, " -_\t"))
		}
	}
	output.RootAssembly = &AssemblyIdentity{Number: report.RootNumber, Name: report.RootName}
	standard := []Column{}
	titles := map[string]string{
		"sequence": "结构序号", "level": "层级", "parent": "父项代号", "number": "代号", "name": "名称",
		"quantity": "本级数量", "single_quantity": "单台数量", "batch_quantity": "多台数量",
		"material": "材料", "unit": "单位", "unit_weight": "源单重", "total_weight": "源总重",
		"remark": "备注", "revision": "版本",
	}
	for _, field := range []string{
		"sequence", "level", "parent", "number", "name", "quantity", "single_quantity", "batch_quantity",
		"material", "unit", "unit_weight", "total_weight", "remark", "revision",
	} {
		standard = append(standard, Column{ID: "ebom_" + field, Title: titles[field]})
	}
	if len(standard)+len(data.Columns) > 100 {
		return output, report, errors.New("EBOM and source columns exceed output budget")
	}
	output.Columns = append(standard, data.Columns...)
	reached := map[string]bool{}
	var walk func(string, *big.Rat, string, int) error
	walk = func(number string, multiplier *big.Rat, position string, depth int) error {
		if depth > 32 {
			return errors.New("assembly depth exceeds 32")
		}
		reached[number] = true
		for i, row := range children[number] {
			if len(output.Rows) >= 10000 || (len(output.Rows)+1)*len(output.Columns) > 100000 {
				return errors.New("expanded EBOM exceeds row budget")
			}
			code := strings.TrimSpace(get(row, "number").Value)
			qty, _ := decimalQuantity(get(row, "quantity").Value)
			single := new(big.Rat).Mul(multiplier, qty)
			batch := new(big.Rat).Mul(single, new(big.Rat).SetInt64(int64(options.ProductionCount)))
			singleText, err := decimalText(single)
			if err != nil {
				return err
			}
			batchText, err := decimalText(batch)
			if err != nil {
				return err
			}
			seq := fmt.Sprint(i + 1)
			if position != "" {
				seq = position + "." + seq
			}
			values := map[string]Cell{}
			for _, field := range []string{
				"number", "name", "quantity", "material", "unit", "unit_weight", "total_weight", "remark",
			} {
				values[field] = get(row, field)
			}
			values["sequence"] = Cell{Value: seq, State: "value"}
			values["level"] = Cell{Value: fmt.Sprint(depth), State: "value"}
			values["parent"] = Cell{Value: number, State: "value"}
			values["single_quantity"] = Cell{Value: singleText, State: "value"}
			values["batch_quantity"] = Cell{Value: batchText, State: "value"}
			values["revision"] = Cell{State: "not_applicable"}
			if target, ok := drawings[code]; ok {
				if target.Revision != "" {
					values["revision"] = Cell{Value: target.Revision, State: "value"}
				}
				reached[code] = true
			} else {
				partial = true
			}
			// Weight calculation is intentionally not inferred without a stated unit.
			for _, field := range standard {
				v, ok := values[strings.TrimPrefix(field.ID, "ebom_")]
				if !ok {
					v = Cell{State: "not_detected"}
				}
				if v.State == "not_detected" || v.State == "ambiguous" {
					partial = true
				}
			}
			result := Row{Source: row.Source, ID: row.ID, TableID: row.TableID}
			for _, field := range standard {
				v, ok := values[strings.TrimPrefix(field.ID, "ebom_")]
				if !ok {
					v = Cell{State: "not_detected"}
				}
				result.Values = append(result.Values, v)
			}
			result.Values = append(result.Values, row.Values...)
			output.Rows = append(output.Rows, result)
			if _, exists := drawings[code]; exists {
				if err := walk(code, single, seq, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(roots[0], big.NewRat(1, 1), "", 1); err != nil {
		return Dataset{}, report, err
	}
	for _, number := range numbers {
		if !reached[number] {
			return Dataset{}, report, fmt.Errorf("drawing is not reachable from root assembly: %s", number)
		}
	}
	if len(output.Rows)*len(output.Columns) > 100000 {
		return Dataset{}, report, errors.New("expanded EBOM exceeds cell budget")
	}
	issues = append(issues, fmt.Sprintf("层级 EBOM；根总成：%s；生产台数：%d。"+
		"数量按直接父项用量逐级相乘，多台数量=单台数量×生产台数。", roots[0], options.ProductionCount),
		"同件在不同结构路径保留独立行，不是采购汇总。未执行 Excel 公式；缺少重量单位／版本等证据时保留未检测状态。")
	if partial {
		issues = append(issues, "部分 EBOM：身份／类型／字段仍有未验证项，不代表已证明整套图纸完整；未匹配图纸的物料可能是外购件或缺失子图，未擅自区分。")
	}
	if len(issues) > 1000 {
		return Dataset{}, report, errors.New("EBOM issue count exceeds export budget")
	}
	output.Partial = partial
	output.Issues = issues
	report.Partial = partial
	report.Issues = issues
	report.Occurrences = len(output.Rows)
	return output, report, nil
}

// EBOMBindings creates deterministic template projections; no model copies values.
func EBOMBindings(layout TemplateLayout) (map[string]string, error) {
	bindings := map[string]string{}
	for _, field := range layout.Fields {
		meaning := EBOMField(field.Label)
		if meaning == "" {
			return nil, fmt.Errorf("unsupported EBOM template heading: %s", field.Label)
		}
		bindings[field.Key] = "ebom_" + meaning
	}
	return bindings, nil
}
