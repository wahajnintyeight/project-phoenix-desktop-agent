package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	pdfread "github.com/giraffesyo/pdf"
	gpdf "github.com/gpdf-dev/gpdf"
	"github.com/gpdf-dev/gpdf/document"
	"github.com/gpdf-dev/gpdf/template"
)

const maxPDFRead = 25 << 20

// ReadPDF extracts searchable text from a PDF in the library.
func (s *Store) ReadPDF(p string, firstPage, pageCount int) (string, int, error) {
	n, data, err := s.Bytes(p)
	if err != nil {
		return "", 0, err
	}
	if n.Dir {
		return "", 0, fmt.Errorf("%s is a folder", n.Path)
	}
	if len(data) > maxPDFRead {
		return "", 0, fmt.Errorf("%s is larger than the %d MB PDF reading limit", n.Path, maxPDFRead>>20)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", 0, fmt.Errorf("%s is not a PDF file", n.Path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	doc, err := pdfread.Extract(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, fmt.Errorf("could not read %s: %w", n.Path, err)
	}
	if firstPage < 1 {
		firstPage = 1
	}
	if pageCount <= 0 {
		pageCount = 10
	}
	if pageCount > 20 {
		pageCount = 20
	}
	start := min(firstPage-1, len(doc.Pages))
	end := min(start+pageCount, len(doc.Pages))
	var out strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&out, "--- Page %d ---\n%s\n", i+1, doc.Pages[i].Text())
	}
	if start == end {
		return "", len(doc.Pages), errors.New("the requested page is outside the document")
	}
	return out.String(), len(doc.Pages), nil
}

// ExportPDF lays out plain text into a paginated PDF. Markdown markers are kept as text.
func ExportPDF(title, content string) ([]byte, error) {
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("PDF content cannot be empty")
	}
	if len(content) > MaxWrite {
		return nil, fmt.Errorf("PDF content is larger than %d MB", MaxWrite>>20)
	}
	windowsDir := os.Getenv("WINDIR")
	if windowsDir == "" {
		windowsDir = `C:\Windows`
	}
	font, err := os.ReadFile(filepath.Join(windowsDir, "Fonts", "arial.ttf"))
	if err != nil {
		return nil, fmt.Errorf("could not load the Windows Arial font for PDF export: %w", err)
	}
	lines := wrapPDFText(content, 92)
	doc := gpdf.NewDocument(gpdf.WithPageSize(document.A4), gpdf.WithMargins(document.UniformEdges(document.Mm(18))),
		gpdf.WithFont("PhoenixArial", font), gpdf.WithDefaultFont("PhoenixArial", 10))
	for start := 0; start < len(lines); start += 48 {
		page := doc.AddPage()
		page.AutoRow(func(row *template.RowBuilder) {
			row.Col(12, func(col *template.ColBuilder) {
				if start == 0 && title != "" {
					col.Text(title, template.FontSize(18), template.Bold())
				}
				for _, line := range lines[start:min(start+48, len(lines))] {
					col.Text(line, template.FontSize(10))
				}
			})
		})
	}
	data, err := doc.Generate()
	if err != nil {
		return nil, fmt.Errorf("could not create PDF: %w", err)
	}
	return data, nil
}

func wrapPDFText(text string, width int) []string {
	var lines []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, " ")
			continue
		}
		line := ""
		for _, word := range words {
			for utf8.RuneCountInString(word) > width {
				runes := []rune(word)
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				lines = append(lines, string(runes[:width]))
				word = string(runes[width:])
			}
			if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > width {
				lines = append(lines, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
