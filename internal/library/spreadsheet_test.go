package library

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xuri/excelize/v2"
)

func TestReadSpreadsheet(t *testing.T) {
	s := newStore(t)
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellValue("Sheet1", "B2", "Company"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "B3", "Careers"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellHyperLink("Sheet1", "B3", "https://example.com/jobs", "External"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("Other"); err != nil {
		t.Fatal(err)
	}
	data, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("", "tracker.xlsx", data.Bytes(), "user", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadSpreadsheet("tracker.xlsx", "Sheet1", 2, 1)
	if err != nil || len(got.Sheets) != 2 || len(got.Rows) != 1 || got.Rows[0].Number != 2 || got.Rows[0].Cells[1] != "Company" || got.NextRow != 3 {
		t.Fatalf("first page: %+v, %v", got, err)
	}
	got, err = s.ReadSpreadsheet("tracker.xlsx", "Sheet1", got.NextRow, 10)
	if err != nil || len(got.Rows) != 1 || got.Rows[0].Links["B3"] != "https://example.com/jobs" {
		t.Fatalf("next page: %+v, %v", got, err)
	}
	if _, err := s.ReadSpreadsheet("tracker.xlsx", "Missing", 1, 10); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing sheet: %v", err)
	}
	if _, err := s.ReadSpreadsheet("tracker.xlsx", "", 1, 101); err == nil {
		t.Fatal("unbounded row request accepted")
	}
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := NewServer(s, "agent").Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "files_read_spreadsheet", Arguments: map[string]any{"path": "tracker.xlsx", "first_row": 3}})
	if err != nil || response.IsError {
		t.Fatalf("tool: %+v, %v", response, err)
	}
	var fromTool spreadsheetResult
	if err := json.Unmarshal([]byte(response.Content[0].(*sdk.TextContent).Text), &fromTool); err != nil || len(fromTool.Rows) != 1 || fromTool.Rows[0].Links["B3"] != "https://example.com/jobs" {
		t.Fatalf("tool rows: %+v, %v", fromTool, err)
	}
}
