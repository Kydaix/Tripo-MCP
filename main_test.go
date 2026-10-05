package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func cli(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	t.Setenv("TRIPO_MCP_HOME", t.TempDir())
	var out, errout bytes.Buffer
	code := run(context.Background(), args, bytes.NewReader(nil), &out, &errout)
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("%s %s: %v", out.String(), errout.String(), err)
	}
	return code, v
}
func TestAdvancedCLIDryRun(t *testing.T) {
	code, v := cli(t, "generate", "--prompt", "owl", "--model", "p2.0", "--quad=false", "--variation-faces", "30000,50000", "--dry-run")
	if code != 0 || v["quad"] != false || v["count"] != float64(2) || v["model"] != "Nexus-v2.0-20260801" {
		t.Fatalf("%d %v", code, v)
	}
	code, v = cli(t, "edit", "source", "--operation", "texture", "--prompt", "bronze", "--texture-quality", "extreme", "--delight=false", "--dry-run")
	if code != 0 || v["delight"] != false || v["texture_quality"] != "extreme" {
		t.Fatalf("%d %v", code, v)
	}
	code, _ = cli(t, "export", "source", "--format", "fbx", "--texture-size", "8192", "--packaging", "zip", "--dry-run")
	if code != 0 {
		t.Fatal(code)
	}
}
func TestCLIParameterFileRejectsUnknownFieldsAndConflicts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "parameters.json")
	if err := os.WriteFile(p, []byte(`{"prompt":"owl","geometry_quality":"detailed","faces":2000000}`), 0600); err != nil {
		t.Fatal(err)
	}
	code, v := cli(t, "generate", "--params", p, "--dry-run")
	if code != 0 || v["faces"] != float64(2000000) {
		t.Fatalf("%d %v", code, v)
	}
	code, _ = cli(t, "generate", "--params", p, "--faces", "20000", "--dry-run")
	if code != 2 {
		t.Fatal("conflicting sources")
	}
	if err := os.WriteFile(p, []byte(`{"prompt":"owl","textures_quality":"extreme"}`), 0600); err != nil {
		t.Fatal(err)
	}
	code, _ = cli(t, "generate", "--params", p, "--dry-run")
	if code != 2 {
		t.Fatal("unknown JSON field accepted")
	}
}
