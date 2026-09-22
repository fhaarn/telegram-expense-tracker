package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"github.com/xuri/excelize/v2"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRange(t *testing.T) {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.FixedZone("Jakarta", 7*3600))
	for _, tc := range []struct {
		text string
		bad  bool
	}{{"/export", false}, {"/export 2024-02-01 2024-02-29", false}, {"/export 2026-09-30 2026-09-01", true}, {"/export 2026-02-30 2026-03-01", true}, {"/export 2020-01-01 2026-01-01", true}, {"/export nonsense", true}} {
		_, _, err := ParseRange(tc.text, now)
		if (err != nil) != tc.bad {
			t.Fatal(tc, err)
		}
	}
}
func TestWorkbook(t *testing.T) {
	r := Report{Start: "2026-09-01", End: "2026-09-30", Rows: []Row{{"2026-09-22", "Food & drinks", "Tahu Telor", 20000}, {"2026-09-22", "Coffee & drinks", "Kopi Kenangan", 25000}, {"2026-09-22", "Transport", "Bensin", 100000}, {"2026-09-23", "Entertainment", "Cinema", 65000}, {"2026-09-23", "Food & drinks", "=HYPERLINK(\"https://example.com\")", 30000}}}
	b, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	total, err := f.CalcCellValue("Expenses", "F3", excelize.Options{RawCellValue: true})
	if err != nil || total != "240000" {
		t.Fatal(total, err)
	}
	if formula, _ := f.GetCellFormula("Expenses", "C10"); formula != "" {
		t.Fatal("user text became formula")
	}
	for i := 2; i <= 5; i++ {
		if _, err = f.CalcCellValue("Categories", fmt.Sprintf("B%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	f.SetCellValue("Expenses", "D6", 30000)
	total, err = f.CalcCellValue("Expenses", "F3", excelize.Options{RawCellValue: true})
	if err != nil || total != "250000" {
		t.Fatal("recalculation", total, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	chart := false
	for _, file := range zr.File {
		if file.Name == "xl/charts/chart1.xml" {
			rc, _ := file.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			chart = strings.Contains(string(data), "pieChart") && strings.Contains(string(data), "showPercent") && strings.Contains(string(data), "100000")
		}
	}
	if !chart {
		t.Fatal("missing native pie chart, percentage labels, or cached values")
	}
	if path := os.Getenv("EXPORT_SAMPLE_PATH"); path != "" {
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestEmptyAndLimits(t *testing.T) {
	if _, err := Build(Report{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(Report{Rows: make([]Row, MaxRows+1)}); err != ErrLimit {
		t.Fatal(err)
	}
	if _, err := Build(Report{Rows: []Row{{Amount: MaxTotalRupiah + 1}}}); err != ErrLimit {
		t.Fatal(err)
	}
}
func BenchmarkFullExport(b *testing.B) {
	r := Report{Start: "2026-09-01", End: "2026-09-30"}
	for i := 0; i < MaxRows; i++ {
		r.Rows = append(r.Rows, Row{"2026-09-22", fmt.Sprintf("Category %d", i%10), "Lunch", 20000})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Build(r); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCustomCategoriesAndChartGrouping(t *testing.T) {
	r := Report{Start: "2026-09-01", End: "2026-09-30"}
	for i := 0; i < 10; i++ {
		r.Rows = append(r.Rows, Row{"2026-09-22", fmt.Sprintf("Custom *?~ %d", i), "日本語, café \"quoted\"", int64(100 + i)})
	}
	b, err := Build(r)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if path := os.Getenv("EXPORT_CUSTOM_SAMPLE_PATH"); path != "" {
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	value, err := f.GetCellValue("Categories", "B2", excelize.Options{RawCellValue: true})
	if err != nil || value != "109" {
		t.Fatal("wildcard category matching", value, err)
	}
	value, err = f.GetCellValue("Categories", "E9", excelize.Options{RawCellValue: true})
	if err != nil || value != "303" {
		t.Fatal("remaining category sum", value, err)
	}
}
