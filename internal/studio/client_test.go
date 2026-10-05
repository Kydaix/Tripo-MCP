package studio

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExportAndResolutionContracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v2/studio/operation/export":
			if body["format"] != "gltf" || body["texture_packaging"] != "embedded" {
				t.Error("wrong GLB export contract")
			}
			io.WriteString(w, `{"code":0,"data":{"operator_id":"export-1"}}`)
		case "/v2/studio/progress":
			io.WriteString(w, `{"code":0,"data":[{"operator_id":"export-1","status":"success","progress":100}]}`)
		case "/v2/studio/operation/download_with_name":
			if body["operator_id"] != "export-1" {
				t.Error("wrong export operator")
			}
			io.WriteString(w, `{"code":0,"data":{"model_url":"https://models.tripo3d.ai/test.glb"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(auth.Session{Token: "fixture"})
	c.BaseURL = srv.URL
	r, _, err := c.Export(context.Background(), ExportInput{Format: "glb", Project: "project-1"})
	if err != nil || r.OperatorID != "export-1" {
		t.Fatal(err)
	}
	p, err := c.Progress(context.Background(), r.OperatorID)
	if err != nil || p.Status != "success" {
		t.Fatal(err)
	}
	u, err := c.ExportURL(context.Background(), r.OperatorID)
	if err != nil || u != "https://models.tripo3d.ai/test.glb" {
		t.Fatal(err)
	}
}

func TestSessionNeverFollowsRedirect(t *testing.T) {
	received := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
	defer srv.Close()
	c := New(auth.Session{Token: "secret-fixture"})
	c.BaseURL = srv.URL
	if _, err := c.Account(context.Background()); err == nil || received {
		t.Fatal("credentials followed redirect")
	}
}

func TestAssetValidation(t *testing.T) {
	for _, u := range []string{"http://models.tripo3d.ai/a", "https://127.0.0.1/a", "https://tripo3d.ai.evil.test/a", "https://user:pass@models.tripo3d.ai/a", "https://models.tripo3d.ai:8443/a"} {
		if _, err := assetURL(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	if _, err := assetURL("https://models.tripo3d.ai/a?signature=fixture"); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 32)
	copy(b, "glTF")
	binary.LittleEndian.PutUint32(b[4:8], 2)
	binary.LittleEndian.PutUint32(b[8:12], 32)
	if err := ValidateModel(bytes.NewReader(b), 32, "glb"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateModel(bytes.NewReader(b), 64, "glb"); err == nil {
		t.Fatal("truncated GLB accepted")
	}
	if err := ValidateModel(bytes.NewReader([]byte("<html>signed URL expired</html>")), 30, "glb"); err == nil {
		t.Fatal("HTML accepted as GLB")
	}
}
