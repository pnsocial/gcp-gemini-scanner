package output

import (
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/pnsocial/gemini-api-scanner/internal/models"
)

// PrintDryRunProjects prints discovered projects as a table.
func PrintDryRunProjects(rows []models.ProjectInfo) {
	tw := tablewriter.NewWriter(os.Stdout)
	tw.Header("Organization", "Full Folder Path", "Project Name", "Project ID")
	for _, p := range rows {
		_ = tw.Append(p.Organization, p.FullFolderPath, p.ProjectName, p.ProjectID)
	}
	_ = tw.Render()
}

// PrintResults prints output rows (summary view for terminal).
func PrintResults(rows []models.OutputRow) {
	tw := tablewriter.NewWriter(os.Stdout)
	tw.Header("Project ID", "Gemini", "Vertex", "Key", "Key type", "UID", "Restriction", "Created (UTC)")
	for _, r := range rows {
		_ = tw.Append(
			r.ProjectID,
			r.GeminiServiceStatus,
			r.VertexServiceStatus,
			r.KeyDisplayName,
			r.KeyType,
			r.KeyUID,
			r.RestrictionType,
			r.CreatedTimeUTC,
		)
	}
	_ = tw.Render()
	_, _ = fmt.Fprintf(os.Stdout, "\nChi tiết đầy đủ (Folder Path, Audit URL, ...) xem tại CSV đã lưu.\n")
}
