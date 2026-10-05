package studio

import "context"

type ExportOptions struct {
	Format         string   `json:"format" jsonschema:"glb, fbx, obj, stl, 3mf ou usdz."`
	TextureSize    int      `json:"texture_size,omitempty" jsonschema:"512, 1024, 2048 (défaut), 4096 ou 8192."`
	Packaging      string   `json:"packaging,omitempty" jsonschema:"embedded (GLB/FBX) ou zip ; archive par défaut pour OBJ."`
	PackUV         bool     `json:"pack_uv,omitempty"`
	VertexColors   bool     `json:"vertex_colors,omitempty" jsonschema:"Couleurs de sommets OBJ."`
	FBXPreset      string   `json:"fbx_preset,omitempty" jsonschema:"blender, 3dsmax ou mixamo."`
	WithAnimation  bool     `json:"with_animation,omitempty"`
	Animations     []string `json:"animations,omitempty"`
	AnimateInPlace bool     `json:"animate_in_place,omitempty"`
	BakeAnimation  bool     `json:"bake_animation,omitempty"`
	BakeFrame      int      `json:"bake_frame,omitempty"`
}
type ExportInput struct {
	ExportOptions
	Project string `json:"project_id"`
}

func (in ExportOptions) Normalize() (ExportOptions, error) {
	if in.Format == "" {
		in.Format = "glb"
	}
	if !oneOf(in.Format, "glb", "fbx", "obj", "stl", "3mf", "usdz") {
		return in, invalid("Format d'export invalide.")
	}
	if in.TextureSize == 0 {
		in.TextureSize = 2048
	}
	switch in.TextureSize {
	case 512, 1024, 2048, 4096, 8192:
	default:
		return in, invalid("Taille de texture invalide.")
	}
	if in.Packaging == "" {
		in.Packaging = "embedded"
		if in.Format == "obj" {
			in.Packaging = "zip"
		}
	}
	if !oneOf(in.Packaging, "embedded", "zip") {
		return in, invalid("Packaging attendu : embedded ou zip.")
	}
	if in.Format == "obj" && in.Packaging != "zip" {
		return in, invalid("OBJ nécessite packaging=zip pour conserver ses fichiers associés.")
	}
	if in.FBXPreset == "" {
		in.FBXPreset = "blender"
	}
	if !oneOf(in.FBXPreset, "blender", "3dsmax", "mixamo") {
		return in, invalid("Preset FBX invalide.")
	}
	if in.Format != "fbx" && in.FBXPreset != "blender" {
		return in, invalid("fbx_preset nécessite FBX.")
	}
	if in.VertexColors && in.Format != "obj" {
		return in, invalid("vertex_colors nécessite OBJ.")
	}
	if in.BakeFrame < 0 || in.BakeFrame > 10000000 || len(in.Animations) > 100 {
		return in, invalid("Paramètres d'animation invalides.")
	}
	if !in.WithAnimation && !in.BakeAnimation && (len(in.Animations) > 0 || in.AnimateInPlace || in.BakeFrame != 0) {
		return in, invalid("Activer with_animation ou bake_animation.")
	}
	return in, nil
}
func (in ExportOptions) FileFormat() string {
	if in.Packaging == "zip" {
		return "zip"
	}
	return in.Format
}

func (c *Client) Export(ctx context.Context, in ExportInput) (Receipt, string, error) {
	opts, err := in.ExportOptions.Normalize()
	if err != nil {
		return Receipt{}, "", err
	}
	format := opts.Format
	if format == "glb" {
		format = "gltf"
	}
	body := map[string]any{"project_id": in.Project, "format": format, "model_version": "default", "name": "model", "texture_packaging": opts.Packaging, "texture_size": opts.TextureSize, "pack_uv": opts.PackUV, "export_vertex_colors": opts.VertexColors, "export_orientation": "-y", "fbx_preset": opts.FBXPreset, "with_animation": opts.WithAnimation, "animations": nonNil(opts.Animations), "animate_in_place": opts.AnimateInPlace, "enable_bake_animation": opts.BakeAnimation, "bake_animation_frame": opts.BakeFrame}
	var out struct {
		Receipt
		ModelURL string `json:"model_url"`
	}
	if err = c.request(ctx, "POST", "/v2/studio/operation/export", body, &out, true); err != nil {
		return Receipt{}, "", err
	}
	if out.OperatorID == "" && out.ModelURL == "" {
		return Receipt{}, "", uncertain(true, "", "")
	}
	if out.ProjectID == "" {
		out.ProjectID = in.Project
	}
	return out.Receipt, out.ModelURL, nil
}
