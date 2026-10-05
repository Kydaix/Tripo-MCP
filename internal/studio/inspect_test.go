package studio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func inspectFixture(t *testing.T, degenerate, black bool) string {
	t.Helper()
	bin := []byte{}
	for _, v := range []float32{0, 0, 0, 2, 0, 0, 0, 2, 0} {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
	}
	uv := []float32{0, 0, 1, 0, 0, 1}
	if degenerate {
		uv = []float32{.5, .5, .5, .5, .5, .5}
	}
	for _, v := range uv {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if !black {
		img.Set(1, 1, color.RGBA{R: 255, A: 255})
	}
	var encoded bytes.Buffer
	png.Encode(&encoded, img)
	imageOffset := len(bin)
	bin = append(bin, encoded.Bytes()...)
	imageLength := encoded.Len()
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	d := map[string]any{"asset": map[string]any{"version": "2.0"}, "scene": 0, "scenes": []any{map[string]any{"nodes": []int{0}}}, "nodes": []any{map[string]any{"name": "mesh", "mesh": 0, "scale": []int{1, 2, 1}}}, "meshes": []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": 0, "TEXCOORD_0": 1}, "material": 0}}}}, "accessors": []any{map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"}, map[string]any{"bufferView": 1, "componentType": 5126, "count": 3, "type": "VEC2"}}, "bufferViews": []any{map[string]any{"buffer": 0, "byteOffset": 0, "byteLength": 36}, map[string]any{"buffer": 0, "byteOffset": 36, "byteLength": 24}, map[string]any{"buffer": 0, "byteOffset": imageOffset, "byteLength": imageLength}}, "buffers": []any{map[string]any{"byteLength": len(bin)}}, "materials": []any{map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]any{"index": 0}}}}, "textures": []any{map[string]any{"source": 0}}, "images": []any{map[string]any{"bufferView": 2, "mimeType": "image/png"}}}
	j, _ := json.Marshal(d)
	for len(j)%4 != 0 {
		j = append(j, ' ')
	}
	b := []byte("glTF")
	b = binary.LittleEndian.AppendUint32(b, 2)
	b = binary.LittleEndian.AppendUint32(b, uint32(28+len(j)+len(bin)))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(j)))
	b = append(b, []byte("JSON")...)
	b = append(b, j...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(bin)))
	b = append(b, []byte("BIN\x00")...)
	b = append(b, bin...)
	p := filepath.Join(t.TempDir(), "fixture.glb")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInspectionDetectsPaletteUVAndEmptyTextureWithoutEditingFile(t *testing.T) {
	for _, bad := range []bool{false, true} {
		p := inspectFixture(t, bad, bad)
		before, _ := os.ReadFile(p)
		r := InspectModel(p, true)
		after, _ := os.ReadFile(p)
		if !bytes.Equal(before, after) || len(r.Dimensions) != 3 || r.Dimensions[0] != 2 || r.Dimensions[1] != 4 || r.Triangles != 1 {
			t.Fatalf("%+v", r)
		}
		if bad {
			if r.UVStatus != "unusable" || r.BlackTextures != 1 || CheckTextureUV(r) == nil {
				t.Fatalf("missed unusable artifact: %+v", r)
			}
		} else {
			if r.UVStatus != "checked" || r.BlackTextures != 0 || CheckTextureUV(r) != nil {
				t.Fatalf("rejected valid atlas: %+v", r)
			}
		}
	}
}

func TestGLBMalformedAndUnsupportedNeverPanicOrPretendVerified(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("glTF"), append([]byte("glTF"), make([]byte, 100)...)} {
		p := filepath.Join(t.TempDir(), "bad.glb")
		os.WriteFile(p, b, 0600)
		if r := InspectModel(p, true); r.UVStatus != "unverified" {
			t.Fatal(r)
		}
	}
	p := inspectFixture(t, false, false)
	d, err := readGLB(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{-1, 100} {
		if _, _, err := d.accessor(index, 2); err == nil {
			t.Fatal("bounds accepted")
		}
	}
	d.Accessors[1].Offset = math.MaxInt
	if _, _, err := d.accessor(1, 2); err == nil {
		t.Fatal("overflow accepted")
	}
}
