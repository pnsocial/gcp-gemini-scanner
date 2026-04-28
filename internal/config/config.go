package config

import (
	"path/filepath"
	"strings"
)

// Config holds CLI flag values after validation.
type Config struct {
	OrgID     string
	FolderIDs []string // multiple roots when --folderid is comma-separated

	Output   string
	LogFile  string
	Workers  int
	RPS      int
	MaxDepth int
	DryRun   bool
	Debug    bool
}

// ScopeKind indicates how the scan root was specified.
type ScopeKind int

const (
	ScopeNone ScopeKind = iota
	ScopeOrg
	ScopeFolders
)

// ParseFolderList splits comma-separated folder ids into trimmed non-empty values.
func ParseFolderList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ResolveLogPath returns logFlag if non-empty; otherwise a path beside outputCSV with a .log suffix.
func ResolveLogPath(outputCSV, logFlag string) string {
	if logFlag != "" {
		return logFlag
	}
	if outputCSV == "" {
		return "gemini-api-scanner.log"
	}
	ext := filepath.Ext(outputCSV)
	if ext == "" {
		return outputCSV + ".log"
	}
	return strings.TrimSuffix(outputCSV, ext) + ".log"
}
