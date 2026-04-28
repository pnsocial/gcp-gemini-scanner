// Package progress writes scan phase status to stderr (stdout stays clean for piping).
package progress

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const spinPeriod = 100 * time.Millisecond

var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

const (
	symOK  = "[✔]"
	symErr = "[✘]"
)

// Reporter renders phase activity on stderr without touching stdout.
type Reporter struct {
	w io.Writer

	mu sync.Mutex

	authDone atomic.Bool

	discoverCnt  int
	discoverDone atomic.Bool

	scanTotal int64 // atomic semantics via Store/Load
	scanDone  atomic.Int64

	active   string // "auth"|"discover"|"scan"|"report"|"saving"|""
	stopSpin chan struct{}
	spinDone sync.WaitGroup

	spinIx atomic.Uint32
}

// New constructs a Reporter that writes to w (typically os.Stderr).
func New(w io.Writer) *Reporter {
	return &Reporter{w: w}
}

func (r *Reporter) startSpinner(tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopSpin != nil {
		return
	}
	r.active = tag
	r.stopSpin = make(chan struct{})
	r.spinDone.Add(1)
	go r.spinLoop()
}

func (r *Reporter) stopSpinner(tag string, finalLn func()) {
	r.mu.Lock()
	ch := r.stopSpin
	active := r.active
	r.mu.Unlock()

	if active != tag || ch == nil {
		if finalLn != nil {
			finalLn()
		}
		return
	}
	close(ch)
	r.spinDone.Wait()
	_, _ = fmt.Fprintln(r.w) // newline after last \r-rendered spinner line

	r.mu.Lock()
	r.active = ""
	r.stopSpin = nil
	r.mu.Unlock()

	if finalLn != nil {
		finalLn()
	}
}

// AbortActiveSpinnerQuietly stops whichever spinner runs (scan, discover, etc.) without a ✔ line — used before Ctrl+C “saving…” state.
func (r *Reporter) AbortActiveSpinnerQuietly() {
	r.mu.Lock()
	ch := r.stopSpin
	active := r.active
	r.mu.Unlock()

	if active == "" || ch == nil {
		return
	}
	close(ch)
	r.spinDone.Wait()
	_, _ = fmt.Fprintln(r.w)

	r.mu.Lock()
	r.stopSpin = nil
	r.active = ""
	r.mu.Unlock()
}

func (r *Reporter) spinLoop() {
	defer r.spinDone.Done()
	t := time.NewTicker(spinPeriod)
	defer t.Stop()
	for {
		select {
		case <-r.stopSpin:
			return
		case <-t.C:
			r.mu.Lock()
			a := r.active
			w := r.w
			r.mu.Unlock()
			fr := spinnerFrames[r.spinIx.Add(1)%uint32(len(spinnerFrames))]
			var line string
			switch a {
			case "auth":
				line = fmt.Sprintf("[%c] Xác thực thông tin (ADC)...", fr)
			case "discover":
				line = fmt.Sprintf("[%c] Tiếp nhận và rà soát Projects...", fr)
			case "scan":
				total := atomic.LoadInt64(&r.scanTotal)
				done := r.scanDone.Load()
				suffix := " ..."
				if total > 0 {
					suffix = fmt.Sprintf(" (%d / %d projects)", done, total)
				}
				line = fmt.Sprintf("[%c] Kiểm tra API Endpoints%s", fr, suffix)
			case "report":
				line = fmt.Sprintf("[%c] Tổng hợp và xuất báo cáo...", fr)
			case "saving":
				line = fmt.Sprintf("[%c] Đang lưu dữ liệu đã thu thập...", fr)
			default:
				continue
			}
			_, _ = fmt.Fprintf(w, "\r\033[K%s", line)
		}
	}
}

// AuthStart marks phase 1 running (spinner on stderr).
func (r *Reporter) AuthStart() {
	r.authDone.Store(false)
	r.startSpinner("auth")
}

// AuthDone completes phase 1.
func (r *Reporter) AuthDone() {
	r.stopSpinner("auth", func() {
		r.authDone.Store(true)
		_, _ = fmt.Fprintf(r.w, "%s Xác thực thông tin (ADC)\n", symOK)
	})
}

// AuthAbort terminates phase 1 with error (drops spinner safely).
func (r *Reporter) AuthAbort() {
	r.stopSpinner("auth", func() {
		_, _ = fmt.Fprintf(r.w, "%s Xác thực thông tin (ADC)\n", symErr)
	})
}

// DiscoverAbort terminates phase 2 without a project count.
func (r *Reporter) DiscoverAbort() {
	r.stopSpinner("discover", func() {
		_, _ = fmt.Fprintf(r.w, "%s Tiếp nhận và rà soát Projects\n", symErr)
	})
}

// DiscoverStart marks phase 2 running.
func (r *Reporter) DiscoverStart() {
	r.discoverCnt = 0
	r.discoverDone.Store(false)
	r.startSpinner("discover")
}

// DiscoverDone completes phase 2 with project count (0 allowed).
func (r *Reporter) DiscoverDone(projectCount int) {
	r.mu.Lock()
	r.discoverCnt = projectCount
	r.mu.Unlock()

	r.stopSpinner("discover", func() {
		r.discoverDone.Store(true)
		_, _ = fmt.Fprintf(r.w, "%s Tiếp nhận và rà soát Projects        (%d projects)\n", symOK, projectCount)
	})
}

// SetScanTotal sets the scan denominator shown during phase 3.
func (r *Reporter) SetScanTotal(n int64) {
	atomic.StoreInt64(&r.scanTotal, n)
}

// BumpScan increments completed projects counter (workers call once per finished project).
func (r *Reporter) BumpScan() int64 {
	return r.scanDone.Add(1)
}

// ScanStart shows phase 3 (spinner with live counters).
func (r *Reporter) ScanStart(totalProjects int64) {
	r.scanDone.Store(0)
	atomic.StoreInt64(&r.scanTotal, totalProjects)
	r.startSpinner("scan")
}

// ScanDone completes phase 3.
func (r *Reporter) ScanDone() {
	r.stopSpinner("scan", func() {
		total := atomic.LoadInt64(&r.scanTotal)
		done := r.scanDone.Load()
		suffix := ""
		if total > 0 {
			suffix = fmt.Sprintf(" (%d / %d projects)", done, total)
		}
		_, _ = fmt.Fprintf(r.w, "%s Kiểm tra API Endpoints%s\n", symOK, suffix)
	})
}

// ReportStart marks phase 4 running.
func (r *Reporter) ReportStart() {
	r.startSpinner("report")
}

// ReportDone completes phase 4.
func (r *Reporter) ReportDone() {
	r.stopSpinner("report", func() {
		_, _ = fmt.Fprintf(r.w, "%s Tổng hợp và xuất báo cáo\n", symOK)
	})
}

// UserInterrupt stderr lines after Ctrl+C.
func (r *Reporter) UserInterrupt() {
	_, _ = fmt.Fprintf(r.w, "^C\n")
	_, _ = fmt.Fprintf(r.w, "%s Bị ngắt bởi người dùng\n", symErr)
}

// SavingStart spins while flushing partial CSV after interrupt.
func (r *Reporter) SavingStart() {
	r.startSpinner("saving")
}

// SavingDone clears spinner and confirms row count saved.
func (r *Reporter) SavingDone(csvPath string, rowCount int) {
	r.stopSpinner("saving", func() {
		_, _ = fmt.Fprintf(r.w, "%s Đã lưu %d kết quả vào %s\n", symOK, rowCount, csvPath)
	})
}
