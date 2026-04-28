package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/phuong-macair/gemini-api-scanner/internal/models"
)

// RunSummary aggregates scan statistics for stderr banners.
type RunSummary struct {
	Duration               time.Duration
	ProjectsQueried        int64
	WithGeminiOrVertexSvcs int64
	KeyRows                int64
	ProblemProjects        int64
	CSVFilename            string
}

// FormatDurationVN renders durations like "2m14s" (readable, Vietnamese-friendly).
func FormatDurationVN(d time.Duration) string {
	d = d.Round(time.Millisecond)
	if d < time.Second {
		return d.String()
	}
	return strings.TrimSpace(d.Round(time.Second).String())
}

// CountActiveKeyRows counts CSV-visible API key rows.
func CountActiveKeyRows(rows []models.OutputRow) int64 {
	var n int64
	for _, r := range rows {
		if r.KeyState == "ACTIVE" && r.KeyUID != "" {
			n++
		}
	}
	return n
}

// WriteProgressBanner renders the bordered summary block from the spec (stderr).
func WriteProgressBanner(w io.Writer, s RunSummary, interrupted bool) {
	fmt.Fprintf(w, "─────────────────────────────────────────\n")
	finishWord := "Hoàn tất."
	if interrupted {
		finishWord = "Đã ngắt sau khi thu thập một phần."
	}
	fmt.Fprintf(w, "%s Thời gian: %s\n", finishWord, FormatDurationVN(s.Duration))
	fmt.Fprintf(w, "Projects đã quét       : %d\n", s.ProjectsQueried)
	fmt.Fprintf(w, "Có Gemini/Vertex       : %d\n", s.WithGeminiOrVertexSvcs)
	fmt.Fprintf(w, "API Keys tìm thấy      : %d\n", s.KeyRows)
	fmt.Fprintf(w, "Lỗi / Bị bỏ qua        : %d projects (xem stderr)\n", s.ProblemProjects)
	fmt.Fprintf(w, "Kết quả lưu tại        : %s\n", s.CSVFilename)
	fmt.Fprintf(w, "─────────────────────────────────────────\n")
}
