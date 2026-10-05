package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/engine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
	"time"
)

func TestMCPRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(engine.New(t.TempDir()), "test")
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(list.Tools) != 6 {
		t.Fatalf("tool list: %v %v", list, err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "tripo_generate" {
			schema, _ := json.Marshal(tool.InputSchema)
			if !strings.Contains(string(schema), `"prompt"`) {
				t.Fatalf("embedded input missing: %s", schema)
			}
		}
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "tripo_jobs", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("jobs: %v %v", result, err)
	}
	result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "tripo_generate", Arguments: map[string]any{"request_id": "test", "prompt": "owl", "confirm": false}})
	if err != nil || !result.IsError {
		t.Fatalf("authorization was not enforced: %v %v", result, err)
	}
}
