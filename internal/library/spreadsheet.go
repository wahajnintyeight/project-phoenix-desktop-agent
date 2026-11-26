package library

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const maxSpreadsheetBytes = 25 << 20
const maxSpreadsheetExpanded = 64 << 20

type spreadsheetRow struct {
	Number int               `json:"row"`
	Cells  []string          `json:"cells"`
	Links  map[string]string `json:"links,omitempty"`
}

type spreadsheetResult struct {
	Sheets  []string         `json:"sheets"`
	Sheet   string           `json:"sheet"`
	Rows    []spreadsheetRow `json:"rows"`
	NextRow int              `json:"next_row,omitempty"`
}

// ReadSpreadsheet returns bounded rows and hyperlink targets from an XLSX in the library.
func (s *Store) ReadSpreadsheet(p, sheet string, firstRow, rowCount int) (spreadsheetResult, error) {
	var result spreadsheetResult
	n, err := s.Stat(p)
	if err != nil {
		return result, err
	}
	if n.Dir || !strings.HasSuffix(strings.ToLower(n.Path), ".xlsx") {
		return result, fmt.Errorf("choose an .xlsx file in the library")
	}
	if n.Size > maxSpreadsheetBytes {
		return result, fmt.Errorf("spreadsheet exceeds the 25 MB file limit")
	}
	_, data, err := s.Bytes(p)
	if err != nil {
		return result, err
	}
	// Check unsigned ZIP sizes before passing untrusted archives to the parser.
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return result, fmt.Errorf("invalid xlsx: %w", err)
	}
	var total uint64
	for _, f := range z.File {
		if f.UncompressedSize64 > maxSpreadsheetExpanded || total > maxSpreadsheetExpanded-f.UncompressedSize64 {
			return result, fmt.Errorf("spreadsheet exceeds the 64 MB expanded size limit")
		}
		total += f.UncompressedSize64
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: maxSpreadsheetExpanded, UnzipXMLSizeLimit: maxSpreadsheetExpanded})
	if err != nil {
		return result, fmt.Errorf("could not open spreadsheet: %w", err)
	}
	defer f.Close()
	result.Sheets = f.GetSheetList()
	if len(result.Sheets) == 0 {
		return result, fmt.Errorf("spreadsheet has no worksheets")
	}
	if sheet == "" {
		sheet = result.Sheets[0]
	}
	if idx, _ := f.GetSheetIndex(sheet); idx < 0 {
		return result, fmt.Errorf("worksheet %q not found; available: %s", sheet, strings.Join(result.Sheets, ", "))
	}
	result.Sheet = sheet
	if firstRow == 0 {
		firstRow = 1
	}
	if rowCount == 0 {
		rowCount = 50
	}
	if firstRow < 1 || firstRow > 1048576 || rowCount < 1 || rowCount > 100 {
		return result, fmt.Errorf("first_row must be 1–1048576 and row_count 1–100")
	}
	rows, err := f.Rows(sheet)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	size := 0
	for row := 1; rows.Next(); row++ {
		if row < firstRow {
			continue
		}
		if row >= firstRow+rowCount {
			result.NextRow = row
			break
		}
		cells, err := rows.Columns()
		if err != nil {
			return result, err
		}
		if len(cells) > 256 {
			return result, fmt.Errorf("row %d exceeds the 256-column reading limit", row)
		}
		entry := spreadsheetRow{Number: row, Cells: cells}
		for col, value := range cells {
			size += len(value)
			if size > 128<<10 {
				return result, fmt.Errorf("result exceeds 128 KB; request fewer rows")
			}
			if value == "" {
				continue
			}
			axis, err := excelize.CoordinatesToCellName(col+1, row)
			if err != nil {
				return result, err
			}
			linked, target, err := f.GetCellHyperLink(sheet, axis)
			if err != nil {
				return result, err
			}
			if linked {
				size += len(target)
				if size > 128<<10 {
					return result, fmt.Errorf("result exceeds 128 KB; request fewer rows")
				}
				if entry.Links == nil {
					entry.Links = map[string]string{}
				}
				entry.Links[axis] = target
			}
		}
		result.Rows = append(result.Rows, entry)
	}
	if err := rows.Error(); err != nil {
		return result, err
	}
	return result, nil
}
