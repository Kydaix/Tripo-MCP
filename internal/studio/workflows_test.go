package studio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
)

func TestRigPrecheckControlsPaidSubmission(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			checks, submissions := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b map[string]any
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
					t.Error(err)
				}
				wantFields(t, b, map[string]any{"project_id": "project", "model_version": "v3.0-20260909"})
				switch r.URL.Path {
				case "/v2/studio/operation/pre_rig_check":
					checks++
					fmt.Fprintf(w, `{"code":0,"data":{"check_success":true,"riggable":%t,"rig_type":"quadruped"}}`, allowed)
				case "/v2/studio/operation/rigging_model":
					submissions++
					wantFields(t, b, map[string]any{"rig_type": "quadruped"})
					io.WriteString(w, accepted)
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer srv.Close()
			c := New(auth.Session{})
			c.BaseURL = srv.URL
			_, err := c.Edit(context.Background(), "project", EditInput{Operation: "rig"}, nil)
			if checks != 1 || (err == nil) != allowed || (submissions == 1) != allowed {
				t.Fatalf("checks=%d submissions=%d error=%v", checks, submissions, err)
			}
		})
	}
}

func TestImportCarriesModelUVAndTransform(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fixture.obj")
	if err := os.WriteFile(p, []byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	in := ImportInput{ModelFile: p, Name: "owl", UseOriginalUV: Bool(false), Transform: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 3, 4, 1}}
	c := contractClient(t, "/v2/studio/operation/import_user_model", func(b map[string]any) {
		wantFields(t, b, map[string]any{"format": "obj", "name": "owl", "use_original_uv": false, "model": map[string]any{"bucket": "fixture", "key": "model"}})
		matrix, _ := b["transform_matrix"].([]any)
		if len(matrix) != 16 || matrix[12] != float64(2) {
			t.Error(matrix)
		}
	}, accepted)
	if _, err := c.Import(context.Background(), in, &Image{Bucket: "fixture", Key: "model"}); err != nil {
		t.Fatal(err)
	}
	in.Transform[0] = math.Inf(1)
	if _, err := in.Normalize(); err == nil {
		t.Fatal("non-finite transform accepted")
	}
}

func TestArchiveValidationRequiresModelAndCompleteDirectory(t *testing.T) {
	for _, tc := range []struct {
		format, member string
		valid          bool
	}{
		{"zip", "mesh.obj", true}, {"3mf", "3D/3dmodel.model", true}, {"usdz", "mesh.usdc", true},
		{"zip", "error.html", false}, {"3mf", "mesh.obj", false}, {"usdz", "image.png", false},
	} {
		t.Run(tc.format+"/"+tc.member, func(t *testing.T) {
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			f, err := w.Create(tc.member)
			if err != nil {
				t.Fatal(err)
			}
			io.WriteString(f, "fixture")
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			if err = ValidateModel(bytes.NewReader(b.Bytes()), int64(b.Len()), tc.format); (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if err = ValidateModel(bytes.NewReader(b.Bytes()[:b.Len()/2]), int64(b.Len()/2), tc.format); err == nil {
				t.Fatal("truncated archive accepted")
			}
		})
	}
}
