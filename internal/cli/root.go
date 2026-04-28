package cli

import (
	"fmt"
	"os"

	"github.com/phuong-macair/gemini-api-scanner/internal/config"
	"github.com/spf13/cobra"
)

var (
	orgID             string
	folderID          string
	excludedFolderIDs string
	outputPath        string
	logPath           string
	workers           int
	rps               int
	maxDepth          int
	dryRun            bool
	debug             bool
	includeUnbilled   bool
)

// Execute runs the Cobra command tree.
func Execute() error {
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "gemini-api-scanner",
	Short: "Audit Gemini API, Vertex AI API, and API keys across GCP folders and projects",
	RunE:  run,
}

func init() {
	rootCmd.Flags().StringVar(&orgID, "orgid", "", "Google Cloud Organization ID (numeric)")
	rootCmd.Flags().StringVar(&folderID, "folderid", "", "Folder ID(s), comma-separated")
	rootCmd.Flags().StringVar(&excludedFolderIDs, "excluded-folder-ids", "", "Folder ID(s) to skip (comma-separated); subtree and projects under them are not crawled")
	rootCmd.Flags().StringVar(&outputPath, "output", "scan_results.csv", "CSV output path")
	rootCmd.Flags().StringVar(&logPath, "log", "", "Log file path (default: same basename as --output with .log)")
	rootCmd.Flags().IntVar(&workers, "workers", 10, "Worker goroutines")
	rootCmd.Flags().IntVar(&rps, "rps", 50, "Global API rate limit (requests per second)")
	rootCmd.Flags().IntVar(&maxDepth, "max-depth", 20, "Max folder tree depth for DFS")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "List projects only, no service or API key calls")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Verbose debug logging")
	rootCmd.Flags().BoolVar(&includeUnbilled, "include-unbilled", false, "Include projects without billing enabled; default is only projects linked with billing enabled")
}

func buildConfig() (*config.Config, error) {
	var scope config.ScopeKind
	switch {
	case orgID != "" && folderID != "":
		return nil, fmt.Errorf("use exactly one of --orgid or --folderid, not both")
	case orgID != "":
		scope = config.ScopeOrg
	case folderID != "":
		scope = config.ScopeFolders
	default:
		return nil, fmt.Errorf("must set exactly one of --orgid or --folderid")
	}
	if rps <= 0 {
		return nil, fmt.Errorf("--rps must be > 0")
	}
	cfg := &config.Config{
		OrgID:             orgID,
		FolderIDs:         config.ParseFolderList(folderID),
		ExcludedFolderIDs: config.ParseFolderList(excludedFolderIDs),
		Output:            outputPath,
		LogFile:           config.ResolveLogPath(outputPath, logPath),
		Workers:           workers,
		RPS:               rps,
		MaxDepth:          maxDepth,
		DryRun:            dryRun,
		Debug:             debug,
		IncludeUnbilled:   includeUnbilled,
	}
	if scope == config.ScopeFolders && len(cfg.FolderIDs) == 0 {
		return nil, fmt.Errorf("no valid folder ids in --folderid")
	}
	return cfg, nil
}

func run(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}
	cfg, err := buildConfig()
	if err != nil {
		return err
	}
	return runScan(cfg)
}

// ExitOnError is a helper for main.
func ExitOnError(err error) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
