package studio

import (
	"context"
	"strings"

	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

type GenerateInput struct {
	Prompt     string `json:"prompt,omitempty" jsonschema:"Description textuelle ; fournir prompt ou image, jamais les deux."`
	Image      string `json:"image,omitempty" jsonschema:"Chemin absolu d'une image locale PNG JPEG ou WebP."`
	Model      string `json:"model,omitempty" jsonschema:"Version Studio ; par défaut v3.1-20260211."`
	Faces      int    `json:"faces,omitempty" jsonschema:"Budget de faces, de 100 à 2000000 ; défaut 20000."`
	NoTexture  bool   `json:"no_texture,omitempty"`
	Visibility string `json:"visibility,omitempty" jsonschema:"private par défaut, shareable ou public sur demande explicite."`
}

func (in GenerateInput) Normalize() (GenerateInput, error) {
	in.Prompt = strings.TrimSpace(in.Prompt)
	if (in.Prompt == "") == (in.Image == "") {
		return in, fault.New("INVALID_ARGUMENT", "Fournir exactement un prompt ou une image.")
	}
	if len(in.Prompt) > 8000 {
		return in, fault.New("INVALID_ARGUMENT", "Prompt trop long (maximum 8000 octets).")
	}
	if in.Model == "" {
		in.Model = "v3.1-20260211"
	}
	if len(in.Model) > 80 || strings.ContainsAny(in.Model, "\r\n") {
		return in, fault.New("INVALID_ARGUMENT", "Version de modèle invalide.")
	}
	if in.Faces == 0 {
		in.Faces = 20000
	}
	if in.Faces < 100 || in.Faces > 2000000 {
		return in, fault.New("INVALID_ARGUMENT", "Le budget doit être compris entre 100 et 2000000 faces.")
	}
	if in.Visibility == "" {
		in.Visibility = "private"
	}
	if in.Visibility != "private" && in.Visibility != "shareable" && in.Visibility != "public" {
		return in, fault.New("INVALID_ARGUMENT", "Visibilité attendue : private, shareable ou public.")
	}
	return in, nil
}

// Generate follows the Studio request shape observed in the production web app.
// Unknown model versions are passed explicitly; there is no automatic downgrade or retry.
func (c *Client) Generate(ctx context.Context, in GenerateInput, image *Image) (Receipt, error) {
	in, err := in.Normalize()
	if err != nil {
		return Receipt{}, err
	}
	body := map[string]any{"model_version": in.Model, "face_limit": in.Faces, "texture": !in.NoTexture, "pbr": !in.NoTexture, "quad": false, "smart_poly": false, "generate_parts": false, "geometry_quality": "standard", "texture_quality": "standard", "texture_alignment": "original_image", "visibility": in.Visibility, "enable_image_autofix": false}
	path := "/v2/studio/operation/text_to_model"
	if in.Image != "" {
		if image == nil {
			return Receipt{}, fault.New("INVALID_ARGUMENT", "Image non téléversée.")
		}
		path = "/v2/studio/operation/image_to_model"
		body["image"] = image
	} else {
		body["prompt"] = in.Prompt
		body["negative_prompt"] = ""
		body["gen_image_model_version"] = "flux.1_dev"
		body["t_pose"] = false
		body["sketch_to_render"] = false
	}
	var response struct {
		Receipt
		Variations []struct {
			Receipt
			Accepted bool `json:"accepted"`
		} `json:"variations"`
	}
	if err := c.request(ctx, "POST", path, body, &response, true); err != nil {
		return Receipt{}, err
	}
	r := response.Receipt
	if len(response.Variations) > 0 {
		if len(response.Variations) != 1 || !response.Variations[0].Accepted {
			return Receipt{}, uncertain(true, "", "")
		}
		r = response.Variations[0].Receipt
	}
	if r.ProjectID == "" || r.OperatorID == "" {
		return Receipt{}, uncertain(true, "", "")
	}
	return r, nil
}

type ExportInput struct {
	Format  string `json:"format"`
	Project string `json:"project_id"`
}

// Export may consume Studio credits. Callers must obtain explicit authorization.
func (c *Client) Export(ctx context.Context, in ExportInput) (Receipt, string, error) {
	if in.Format != "glb" && in.Format != "fbx" {
		return Receipt{}, "", fault.New("INVALID_ARGUMENT", "Format attendu : glb ou fbx.")
	}
	body := map[string]any{"project_id": in.Project, "format": in.Format, "model_version": "default", "name": "model", "texture_packaging": "embedded", "texture_size": 2048, "pack_uv": false, "export_vertex_colors": false, "export_orientation": "-y", "fbx_preset": "blender", "with_animation": false, "animations": []string{}, "animate_in_place": false, "enable_bake_animation": false, "bake_animation_frame": 0}
	if in.Format == "glb" {
		body["format"] = "gltf"
	}
	var out struct {
		Receipt
		ModelURL string `json:"model_url"`
	}
	if err := c.request(ctx, "POST", "/v2/studio/operation/export", body, &out, true); err != nil {
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
