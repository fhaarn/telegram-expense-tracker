// Package export builds portable Excel expense reports without external services.
package export

import (
	"errors"
	"fmt"
	"github.com/xuri/excelize/v2"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxRows = 2000
const MaxBytes = 5 * 1024 * 1024
const MaxTotalRupiah int64 = 999999999999999

var ErrLimit = errors.New("export exceeds supported size or numeric precision")

type Row struct {
	Date     string `json:"date"`
	Category string `json:"category"`
	Notes    string `json:"notes"`
	Amount   int64  `json:"amount"`
}
type Report struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Rows  []Row  `json:"rows"`
}

func (r Report) Filename() string { return "expenses_" + r.Start + "_to_" + r.End + ".xlsx" }
func ParseRange(text string, now time.Time) (string, string, error) {
	parts := strings.Fields(text)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	if len(parts) != 1 && len(parts) != 3 {
		return "", "", errors.New("use /export or /export YYYY-MM-DD YYYY-MM-DD")
	}
	if len(parts) == 3 {
		var err error
		start, err = time.Parse("2006-01-02", parts[1])
		if err != nil {
			return "", "", err
		}
		end, err = time.Parse("2006-01-02", parts[2])
		if err != nil {
			return "", "", err
		}
	}
	if start.Year() < 1900 || end.Before(start) || end.Sub(start) > 365*24*time.Hour {
		return "", "", errors.New("choose a range of at most 366 days, year 1900 or later")
	}
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}
func Build(r Report) ([]byte, error) {
	if len(r.Rows) > MaxRows {
		return nil, ErrLimit
	}
	var total int64
	categories := map[string]int64{}
	for _, row := range r.Rows {
		if row.Amount <= 0 || row.Amount > MaxTotalRupiah-total {
			return nil, ErrLimit
		}
		total += row.Amount
		categories[row.Category] += row.Amount
	}
	f := excelize.NewFile()
	defer f.Close()
	var first error
	check := func(e error) {
		if first == nil {
			first = e
		}
	}
	check(f.SetSheetName("Sheet1", "Expenses"))
	_, err := f.NewSheet("Categories")
	check(err)
	put := func(sheet, cell string, v any) { check(f.SetCellValue(sheet, cell, v)) }
	formula := func(sheet, cell, expr string, value int64) {
		put(sheet, cell, value)
		check(f.SetCellFormula(sheet, cell, expr))
	}
	style := func(v *excelize.Style) int { id, e := f.NewStyle(v); check(e); return id }
	currency := `"Rp"#,##0;[Red]("Rp"#,##0);"Rp"0`
	title := style(&excelize.Style{Font: &excelize.Font{Family: "Calibri", Size: 20, Bold: true, Color: "17365D"}})
	header := style(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"4472C4"}}, Alignment: &excelize.Alignment{Vertical: "center"}})
	money := style(&excelize.Style{CustomNumFmt: &currency, Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}})
	dateFormat := "ddd, dd mmm yyyy"
	dateStyle := style(&excelize.Style{CustomNumFmt: &dateFormat, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"D9EAD3"}}, Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Vertical: "center"}})
	textStyle := style(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}, Font: &excelize.Font{Family: "Calibri", Size: 11}})
	palette := []string{"FFF2CC", "DDEBF7", "E4DFEC", "D9EAD3", "FCE4D6", "E2F0D9", "F4CCCC", "D9E1F2"}
	names := make([]string, 0, len(categories))
	for c := range categories {
		names = append(names, c)
	}
	sort.Slice(names, func(i, j int) bool {
		if categories[names[i]] == categories[names[j]] {
			return names[i] < names[j]
		}
		return categories[names[i]] > categories[names[j]]
	})
	catStyles := map[string]int{}
	for i, c := range names {
		catStyles[c] = style(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{palette[i%len(palette)]}}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}})
	}
	check(f.MergeCell("Expenses", "A1", "D1"))
	put("Expenses", "A1", "Expense report")
	check(f.SetCellStyle("Expenses", "A1", "D1", title))
	check(f.SetRowHeight("Expenses", 1, 32))
	check(f.MergeCell("Expenses", "A2", "D2"))
	put("Expenses", "A2", r.Start+" to "+r.End+" · IDR")
	check(f.MergeCell("Expenses", "A3", "D3"))
	put("Expenses", "A3", "Snapshot of saved expenses. Export again for updated records.")
	for i, h := range []string{"Date", "Category", "Notes", "Amount"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 5)
		put("Expenses", cell, h)
	}
	check(f.SetCellStyle("Expenses", "A5", "D5", header))
	check(f.SetRowHeight("Expenses", 5, 26))
	check(f.SetColWidth("Expenses", "A", "A", 26))
	check(f.SetColWidth("Expenses", "B", "B", 25))
	check(f.SetColWidth("Expenses", "C", "C", 46))
	check(f.SetColWidth("Expenses", "D", "D", 22))
	check(f.SetColWidth("Expenses", "E", "E", 3))
	check(f.SetColWidth("Expenses", "F", "M", 12))
	lastDate := ""
	for i, row := range r.Rows {
		n := i + 6
		cell := func(col string) string { return fmt.Sprintf("%s%d", col, n) }
		check(f.SetCellStyle("Expenses", cell("A"), cell("D"), textStyle))
		check(f.SetRowHeight("Expenses", n, float64(max(2, (utf8.RuneCountInString(row.Notes)+39)/40))*16))
		if row.Date != lastDate {
			d, e := time.Parse("2006-01-02", row.Date)
			if e != nil {
				return nil, e
			}
			put("Expenses", cell("A"), d)
			check(f.SetCellStyle("Expenses", cell("A"), cell("A"), dateStyle))
			lastDate = row.Date
		}
		put("Expenses", cell("B"), row.Category)
		put("Expenses", cell("C"), row.Notes)
		put("Expenses", cell("D"), row.Amount)
		check(f.SetCellStyle("Expenses", cell("B"), cell("B"), catStyles[row.Category]))
		check(f.SetCellStyle("Expenses", cell("D"), cell("D"), money))
	}
	end := len(r.Rows) + 5
	if end < 6 {
		end = 6
		put("Expenses", "A6", "No expenses in this period.")
	}
	check(f.MergeCell("Expenses", "F2", "L2"))
	put("Expenses", "F2", "TOTAL EXPENSES")
	check(f.SetCellStyle("Expenses", "F2", "L2", header))
	check(f.MergeCell("Expenses", "F3", "L4"))
	formula("Expenses", "F3", fmt.Sprintf("SUM(D6:D%d)", end), total)
	bigMoney := style(&excelize.Style{CustomNumFmt: &currency, Font: &excelize.Font{Size: 24, Bold: true, Color: "17365D"}, Alignment: &excelize.Alignment{Vertical: "center"}})
	check(f.SetCellStyle("Expenses", "F3", "L4", bigMoney))
	put("Categories", "A1", "Category")
	put("Categories", "B1", "Amount (IDR)")
	check(f.SetCellStyle("Categories", "A1", "B1", header))
	check(f.SetColWidth("Categories", "A", "A", 44))
	check(f.SetColWidth("Categories", "B", "B", 24))
	for i, c := range names {
		n := i + 2
		put("Categories", fmt.Sprintf("A%d", n), c)
		// Exact text comparison also supports category names containing * or ?.
		expr := fmt.Sprintf(`SUMPRODUCT((Expenses!$B$6:$B$%d=A%d)*Expenses!$D$6:$D$%d)`, end, n, end)
		formula("Categories", fmt.Sprintf("B%d", n), expr, categories[c])
		check(f.SetCellStyle("Categories", fmt.Sprintf("B%d", n), fmt.Sprintf("B%d", n), money))
	}
	if len(names) > 0 {
		chartNames := names
		if len(names) > 8 {
			chartNames = names[:7]
		}
		put("Categories", "D1", "Chart category")
		put("Categories", "E1", "Amount (IDR)")
		for i, c := range chartNames {
			put("Categories", fmt.Sprintf("D%d", i+2), c)
			formula("Categories", fmt.Sprintf("E%d", i+2), fmt.Sprintf("B%d", i+2), categories[c])
		}
		chartEnd := len(chartNames) + 1
		if len(names) > 8 {
			chartEnd++
			var rest int64
			for _, c := range names[7:] {
				rest += categories[c]
			}
			put("Categories", fmt.Sprintf("D%d", chartEnd), "Remaining categories")
			formula("Categories", fmt.Sprintf("E%d", chartEnd), fmt.Sprintf("SUM(B9:B%d)", len(names)+1), rest)
			put("Expenses", "F25", "Chart groups smaller categories; full breakdown is on Categories.")
		}
		check(f.SetColWidth("Categories", "D", "D", 44))
		check(f.SetColWidth("Categories", "E", "E", 24))
		check(f.SetCellStyle("Categories", "D1", "E1", header))
		check(f.SetCellStyle("Categories", "E2", fmt.Sprintf("E%d", chartEnd), money))
		yes := true
		check(f.AddChart("Expenses", "F6", &excelize.Chart{Type: excelize.Pie, Series: []excelize.ChartSeries{{Name: "Categories!$E$1", Categories: fmt.Sprintf("Categories!$D$2:$D$%d", chartEnd), Values: fmt.Sprintf("Categories!$E$2:$E$%d", chartEnd)}}, Title: []excelize.RichTextRun{{Text: "Spending by category"}}, Dimension: excelize.ChartDimension{Width: 650, Height: 420}, Legend: excelize.ChartLegend{Position: "bottom"}, VaryColors: &yes, PlotArea: excelize.ChartPlotArea{ShowPercent: true, ShowLeaderLines: true}}))
	} else {
		put("Expenses", "F7", "No spending to chart.")
	}
	check(f.SetPanes("Expenses", &excelize.Panes{Freeze: true, YSplit: 5, TopLeftCell: "A6", ActivePane: "bottomLeft"}))
	no := false
	check(f.SetSheetView("Expenses", 0, &excelize.ViewOptions{ShowGridLines: &no}))
	check(f.SetSheetView("Categories", 0, &excelize.ViewOptions{ShowGridLines: &no}))
	if first != nil {
		return nil, first
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	if buf.Len() > MaxBytes {
		return nil, ErrLimit
	}
	data, err := cacheChart(buf.Bytes(), names, categories)
	if len(data) > MaxBytes {
		return nil, ErrLimit
	}
	return data, err
}
