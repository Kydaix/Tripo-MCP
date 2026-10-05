package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
)

func TestAttachRecognizesAllIdleProjectRepresentations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		running any
		idle    bool
	}{
		{"null", nil, true}, {"false", false, true}, {"empty", "", true},
		{"operator", map[string]any{"operator_id": "running"}, false}, {"true", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setup(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("attach attempted a write")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
					"running_operator": tc.running, "operator": map[string]any{"operator_id": "done"},
					"model_url": "https://models.tripo3d.ai/fixture.glb",
				}})
			})
			v, err := e.Attach(context.Background(), AttachRequest{ProjectID: "fixture", RequestID: "attach"})
			if tc.idle && (err != nil || v.State != "success") {
				t.Fatalf("idle project rejected: %v", err)
			}
			if !tc.idle && (err == nil || fault.Public(err).Code != "NOT_READY") {
				t.Fatal("running project accepted")
			}
		})
	}
}

func TestExportDownloadRefreshesSignedLinkWithoutExportingAgain(t *testing.T) {
	resolved := 0
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/studio/operation/download_with_name" {
			t.Errorf("unexpected operation %s", r.URL.Path)
		}
		resolved++
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"model_url": "https://models.tripo3d.ai/fresh.glb"}})
	})
	s, _ := auth.Load(e.Root)
	ref, err := local.Protect([]byte("https://models.tripo3d.ai/stale.glb"))
	if err != nil {
		t.Fatal(err)
	}
	j := Job{ID: "export", Kind: "export", State: "success", Account: s.Account, Format: "glb", DownloadRef: ref, Receipt: studio.Receipt{ProjectID: "project", OperatorID: "export-op"}}
	if err = e.save(&j); err != nil {
		t.Fatal(err)
	}
	// An incompatible destination stops before storage access, after URL resolution.
	_, err = e.Download(context.Background(), j.ID, filepath.Join(t.TempDir(), "wrong.fbx"))
	if err == nil || fault.Public(err).Code != "INVALID_ARGUMENT" || resolved != 1 {
		t.Fatalf("link not refreshed: calls=%d error=%v", resolved, err)
	}
}

func TestCachedDownloadHonorsDestinationAndDetectsSameSizeChanges(t *testing.T) {
	e := New(t.TempDir()) // Cached artifacts must be usable offline, without a session.
	body := []byte("fixture-model")
	path := filepath.Join(t.TempDir(), "original.glb")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	j := Job{ID: "cached", Kind: "generate", State: "downloaded", Artifact: &studio.Artifact{Path: path, Bytes: int64(len(body)), Format: "glb", SHA256: hex.EncodeToString(hash[:])}}
	if err := e.save(&j); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copy.glb")
	v, err := e.Download(context.Background(), j.ID, destination)
	if err != nil || v.Artifact.Path != destination {
		t.Fatalf("destination ignored: %+v %v", v, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(body) {
		t.Fatal("copy differs")
	}
	_, err = e.Download(context.Background(), j.ID, path)
	if err == nil || fault.Public(err).Code != "FILE_EXISTS" {
		t.Fatal("existing file overwritten")
	}
	if err = os.WriteFile(destination, []byte("changed-model"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = e.Download(context.Background(), j.ID, "")
	if err == nil || fault.Public(err).Code != "ARTIFACT_CHANGED" {
		t.Fatalf("same-size modification missed: %v", err)
	}
}
