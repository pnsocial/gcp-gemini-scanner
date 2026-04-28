package output

import (
	"encoding/csv"
	"os"

	"github.com/phuong-macair/gemini-api-scanner/internal/models"
)

var csvHeader = []string{
	"Organization",
	"Full Folder Path",
	"Project Name",
	"Project ID",
	"Gemini Service Status",
	"Vertex Service Status",
	"Key Display Name",
	"Key Type",
	"Key UID",
	"Key State",
	"Restriction Type",
	"Created Time (UTC)",
	"Logging Audit URL",
}

// CSVSink writes scan rows sequentially to a CSV file.
type CSVSink struct {
	w *csv.Writer
	f *os.File
}

// NewCSVSink creates a CSV writer and writes the header row.
func NewCSVSink(path string) (*CSVSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(f)
	s := &CSVSink{w: w, f: f}
	if err := w.Write(csvHeader); err != nil {
		_ = f.Close()
		return nil, err
	}
	w.Flush()
	return s, nil
}

// WriteRow writes one result line and flushes immediately.
func (s *CSVSink) WriteRow(row models.OutputRow) error {
	if s == nil || s.w == nil {
		return nil
	}
	err := s.w.Write([]string{
		row.Organization,
		row.FullFolderPath,
		row.ProjectName,
		row.ProjectID,
		row.GeminiServiceStatus,
		row.VertexServiceStatus,
		row.KeyDisplayName,
		row.KeyType,
		row.KeyUID,
		row.KeyState,
		row.RestrictionType,
		row.CreatedTimeUTC,
		row.LoggingAuditURL,
	})
	if err != nil {
		return err
	}
	s.w.Flush()
	return s.w.Error()
}

// Close flushes and closes the file.
func (s *CSVSink) Close() error {
	if s == nil {
		return nil
	}
	if s.w != nil {
		s.w.Flush()
		err := s.w.Error()
		if s.f != nil {
			cerr := s.f.Close()
			if err != nil {
				return err
			}
			return cerr
		}
		return err
	}
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}
