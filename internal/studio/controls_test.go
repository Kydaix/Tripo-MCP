package studio

import (
	"context"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func contractClient(t *testing.T, path string, check func(map[string]any), reply string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		check(body)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	c := New(auth.Session{})
	c.BaseURL = srv.URL
	return c
}

const accepted = `{"code":0,"data":{"project_id":"fixture-project","operator_id":"fixture-operator"}}`

func wantFields(t *testing.T, body, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if !reflect.DeepEqual(body[k], v) {
			t.Errorf("%s: got %#v want %#v", k, body[k], v)
		}
	}
}
func TestHDStudioControlsReachWire(t *testing.T) {
	c := contractClient(t, "/v2/studio/operation/text_to_model", func(b map[string]any) {
		wantFields(t, b, map[string]any{"model_version": HD31, "face_limit": float64(2000000), "geometry_quality": "detailed", "texture_quality": "extreme", "texture_alignment": "geometry", "delight": true, "pbr": false, "texture": true, "quad": false, "visibility": "shareable", "t_pose": true, "negative_prompt": "background"})
	}, accepted)
	_, err := c.Generate(context.Background(), GenerateInput{Prompt: "owl", GeometryQuality: "detailed", TextureQuality: "extreme", TextureAlignment: "geometry", Delight: true, NoPBR: true, Faces: 2000000, Visibility: "shareable", TPose: true, NegativePrompt: "background"}, nil)
	if err != nil {
		t.Fatal(err)
	}
}
func TestPartsAndImageAutofix(t *testing.T) {
	c := contractClient(t, "/v2/studio/operation/image_to_model", func(b map[string]any) {
		wantFields(t, b, map[string]any{"generate_parts": true, "segmentation_granularity": "detailed", "texture": false, "enable_image_autofix": true})
		if _, ok := b["texture_quality"]; ok {
			t.Error("inactive texture settings sent")
		}
	}, accepted)
	_, err := c.Generate(context.Background(), GenerateInput{Image: "fixture.png", GenerateParts: true, NoTexture: true, PartsLevel: "detailed", ImageAutofix: true}, []*Image{{Bucket: "fixture", Key: "image"}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestP2VariationsUseStudioContract(t *testing.T) {
	c := contractClient(t, "/v2/studio/operation/text_to_model", func(b map[string]any) {
		wantFields(t, b, map[string]any{"model_version": P20, "quad": true, "symmetry": false})
		for _, k := range []string{"face_limit", "texture", "pbr", "smart_poly", "geometry_quality", "generate_parts"} {
			if _, ok := b[k]; ok {
				t.Errorf("HD/single option leaked: %s", k)
			}
		}
		got := b["variations"].([]any)
		if len(got) != 2 || got[1].(map[string]any)["face_limit"] != float64(10000) {
			t.Error(got)
		}
	}, `{"code":0,"data":{"variations":[{"accepted":true,"project_id":"p1","operator_id":"o1"},{"accepted":false}]}}`)
	entries, err := c.Generate(context.Background(), GenerateInput{Prompt: "owl", Model: "p2.0", VariationFaces: []int{5000, 10000}}, nil)
	if err != nil || len(entries) != 2 || !entries[0].Accepted || entries[1].Accepted {
		t.Fatalf("%v %v", entries, err)
	}
}
func TestMultiviewAndBatchPreserveSlots(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "views", true: "batch"}[batch], func(t *testing.T) {
			input := GenerateInput{Images: []string{"front.png", "", "back.png", ""}}
			images := []*Image{{Key: "front"}, nil, {Key: "back"}, nil}
			endpoint := "multiview_to_model"
			reply := accepted
			if batch {
				input = GenerateInput{BatchImages: []string{"one.png", "two.png"}, TextureQuality: "extreme"}
				images = []*Image{{Key: "one"}, {Key: "two"}}
				endpoint = "batch_image_to_model"
				reply = `{"code":0,"data":[{"project_id":"p1","operator_id":"o1"},{"project_id":"p2","operator_id":"o2"}]}`
			}
			c := contractClient(t, "/v2/studio/operation/"+endpoint, func(b map[string]any) {
				list := b["image"].([]any)
				if batch {
					if len(list) != 2 || list[0].(map[string]any)["texture_quality"] != "extreme" {
						t.Error(list)
					}
				} else {
					if len(list) != 4 || list[1] != nil || list[2].(map[string]any)["key"] != "back" {
						t.Error(list)
					}
				}
			}, reply)
			if _, err := c.Generate(context.Background(), input, images); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestInvalidCombinationsRejected(t *testing.T) {
	cases := []GenerateInput{
		{Prompt: "owl", Image: "x"}, {Prompt: "owl", Model: "p2.0", TextureQuality: "extreme"},
		{Prompt: "owl", Model: "p2.0", Faces: 25001}, {Prompt: "owl", Faces: 2000000},
		{Prompt: "owl", GenerateParts: true}, {Prompt: "owl", Count: 4},
		{Prompt: "owl", Model: "p2.0", Count: 3}, {Prompt: "owl", Model: "invented"},
		{Prompt: "owl", ImageAutofix: true}, {Images: []string{"front", "", "", ""}},
	}
	for _, in := range cases {
		if _, err := in.Normalize(); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	for _, in := range []GenerateInput{{Prompt: "owl"}, {Prompt: "owl", Model: "p2.0"}, {Prompt: "owl", NoTexture: true, GenerateParts: true}} {
		norm, err := in.Normalize()
		if err != nil {
			t.Fatal(err)
		}
		again, err := norm.Normalize()
		if err != nil || !reflect.DeepEqual(norm, again) {
			t.Fatal("normalization is not stable")
		}
	}
}
func TestTextureStyleAndMultiviewContract(t *testing.T) {
	c := contractClient(t, "/v2/studio/operation/texture_model", func(b map[string]any) {
		wantFields(t, b, map[string]any{"project_id": "project", "delight": false, "texture_quality": "extreme", "texture_alignment": "geometry", "part_names": []any{"body"}})
		if b["images"].([]any)[1] != nil || b["style_image"].(map[string]any)["key"] != "style" {
			t.Error(b)
		}
		if _, ok := b["prompt"]; ok {
			t.Error("commercial API field used")
		}
	}, accepted)
	_, err := c.Edit(context.Background(), "project", EditInput{Operation: "texture", Images: []string{"front", "", "back", ""}, StyleImage: "style", Delight: Bool(false), TextureQuality: "extreme", TextureAlignment: "geometry", Parts: []string{"body"}}, []*Image{{Key: "front"}, nil, {Key: "back"}, nil, {Key: "style"}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestOtherEditContracts(t *testing.T) {
	cases := []struct {
		in   EditInput
		path string
		want map[string]any
	}{
		{EditInput{Operation: "texture", Prompt: "bronze"}, "texture_model", map[string]any{"prompt_text": "bronze", "texture_quality": "standard", "delight": true}},
		{EditInput{Operation: "upscale", TextureQuality: "extreme"}, "texture_upscaler", map[string]any{"model_version": "v3.0-20250812", "texture_quality": "extreme"}},
		{EditInput{Operation: "pbr"}, "pbr_generate", map[string]any{"model_version": "v3.0-20250812"}},
		{EditInput{Operation: "remesh", Faces: 5000, Quad: true, Bake: Bool(false), SmartPoly: true}, "remesh", map[string]any{"face_limit": float64(5000), "quad": true, "bake": false, "smart_poly": true, "part_name_list": []any{}}},
		{EditInput{Operation: "segment", PartsLevel: "simple"}, "ai_segmentation", map[string]any{"model_version": "v2.0-20260430", "segmentation_granularity": "simple"}},
		{EditInput{Operation: "fill", Parts: []string{"body"}}, "mesh_fill", map[string]any{"model_version": "default", "part_names": []any{"body"}}},
		{EditInput{Operation: "complete", Parts: []string{"body"}}, "ai_completion", map[string]any{"model_version": "v1.0-20250506", "part_names": []any{"body"}}},
		{EditInput{Operation: "animate", RigType: "biped", Animations: []string{"walk"}}, "retarget_model", map[string]any{"rig_type": "biped", "animations": []any{"walk"}}},
	}
	for _, tc := range cases {
		t.Run(tc.in.Operation, func(t *testing.T) {
			c := contractClient(t, "/v2/studio/operation/"+tc.path, func(b map[string]any) { wantFields(t, b, tc.want) }, accepted)
			if _, err := c.Edit(context.Background(), "project", tc.in, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := (EditInput{Operation: "pbr", TextureQuality: "extreme"}).Normalize(); err == nil {
		t.Fatal("ignored incompatible option")
	}
	if _, err := (EditInput{Operation: "texture", StyleImage: "style.png"}).Normalize(); err == nil {
		t.Fatal("texture without a description or reference accepted")
	}
}
func TestExportAdvancedContract(t *testing.T) {
	c := contractClient(t, "/v2/studio/operation/export", func(b map[string]any) {
		wantFields(t, b, map[string]any{"format": "fbx", "texture_size": float64(8192), "texture_packaging": "zip", "pack_uv": true, "fbx_preset": "mixamo", "with_animation": true, "animations": []any{"walk"}, "animate_in_place": true})
	}, accepted)
	_, _, err := c.Export(context.Background(), ExportInput{Project: "project", ExportOptions: ExportOptions{Format: "fbx", TextureSize: 8192, Packaging: "zip", PackUV: true, FBXPreset: "mixamo", WithAnimation: true, Animations: []string{"walk"}, AnimateInPlace: true}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestIncompleteBatchRetainsLaterReceipts(t *testing.T) {
	entries, err := decodeSubmission(json.RawMessage(`[{},{"project_id":"p","operator_id":"o"}]`))
	if !IsUncertain(err) || len(entries) != 1 || entries[0].OperatorID != "o" {
		t.Fatalf("%+v %v", entries, err)
	}
}

func TestStudioQuadFBXIsNotRelabeledGLB(t *testing.T) {
	for _, format := range []string{"glb", "fbx"} {
		actual, err := ModelFormat("https://models.tripo3d.ai/model." + format + "?signature=fixture")
		if err != nil || actual != format {
			t.Fatalf("%s %v", actual, err)
		}
	}
	if _, err := ModelFormat("https://models.tripo3d.ai/model.json"); err == nil {
		t.Fatal("unknown format accepted")
	}
}
