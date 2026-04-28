package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/phuong-macair/gemini-api-scanner/internal/config"
	"github.com/phuong-macair/gemini-api-scanner/internal/gcp"
	"github.com/phuong-macair/gemini-api-scanner/internal/models"
	"github.com/phuong-macair/gemini-api-scanner/internal/output"
	"github.com/phuong-macair/gemini-api-scanner/internal/progress"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func runScan(cfg *config.Config) error {
	log, logClose, err := newLogger(cfg.Debug, cfg.LogFile)
	if err != nil {
		return err
	}
	defer logClose()
	defer log.Sync() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prog := progress.New(os.Stderr)
	interrupted := atomic.Bool{}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if interrupted.Swap(true) {
			os.Exit(130)
		}
		prog.UserInterrupt()
		prog.AbortActiveSpinnerQuietly()
		prog.SavingStart()
		cancel()
	}()
	defer signal.Stop(sigCh)

	started := time.Now()

	prog.AuthStart()
	opts, err := gcp.DefaultClientOptions(ctx)
	if err != nil {
		prog.AuthAbort()
		return err
	}
	client, err := gcp.NewClient(ctx, cfg.RPS, log, opts...)
	if err != nil {
		prog.AuthAbort()
		return err
	}
	defer client.Close()
	prog.AuthDone()

	orgLabel := ""
	if cfg.OrgID != "" {
		orgLabel = gcp.ResolveOrgDisplayName(ctx, client, cfg.OrgID)
	}

	if cfg.DryRun {
		prog.DiscoverStart()
		var list []models.ProjectInfo
		crawlFn := func(p models.ProjectInfo) {
			log.Info("project", zap.String("id", p.ProjectID), zap.String("path", p.FullFolderPath))
			list = append(list, p)
		}
		if err := gcp.Crawl(ctx, client, cfg, orgLabel, nil, crawlFn); err != nil {
			prog.DiscoverAbort()
			return err
		}
		prog.DiscoverDone(len(list))
		fmt.Println("Dry run — projects discovered (no service / API key scan):")
		output.PrintDryRunProjects(list)
		fmt.Println("Details logged to", cfg.LogFile)
		return nil
	}

	prog.DiscoverStart()
	jobsDiscover := make(chan models.ProjectInfo, 256)
	var projects []models.ProjectInfo
	var discMu sync.Mutex
	discDone := make(chan struct{})
	go func() {
		defer close(discDone)
		for p := range jobsDiscover {
			discMu.Lock()
			projects = append(projects, p)
			discMu.Unlock()
		}
	}()
	if err := gcp.Crawl(ctx, client, cfg, orgLabel, jobsDiscover, nil); err != nil {
		close(jobsDiscover)
		<-discDone
		prog.DiscoverAbort()
		return err
	}
	close(jobsDiscover)
	<-discDone

	discMu.Lock()
	nProj := int64(len(projects))
	discMu.Unlock()
	prog.DiscoverDone(int(nProj))

	csv, err := output.NewCSVSink(cfg.Output)
	if err != nil {
		return err
	}
	defer func() { _ = csv.Close() }()

	results := make(chan models.OutputRow, 256)

	var collectorWG sync.WaitGroup
	var rowsMu sync.Mutex
	var allRows []models.OutputRow
	var geminiVertex atomic.Int64
	var problemProj atomic.Int64

	collectorWG.Add(1)
	go func() {
		defer collectorWG.Done()
		for r := range results {
			rowsMu.Lock()
			allRows = append(allRows, r)
			rowsMu.Unlock()
			if werr := csv.WriteRow(r); werr != nil {
				log.Error("csv write", zap.Error(werr))
			}
		}
	}()

	jobs := make(chan models.ProjectInfo, 256)

	var workerWG sync.WaitGroup
	for w := 0; w < cfg.Workers; w++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			worker(ctx, client, jobs, results, prog, log, cfg.Debug, &geminiVertex, &problemProj)
		}()
	}

	prog.SetScanTotal(nProj)
	prog.ScanStart(nProj)

	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		for _, p := range projects {
			select {
			case <-ctx.Done():
				return
			case jobs <- p:
			}
		}
	}()
	go func() {
		<-feedDone
		close(jobs)
	}()

	workerWG.Wait()
	close(results)
	collectorWG.Wait()
	prog.ScanDone()

	if interrupted.Load() {
		rowsMu.Lock()
		rowN := len(allRows)
		rowsMu.Unlock()
		prog.SavingDone(cfg.Output, rowN)
		return nil
	}

	elapsed := time.Since(started)
	prog.ReportStart()
	rowsMu.Lock()
	summary := append([]models.OutputRow(nil), allRows...)
	rowsMu.Unlock()

	sum := output.RunSummary{
		Duration:               elapsed,
		ProjectsQueried:        nProj,
		WithGeminiOrVertexSvcs: geminiVertex.Load(),
		KeyRows:                output.CountActiveKeyRows(summary),
		ProblemProjects:        problemProj.Load(),
		CSVFilename:            cfg.Output,
	}
	output.WriteProgressBanner(os.Stderr, sum, false)
	prog.ReportDone()

	if len(summary) > 0 {
		output.PrintResults(summary)
	}
	fmt.Fprintf(os.Stderr, "Log: %s\n", cfg.LogFile)
	return nil
}

func newLogger(debug bool, logPath string) (*zap.Logger, func(), error) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	level := zapcore.InfoLevel
	if debug {
		level = zapcore.DebugLevel
	}
	fileCore := zapcore.NewCore(zapcore.NewJSONEncoder(encCfg), zapcore.AddSync(f), level)
	cleanup := func() { _ = f.Close() }
	if debug {
		consoleCfg := zap.NewDevelopmentEncoderConfig()
		consoleCore := zapcore.NewCore(
			zapcore.NewConsoleEncoder(consoleCfg),
			zapcore.AddSync(os.Stderr),
			level,
		)
		return zap.New(zapcore.NewTee(fileCore, consoleCore), zap.AddCaller()), cleanup, nil
	}
	return zap.New(fileCore, zap.AddCaller()), cleanup, nil
}

func worker(
	ctx context.Context,
	c *gcp.Client,
	jobs <-chan models.ProjectInfo,
	out chan<- models.OutputRow,
	prog *progress.Reporter,
	log *zap.Logger,
	debug bool,
	geminiVertex *atomic.Int64,
	problemProj *atomic.Int64,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case j, ok := <-jobs:
			if !ok {
				return
			}
			if debug {
				log.Debug("scan project", zap.String("project_id", j.ProjectID))
			}
			rows, brief := gcp.ScanProject(ctx, c, j)
			if brief.GeminiOrVertexSvcEnabled {
				geminiVertex.Add(1)
			}
			if brief.ProjectProblem {
				problemProj.Add(1)
			}
			prog.BumpScan()
			for _, row := range rows {
				select {
				case <-ctx.Done():
					return
				case out <- row:
				}
			}
		}
	}
}
