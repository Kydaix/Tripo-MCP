package engine

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestPartialBatchPersistsAllVariantsAndResumes(t *testing.T) {
	var paid atomic.Int32
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/studio/operation/text_to_model":
			paid.Add(1)
			io.WriteString(w, `{"code":0,"data":{"variations":[{"accepted":true,"project_id":"p1","operator_id":"o1"},{"accepted":false},{"accepted":true,"project_id":"p3","operator_id":"o3"},{"accepted":true,"project_id":"p4","operator_id":"o4"}]}}`)
		case "/v2/studio/progress":
			var body struct {
				IDs []string `json:"ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"operator_id": body.IDs[0], "status": "success", "progress": 100}}})
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	in := request()
	in.Model = "p2.0"
	in.Count = 4
	first, err := e.Generate(context.Background(), in)
	if err != nil || len(first.Variants) != 4 {
		t.Fatalf("%+v %v", first, err)
	}
	restarted := New(e.Root)
	restarted.Client = e.Client
	result, err := restarted.Wait(context.Background(), first.ID)
	if err != nil || result.State != "partial" || result.Variants[0].State != "success" || result.Variants[1].State != "failed" {
		t.Fatalf("%+v %v", result, err)
	}
	child, err := restarted.read(first.ID + ":3")
	if err != nil || child.Receipt.OperatorID != "o3" {
		t.Fatalf("%+v %v", child, err)
	}
	child.State = "downloaded"
	if err = restarted.save(&child); err != nil {
		t.Fatal(err)
	}
	parent, err := restarted.read(first.ID)
	if err != nil || parent.Variants[2].State != "downloaded" || parent.Variants[0].Receipt.OperatorID != "o1" {
		t.Fatal("sibling was lost")
	}
	if _, err = restarted.Generate(context.Background(), in); err != nil || paid.Load() != 1 {
		t.Fatalf("resubmitted: %d %v", paid.Load(), err)
	}
	if _, err = e.Download(context.Background(), first.ID, ""); fault.Public(err).Code != "INVALID_ARGUMENT" {
		t.Fatal("batch download must select a child")
	}
}
func TestMissingVariantStaysUncertainAfterKnownChildCompletes(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/studio/progress" {
			io.WriteString(w, `{"code":0,"data":[{"operator_id":"o1","status":"success","progress":100}]}`)
		} else {
			io.WriteString(w, `{"code":0,"data":{"variations":[{"accepted":true,"project_id":"p1","operator_id":"o1"}]}}`)
		}
	})
	in := request()
	in.Model = "p2.0"
	in.Count = 2
	v, err := e.Generate(context.Background(), in)
	if !studio.IsUncertain(err) || v.State != "outcome_unknown" || len(v.Variants) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	child, err := e.Poll(context.Background(), v.ID+":1")
	if err != nil || child.State != "success" {
		t.Fatal(err)
	}
	parent, err := e.Poll(context.Background(), v.ID)
	if err != nil || parent.State != "outcome_unknown" {
		t.Fatal("lost incomplete submission state")
	}
}
func seedSource(t *testing.T, e *Engine) {
	t.Helper()
	s, err := auth.Load(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	j := Job{ID: "source", Kind: "generate", State: "success", Account: s.Account, Receipt: studio.Receipt{ProjectID: "project", OperatorID: "original"}, Format: "glb", CreatedAt: time.Now()}
	if err = e.save(&j); err != nil {
		t.Fatal(err)
	}
}

const sourceReply = `{"code":0,"data":{"id":"project","model_url":"https://models.tripo3d.ai/fixture.glb","operator":{"operator_id":"original","is_textured":true}}}`

func TestEditIsDurableAndSourceChangesCannotBeIgnored(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "changed"}[changed], func(t *testing.T) {
			var writes atomic.Int32
			e := setup(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if changed {
						io.WriteString(w, `{"code":0,"data":{"operator":{"operator_id":"newer"}}}`)
					} else {
						io.WriteString(w, sourceReply)
					}
					return
				}
				writes.Add(1)
				if r.URL.Path != "/v2/studio/operation/texture_model" {
					t.Error(r.URL.Path)
				}
				io.WriteString(w, `{"code":0,"data":{"operator_id":"textured","project_id":"project"}}`)
			})
			seedSource(t, e)
			in := EditRequest{EditInput: studio.EditInput{Operation: "texture", Prompt: "bronze", TextureQuality: "extreme", Parts: []string{"mesh"}}, JobID: "source", RequestID: "texture-1", Confirm: true}
			v, err := e.Edit(context.Background(), in)
			if changed {
				if fault.Public(err).Code != "SOURCE_CHANGED" || writes.Load() != 0 {
					t.Fatal("stale source submitted")
				}
				return
			}
			if err != nil || v.OperatorID != "textured" {
				t.Fatalf("%+v %v", v, err)
			}
			restart := New(e.Root)
			restart.Client = e.Client
			if _, err = restart.Edit(context.Background(), in); err != nil || writes.Load() != 1 {
				t.Fatal("edit repeated")
			}
			in.TextureQuality = "detailed"
			if _, err = restart.Edit(context.Background(), in); fault.Public(err).Code != "REQUEST_CONFLICT" {
				t.Fatal("different edit reused")
			}
		})
	}
}
func TestUncertainEditNeverRetries(t *testing.T) {
	var writes atomic.Int32
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, sourceReply)
			return
		}
		writes.Add(1)
		w.WriteHeader(500)
	})
	seedSource(t, e)
	in := EditRequest{EditInput: studio.EditInput{Operation: "upscale"}, JobID: "source", RequestID: "upscale", Confirm: true}
	v, err := e.Edit(context.Background(), in)
	if !studio.IsUncertain(err) || v.State != "outcome_unknown" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = e.Edit(context.Background(), in); err != nil || writes.Load() != 1 {
		t.Fatal("uncertain edit retried")
	}
}
func TestEditsOnSameProjectAreSerialized(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, sourceReply)
			return
		}
		close(entered)
		<-release
		io.WriteString(w, `{"code":0,"data":{"operator_id":"new","project_id":"project"}}`)
	})
	seedSource(t, e)
	in := EditRequest{EditInput: studio.EditInput{Operation: "pbr"}, JobID: "source", RequestID: "first", Confirm: true}
	done := make(chan error, 1)
	go func() { _, err := e.Edit(context.Background(), in); done <- err }()
	<-entered
	in.RequestID = "second"
	_, err := e.Edit(context.Background(), in)
	close(release)
	if fault.Public(err).Code != "BUSY" {
		t.Fatalf("project not locked: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestNewOperationsRequireAuthorization(t *testing.T) {
	e := New(t.TempDir())
	if _, err := e.Edit(context.Background(), EditRequest{}); fault.Public(err).Code != "CONFIRM_REQUIRED" {
		t.Fatal(err)
	}
	if _, err := e.Import(context.Background(), ImportRequest{}); fault.Public(err).Code != "CONFIRM_REQUIRED" {
		t.Fatal(err)
	}
	if _, err := e.Export(context.Background(), ExportRequest{}); fault.Public(err).Code != "CONFIRM_REQUIRED" {
		t.Fatal(err)
	}
}

func TestTextureResolvesAllPartsFromDownloadedSource(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, sourceReply)
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		parts, ok := b["part_names"].([]any)
		if !ok || len(parts) != 1 || parts[0] != "owl" {
			t.Errorf("wrong default parts: %v", b)
		}
		io.WriteString(w, `{"code":0,"data":{"operator_id":"textured","project_id":"project"}}`)
	})
	seedSource(t, e)
	data := []byte(`{"nodes":[{"name":"owl","mesh":0}]}`)
	b := make([]byte, 20)
	copy(b, "glTF")
	binary.LittleEndian.PutUint32(b[12:], uint32(len(data)))
	copy(b[16:], "JSON")
	b = append(b, data...)
	p := filepath.Join(t.TempDir(), "fixture.glb")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	source, err := e.read("source")
	if err != nil {
		t.Fatal(err)
	}
	source.State = "downloaded"
	digest := sha256.Sum256(b)
	source.Artifact = &studio.Artifact{Path: p, Bytes: int64(len(b)), Format: "glb", SHA256: hex.EncodeToString(digest[:])}
	if err = e.save(&source); err != nil {
		t.Fatal(err)
	}
	_, err = e.Edit(context.Background(), EditRequest{EditInput: studio.EditInput{Operation: "texture", Prompt: "bronze"}, JobID: "source", RequestID: "texture-auto-parts", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
}
