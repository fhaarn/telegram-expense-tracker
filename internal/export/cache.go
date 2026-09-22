package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Excelize emits chart references without caches. Supply the verified snapshot
// values so previews can display charts before an Excel recalculation occurs.
func cacheChart(data []byte, names []string, totals map[string]int64) ([]byte, error) {
	labels := append([]string(nil), names...)
	values := []int64{}
	if len(labels) > 8 {
		labels = labels[:7]
	}
	for _, label := range labels {
		values = append(values, totals[label])
	}
	if len(names) > 8 {
		var rest int64
		for _, n := range names[7:] {
			rest += totals[n]
		}
		labels = append(labels, "Remaining categories")
		values = append(values, rest)
	}
	var sc, nc strings.Builder
	fmt.Fprintf(&sc, "<strCache><ptCount val=\"%d\"></ptCount>", len(labels))
	fmt.Fprintf(&nc, "<numCache><formatCode>0</formatCode><ptCount val=\"%d\"></ptCount>", len(labels))
	for i, label := range labels {
		var escaped bytes.Buffer
		_ = xml.EscapeText(&escaped, []byte(label))
		fmt.Fprintf(&sc, "<pt idx=\"%d\"><v>%s</v></pt>", i, escaped.String())
		fmt.Fprintf(&nc, "<pt idx=\"%d\"><v>%d</v></pt>", i, values[i])
	}
	sc.WriteString("</strCache>")
	nc.WriteString("</numCache>")
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, file := range zr.File {
		reader, e := file.Open()
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(reader)
		reader.Close()
		if e != nil {
			return nil, e
		}
		s := string(b)
		if strings.HasPrefix(file.Name, "xl/worksheets/") {
			s = strings.ReplaceAll(s, ` t="str"><f>`, `><f>`)
		}
		if file.Name == "xl/charts/chart1.xml" {
			s = strings.Replace(s, "<numCache><formatCode></formatCode></numCache>", nc.String(), 1)
			s = strings.Replace(s, "</f></strRef></cat>", "</f>"+sc.String()+"</strRef></cat>", 1)
		}
		w, e := zw.CreateHeader(&file.FileHeader)
		if e != nil {
			return nil, e
		}
		if _, e = w.Write([]byte(s)); e != nil {
			return nil, e
		}
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
