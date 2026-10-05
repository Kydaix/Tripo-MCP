package studio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"strings"
)

type ModelInspection struct {
	Status        string    `json:"status"`
	Dimensions    []float64 `json:"dimensions_m,omitempty"`
	Triangles     int       `json:"triangles,omitempty"`
	DegenerateUV  int       `json:"degenerate_uv_triangles,omitempty"`
	MissingUV     bool      `json:"missing_uv,omitempty"`
	UVStatus      string    `json:"uv_status"`
	BlackTextures int       `json:"all_black_base_color_textures,omitempty"`
	TextureStatus string    `json:"texture_status"`
	Warnings      []string  `json:"warnings,omitempty"`
}
type ImportInfo struct {
	UseOriginalUV bool             `json:"use_original_uv"`
	Source        *ModelInspection `json:"source,omitempty"`
}
type glbAccessor struct {
	BufferView *int            `json:"bufferView"`
	Offset     int             `json:"byteOffset"`
	Component  int             `json:"componentType"`
	Count      int             `json:"count"`
	Type       string          `json:"type"`
	Min        []float64       `json:"min"`
	Max        []float64       `json:"max"`
	Normalized bool            `json:"normalized"`
	Sparse     json.RawMessage `json:"sparse"`
}
type glbPrimitive struct {
	Attributes map[string]int             `json:"attributes"`
	Indices    *int                       `json:"indices"`
	Mode       *int                       `json:"mode"`
	Material   *int                       `json:"material"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}
type glbDocument struct {
	Scene  *int `json:"scene"`
	Scenes []struct {
		Nodes []int `json:"nodes"`
	} `json:"scenes"`
	Nodes []struct {
		Mesh        *int      `json:"mesh"`
		Children    []int     `json:"children"`
		Matrix      []float64 `json:"matrix"`
		Translation []float64 `json:"translation"`
		Rotation    []float64 `json:"rotation"`
		Scale       []float64 `json:"scale"`
	} `json:"nodes"`
	Meshes []struct {
		Primitives []glbPrimitive `json:"primitives"`
	} `json:"meshes"`
	Accessors []glbAccessor `json:"accessors"`
	Views     []struct {
		Buffer     int                        `json:"buffer"`
		Offset     int                        `json:"byteOffset"`
		Length     int                        `json:"byteLength"`
		Stride     int                        `json:"byteStride"`
		Extensions map[string]json.RawMessage `json:"extensions"`
	} `json:"bufferViews"`
	Buffers []struct {
		URI    string `json:"uri"`
		Length int    `json:"byteLength"`
	} `json:"buffers"`
	Materials []struct {
		PBR struct {
			BaseColor *struct {
				Index    int `json:"index"`
				TexCoord int `json:"texCoord"`
			} `json:"baseColorTexture"`
		} `json:"pbrMetallicRoughness"`
	} `json:"materials"`
	Textures []struct {
		Source *int `json:"source"`
	} `json:"textures"`
	Images []struct {
		View *int `json:"bufferView"`
	} `json:"images"`
	bin []byte
}

func readGLB(path string) (*glbDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxModelBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxModelBytes || len(b) < 20 || string(b[:4]) != "glTF" || binary.LittleEndian.Uint32(b[4:]) != 2 || uint64(binary.LittleEndian.Uint32(b[8:])) != uint64(len(b)) {
		return nil, invalid("GLB non inspectable (version, taille ou limite 100 Mo).")
	}
	d := &glbDocument{}
	hasJSON := false
	for pos := 12; pos < len(b); {
		if len(b)-pos < 8 {
			return nil, invalid("Bloc GLB incomplet.")
		}
		n := int64(binary.LittleEndian.Uint32(b[pos:]))
		kind := binary.LittleEndian.Uint32(b[pos+4:])
		pos += 8
		if n > int64(len(b)-pos) || n%4 != 0 {
			return nil, invalid("Bloc GLB invalide.")
		}
		chunk := b[pos : pos+int(n)]
		switch kind {
		case 0x4e4f534a:
			if hasJSON || pos != 20 || n > 16<<20 || json.Unmarshal(bytes.TrimRight(chunk, "\x00 "), d) != nil {
				return nil, invalid("JSON GLB invalide.")
			}
			hasJSON = true
		case 0x004e4942:
			if d.bin != nil {
				return nil, invalid("Plusieurs blocs BIN GLB.")
			}
			d.bin = chunk
		}
		pos += int(n)
	}
	if !hasJSON {
		return nil, invalid("Métadonnées GLB absentes.")
	}
	return d, nil
}

func (d *glbDocument) view(index int) ([]byte, int, error) {
	if index < 0 || index >= len(d.Views) {
		return nil, 0, invalid("Vue GLB invalide.")
	}
	v := d.Views[index]
	if v.Buffer != 0 || len(d.Buffers) == 0 || d.Buffers[0].URI != "" || len(v.Extensions) > 0 || v.Offset < 0 || v.Length < 0 || v.Offset > len(d.bin) || v.Length > len(d.bin)-v.Offset || v.Offset+v.Length > d.Buffers[0].Length {
		return nil, 0, invalid("Buffer GLB externe, compressé ou invalide.")
	}
	return d.bin[v.Offset : v.Offset+v.Length], v.Stride, nil
}
func (d *glbDocument) accessor(index, components int) (func(int, int) (float64, error), int, error) {
	if index < 0 || index >= len(d.Accessors) {
		return nil, 0, invalid("Accesseur GLB invalide.")
	}
	a := d.Accessors[index]
	typ := map[int]string{1: "SCALAR", 2: "VEC2", 3: "VEC3"}[components]
	if a.BufferView == nil || len(a.Sparse) > 0 || a.Count < 0 || a.Count > 9000000 || a.Type != typ {
		return nil, 0, invalid("Accesseur GLB non pris en charge.")
	}
	b, stride, err := d.view(*a.BufferView)
	if err != nil {
		return nil, 0, err
	}
	size := 0
	switch a.Component {
	case 5121:
		size = 1
	case 5123:
		size = 2
	case 5125, 5126:
		size = 4
	default:
		return nil, 0, invalid("Composante GLB non prise en charge.")
	}
	if stride == 0 {
		stride = size * components
	}
	if stride < size*components || stride > 252 || a.Offset < 0 || a.Offset > len(b) || a.Count > 0 && int64(a.Offset)+int64(a.Count-1)*int64(stride)+int64(size*components) > int64(len(b)) {
		return nil, 0, invalid("Bornes d'accesseur GLB invalides.")
	}
	return func(row, col int) (float64, error) {
		if row < 0 || row >= a.Count || col < 0 || col >= components {
			return 0, invalid("Indice GLB hors limites.")
		}
		p := a.Offset + row*stride + col*size
		v := 0.0
		switch a.Component {
		case 5121:
			v = float64(b[p])
			if a.Normalized {
				v /= 255
			}
		case 5123:
			v = float64(binary.LittleEndian.Uint16(b[p:]))
			if a.Normalized {
				v /= 65535
			}
		case 5125:
			v = float64(binary.LittleEndian.Uint32(b[p:]))
		case 5126:
			v = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[p:])))
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, invalid("Coordonnée GLB non finie.")
		}
		return v, nil
	}, a.Count, nil
}

// InspectModel is bounded and read-only. Unsupported formats/encodings are
// explicitly unverified; a small count of unique UV points alone is not proof
// of bad UVs. Zero-area triangles are evidence independent of texture size.
func InspectModel(path string, textures bool) *ModelInspection {
	r := &ModelInspection{Status: "unverified", UVStatus: "unverified", TextureStatus: "not_checked"}
	if !strings.EqualFold(strings.TrimPrefix(filepathExt(path), "."), "glb") {
		r.Warnings = []string{"Inspection UV/dimensions disponible pour GLB uniquement."}
		return r
	}
	d, err := readGLB(path)
	if err != nil {
		r.Warnings = []string{"GLB non inspectable localement ; UV et dimensions non vérifiés."}
		return r
	}
	r.Status = "checked"
	r.UVStatus = "checked"
	uvUnknown := false
	for _, mesh := range d.Meshes {
		for _, p := range mesh.Primitives {
			if p.Mode != nil && *p.Mode != 4 || len(p.Extensions) > 0 {
				uvUnknown = true
				continue
			}
			posID, ok := p.Attributes["POSITION"]
			if !ok {
				uvUnknown = true
				continue
			}
			_, vertices, err := d.accessor(posID, 3)
			if err != nil {
				uvUnknown = true
				continue
			}
			uvID, ok := p.Attributes["TEXCOORD_0"]
			if !ok {
				r.MissingUV = true
				continue
			}
			uv, uvCount, err := d.accessor(uvID, 2)
			if err != nil || uvCount != vertices {
				uvUnknown = true
				continue
			}
			count := vertices
			var indices func(int, int) (float64, error)
			if p.Indices != nil {
				indices, count, err = d.accessor(*p.Indices, 1)
				if err != nil {
					uvUnknown = true
					continue
				}
			}
			if count%3 != 0 {
				uvUnknown = true
				continue
			}
			for i := 0; i < count; i += 3 {
				var u [3][2]float64
				valid := true
				for corner := 0; corner < 3; corner++ {
					index := i + corner
					if indices != nil {
						v, e := indices(index, 0)
						if e != nil || v != math.Trunc(v) || v > float64(vertices-1) {
							valid = false
							break
						}
						index = int(v)
					}
					for c := 0; c < 2; c++ {
						v, e := uv(index, c)
						if e != nil {
							valid = false
							break
						}
						u[corner][c] = v
					}
				}
				if !valid {
					uvUnknown = true
					break
				}
				r.Triangles++
				area := (u[1][0]-u[0][0])*(u[2][1]-u[0][1]) - (u[1][1]-u[0][1])*(u[2][0]-u[0][0])
				if math.Abs(area) <= 1e-15 {
					r.DegenerateUV++
				}
			}
		}
	}
	if uvUnknown || r.Triangles == 0 {
		r.UVStatus = "unverified"
		r.Warnings = append(r.Warnings, "UV non inspectables localement (compression Meshopt/Draco ou structure non prise en charge) ; aucun dépliage validé.")
	}
	if r.MissingUV || r.Triangles > 0 && r.DegenerateUV == r.Triangles {
		r.UVStatus = "unusable"
		r.Warnings = append(r.Warnings, "UV absents ou tous les triangles UV sans surface : créer un atlas avant le texturage.")
	} else if r.DegenerateUV > 0 {
		r.Warnings = append(r.Warnings, "Certains triangles UV sont sans surface ; contrôler le dépliage.")
	}
	r.Dimensions = d.dimensions()
	if textures {
		d.inspectTextures(r)
	}
	if len(r.Warnings) > 0 {
		r.Status = "warning"
	}
	return r
}

func filepathExt(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return ""
	}
	return path[i:]
}

type matrix [16]float64

func identity() matrix { return matrix{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }
func multiply(a, b matrix) (out matrix) {
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			for k := 0; k < 4; k++ {
				out[c*4+r] += a[k*4+r] * b[c*4+k]
			}
		}
	}
	return
}

func (d *glbDocument) dimensions() []float64 {
	if len(d.Scenes) == 0 {
		return nil
	}
	scene := 0
	if d.Scene != nil {
		scene = *d.Scene
	}
	if scene < 0 || scene >= len(d.Scenes) {
		return nil
	}
	lo, hi := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}, [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	seen := map[int]bool{}
	valid := true
	points := 0
	var walk func(int, matrix, int)
	walk = func(id int, parent matrix, depth int) {
		if !valid {
			return
		}
		if depth > 128 || id < 0 || id >= len(d.Nodes) || seen[id] {
			valid = false
			return
		}
		seen[id] = true
		n := d.Nodes[id]
		m := identity()
		if len(n.Matrix) > 0 {
			if len(n.Matrix) != 16 {
				valid = false
				return
			}
			copy(m[:], n.Matrix)
		} else {
			if len(n.Rotation) > 0 {
				if len(n.Rotation) != 4 {
					valid = false
					return
				}
				x, y, z, w := n.Rotation[0], n.Rotation[1], n.Rotation[2], n.Rotation[3]
				m = matrix{1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0, 2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0, 2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0, 0, 0, 0, 1}
			}
			if len(n.Scale) > 0 {
				if len(n.Scale) != 3 {
					valid = false
					return
				}
				for c := 0; c < 3; c++ {
					for r := 0; r < 3; r++ {
						m[c*4+r] *= n.Scale[c]
					}
				}
			}
			if len(n.Translation) > 0 {
				if len(n.Translation) != 3 {
					valid = false
					return
				}
				copy(m[12:15], n.Translation)
			}
		}
		m = multiply(parent, m)
		if n.Mesh != nil {
			if *n.Mesh < 0 || *n.Mesh >= len(d.Meshes) {
				valid = false
				return
			}
			for _, p := range d.Meshes[*n.Mesh].Primitives {
				id, ok := p.Attributes["POSITION"]
				if !ok || len(p.Extensions) > 0 {
					valid = false
					return
				}
				pos, count, err := d.accessor(id, 3)
				if err != nil {
					// Compressed positions still expose a local bounding box in glTF.
					if id < 0 || id >= len(d.Accessors) || len(d.Accessors[id].Min) != 3 || len(d.Accessors[id].Max) != 3 {
						valid = false
						return
					}
					bounds := d.Accessors[id]
					count = 8
					pos = func(row, col int) (float64, error) {
						if bounds.Min[col] > bounds.Max[col] {
							return 0, invalid("Bornes GLB invalides.")
						}
						if row&(1<<col) == 0 {
							return bounds.Min[col], nil
						}
						return bounds.Max[col], nil
					}
				}
				points += count
				if points > 9000000 {
					valid = false
					return
				}
				for i := 0; i < count; i++ {
					var v [3]float64
					for c := 0; c < 3; c++ {
						v[c], err = pos(i, c)
						if err != nil {
							valid = false
							return
						}
					}
					for r := 0; r < 3; r++ {
						x := m[r]*v[0] + m[4+r]*v[1] + m[8+r]*v[2] + m[12+r]
						if math.IsNaN(x) || math.IsInf(x, 0) {
							valid = false
							return
						}
						lo[r] = math.Min(lo[r], x)
						hi[r] = math.Max(hi[r], x)
					}
				}
			}
		}
		for _, child := range n.Children {
			walk(child, m, depth+1)
		}
	}
	for _, id := range d.Scenes[scene].Nodes {
		walk(id, identity(), 0)
	}
	if !valid || points == 0 {
		return nil
	}
	return []float64{hi[0] - lo[0], hi[1] - lo[1], hi[2] - lo[2]}
}

func (d *glbDocument) inspectTextures(r *ModelInspection) {
	r.TextureStatus = "not_present"
	seen := map[int]bool{}
	unverified := false
	checked := false
	for _, m := range d.Materials {
		ref := m.PBR.BaseColor
		if ref == nil {
			continue
		}
		if ref.Index < 0 || ref.Index >= len(d.Textures) || d.Textures[ref.Index].Source == nil {
			unverified = true
			continue
		}
		i := *d.Textures[ref.Index].Source
		if seen[i] {
			continue
		}
		seen[i] = true
		if i < 0 || i >= len(d.Images) || d.Images[i].View == nil {
			unverified = true
			continue
		}
		b, _, err := d.view(*d.Images[i].View)
		if err != nil {
			unverified = true
			continue
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 4096*4096 {
			unverified = true
			continue
		}
		img, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			unverified = true
			continue
		}
		checked = true
		black := true
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y && black; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				red, green, blue, _ := img.At(x, y).RGBA()
				if red != 0 || green != 0 || blue != 0 {
					black = false
					break
				}
			}
		}
		if black {
			r.BlackTextures++
		}
	}
	if checked {
		r.TextureStatus = "checked"
	}
	if unverified {
		r.TextureStatus = "unverified"
	}
	if r.BlackTextures > 0 {
		r.TextureStatus = "warning"
		r.Warnings = append(r.Warnings, "Texture de couleur entièrement noire ou transparente détectée ; le succès Studio ne garantit pas un résultat visuel exploitable.")
	}
}

func CheckTextureUV(r *ModelInspection) error {
	if r != nil && r.UVStatus == "unusable" {
		return fault.New("UV_UNUSABLE", "UV du modèle importé inutilisables pour le texturage : absents ou tous les triangles sans surface. use_original_uv=false ne garantit pas leur reconstruction par Studio. Exporter un atlas UV avec une case par face, puis importer avec use_original_uv=true. Aucune opération payante envoyée.")
	}
	return nil
}
