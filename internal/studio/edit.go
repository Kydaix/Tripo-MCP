package studio

import (
	"context"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
)

type EditInput struct {
	Operation        string   `json:"operation" jsonschema:"texture, upscale, pbr, remesh, segment, fill, complete, rig ou animate."`
	Prompt           string   `json:"prompt,omitempty" jsonschema:"texture : description des matériaux."`
	Image            string   `json:"image,omitempty" jsonschema:"texture : référence locale PNG JPEG WebP."`
	Images           []string `json:"images,omitempty" jsonschema:"texture multivue : avant, gauche, arrière, droite ; chaîne vide pour une vue absente."`
	StyleImage       string   `json:"style_image,omitempty" jsonschema:"texture : image locale de style, combinable avec le prompt ou les vues."`
	TextureQuality   string   `json:"texture_quality,omitempty" jsonschema:"texture : standard/detailed/extreme ; upscale : detailed/extreme."`
	TextureAlignment string   `json:"texture_alignment,omitempty" jsonschema:"texture : original_image ou geometry."`
	Delight          *bool    `json:"delight,omitempty" jsonschema:"texture : retirer l'éclairage (défaut true)."`
	Parts            []string `json:"parts,omitempty" jsonschema:"Noms des parties ciblées : toutes par défaut pour texture/remesh ; requis pour fill/complete."`
	Faces            int      `json:"faces,omitempty" jsonschema:"remesh : budget de polygones."`
	Quad             bool     `json:"quad,omitempty" jsonschema:"remesh : topologie en quadrangles."`
	SmartPoly        bool     `json:"smart_poly,omitempty" jsonschema:"remesh : réduction Smart Poly."`
	Bake             *bool    `json:"bake,omitempty" jsonschema:"remesh : reprojeter les textures (défaut true)."`
	PartsLevel       string   `json:"parts_level,omitempty" jsonschema:"segment : simple, balanced ou detailed."`
	RigType          string   `json:"rig_type,omitempty" jsonschema:"rig : auto ou biped ; animate : type de rig existant."`
	Skeleton         string   `json:"skeleton,omitempty" jsonschema:"rig biped : mixamo, actorcore, unreal, unity ou vrm."`
	Animations       []string `json:"animations,omitempty" jsonschema:"animate : noms exacts des animations Studio compatibles avec le rig."`
	MotionAssetID    string   `json:"motion_asset_id,omitempty" jsonschema:"animate : mouvement Studio existant ; exclusif avec animations."`
}

var editFields = map[string][]string{
	"texture": {"prompt", "image", "images", "style_image", "texture_quality", "texture_alignment", "delight", "parts"},
	"upscale": {"texture_quality"}, "pbr": {},
	"remesh": {"faces", "quad", "smart_poly", "bake", "parts"}, "segment": {"parts_level"},
	"rig": {"rig_type", "skeleton"}, "animate": {"rig_type", "animations", "motion_asset_id"},
	"fill": {"parts"}, "complete": {"parts"},
}

func (in EditInput) Normalize() (EditInput, error) {
	allowed, ok := editFields[in.Operation]
	if !ok {
		return in, invalid("Opération inconnue ; consulter capabilities.")
	}
	b, _ := json.Marshal(in)
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	for k := range fields {
		if k != "operation" && !oneOf(k, allowed...) {
			return in, invalid("Paramètre " + k + " incompatible avec " + in.Operation + ".")
		}
	}
	switch in.Operation {
	case "fill", "complete":
		if len(in.Parts) == 0 {
			return in, invalid("Indiquer les parties à compléter avec parts.")
		}
	case "texture":
		sources := 0
		for _, v := range []bool{in.Prompt != "", in.Image != "", len(in.Images) > 0} {
			if v {
				sources++
			}
		}
		if sources != 1 {
			return in, invalid("Texture : choisir prompt, image ou multivues.")
		}
		if len(in.Prompt) > 8000 {
			return in, invalid("Prompt trop long.")
		}
		if len(in.Images) > 0 {
			if err := ValidateViews(in.Images); err != nil {
				return in, err
			}
		}
		if in.TextureQuality == "" {
			in.TextureQuality = "standard"
		}
		if !oneOf(in.TextureQuality, "standard", "detailed", "extreme") {
			return in, invalid("Qualité de texture invalide.")
		}
		if in.TextureAlignment == "" {
			in.TextureAlignment = "original_image"
		}
		if !oneOf(in.TextureAlignment, "original_image", "geometry") {
			return in, invalid("Alignement de texture invalide.")
		}
		if in.Delight == nil {
			in.Delight = Bool(true)
		}
	case "upscale":
		if in.TextureQuality == "" {
			in.TextureQuality = "detailed"
		}
		if !oneOf(in.TextureQuality, "detailed", "extreme") {
			return in, invalid("Upscale : detailed (4K) ou extreme (8K).")
		}
	case "remesh":
		if in.Faces == 0 {
			in.Faces = 20000
		}
		if in.Faces < 500 || in.Faces > 2000000 {
			return in, invalid("Budget de retopologie invalide (500 à 2000000).")
		}
		if in.Bake == nil {
			in.Bake = Bool(true)
		}
	case "segment":
		if in.PartsLevel == "" {
			in.PartsLevel = "balanced"
		}
		if !oneOf(in.PartsLevel, "simple", "balanced", "detailed") {
			return in, invalid("Niveau de segmentation invalide.")
		}
	case "rig":
		if in.RigType == "" {
			in.RigType = "auto"
		}
		if !oneOf(in.RigType, "auto", "biped") {
			return in, invalid("Rig : auto ou biped.")
		}
		if in.RigType == "biped" && in.Skeleton == "" {
			in.Skeleton = "mixamo"
		}
		if in.Skeleton != "" && (in.RigType != "biped" || !oneOf(in.Skeleton, "mixamo", "actorcore", "unreal", "unity", "vrm")) {
			return in, invalid("Preset de squelette invalide.")
		}
	case "animate":
		if (len(in.Animations) == 0) == (in.MotionAssetID == "") {
			return in, invalid("Choisir animations ou motion_asset_id.")
		}
		if !oneOf(in.RigType, "biped", "quadruped", "avian", "aquatic", "serpentine", "hexapod", "octopod", "others") {
			return in, invalid("Type de rig requis pour l'animation.")
		}
		if in.MotionAssetID != "" && in.RigType != "biped" {
			return in, invalid("Les mouvements personnalisés nécessitent un rig humanoïde.")
		}
	}
	if len(in.Parts) > 1000 || len(in.Animations) > 100 {
		return in, invalid("Trop de parties ou d'animations.")
	}
	for _, v := range append(append([]string{}, in.Parts...), in.Animations...) {
		if v == "" || len(v) > 256 {
			return in, invalid("Nom de partie ou d'animation invalide.")
		}
	}
	if len(in.MotionAssetID) > 128 {
		return in, invalid("Identifiant de mouvement invalide.")
	}
	return in, nil
}

func (in EditInput) FilePaths() []string {
	paths := []string{}
	if in.Image != "" {
		paths = append(paths, in.Image)
	} else {
		paths = append(paths, in.Images...)
	}
	if in.StyleImage != "" {
		paths = append(paths, in.StyleImage)
	}
	return paths
}

// Edit targets Studio's current project head. The engine checks the expected
// operator before calling this method and never restores an old head implicitly.
func (c *Client) Edit(ctx context.Context, project string, in EditInput, images []*Image) (Receipt, error) {
	in, err := in.Normalize()
	if err != nil {
		return Receipt{}, err
	}
	if len(images) != len(in.FilePaths()) {
		return Receipt{}, invalid("Images de texture non téléversées.")
	}
	for i, p := range in.FilePaths() {
		if p != "" && images[i] == nil {
			return Receipt{}, invalid("Image de texture absente.")
		}
	}
	body := map[string]any{"project_id": project}
	endpoint := ""
	switch in.Operation {
	case "fill", "complete":
		endpoint = "mesh_fill"
		body["model_version"] = "default"
		body["part_names"] = in.Parts
		if in.Operation == "complete" {
			endpoint = "ai_completion"
			body["model_version"] = "v1.0-20250506"
		}
	case "texture":
		endpoint = "texture_model"
		body["texture_quality"] = in.TextureQuality
		body["texture_alignment"] = in.TextureAlignment
		body["delight"] = *in.Delight
		body["part_names"] = nonNil(in.Parts)
		if in.Prompt != "" {
			body["prompt_text"] = in.Prompt
		}
		if in.Image != "" {
			body["image"] = images[0]
		}
		if len(in.Images) > 0 {
			body["images"] = images[:4]
		}
		if in.StyleImage != "" {
			body["style_image"] = images[len(images)-1]
		}
	case "upscale":
		endpoint = "texture_upscaler"
		body["model_version"] = "v3.0-20250812"
		body["texture_quality"] = in.TextureQuality
	case "pbr":
		endpoint = "pbr_generate"
		body["model_version"] = "v3.0-20250812"
	case "remesh":
		endpoint = "remesh"
		body["model_version"] = "default"
		body["face_limit"] = in.Faces
		body["quad"] = in.Quad
		body["smart_poly"] = in.SmartPoly
		body["bake"] = *in.Bake
		body["part_name_list"] = nonNil(in.Parts)
	case "segment":
		endpoint = "ai_segmentation"
		body["model_version"] = "v2.0-20260430"
		body["segmentation_granularity"] = in.PartsLevel
	case "rig":
		var check struct {
			Success  bool   `json:"check_success"`
			Riggable bool   `json:"riggable"`
			Type     string `json:"rig_type"`
		}
		if err = c.request(ctx, "POST", "/v2/studio/operation/pre_rig_check", map[string]string{"project_id": project, "model_version": "v3.0-20260909"}, &check, false); err != nil {
			return Receipt{}, err
		}
		if !check.Success || !check.Riggable {
			return Receipt{}, fault.New("NOT_RIGGABLE", "Le contrôle Studio ne valide pas ce modèle pour le rigging.")
		}
		rig := in.RigType
		if rig == "auto" {
			rig = check.Type
		}
		if !oneOf(rig, "biped", "quadruped", "avian", "aquatic", "serpentine", "hexapod", "octopod", "others") {
			return Receipt{}, fault.New("PROTOCOL_CHANGED", "Type de rig non reconnu.")
		}
		endpoint = "rigging_model"
		body["model_version"] = "v3.0-20260909"
		body["rig_type"] = rig
		if in.Skeleton != "" {
			body["spec"] = in.Skeleton
		}
	case "animate":
		endpoint = "retarget_model"
		body["model_version"] = "default"
		body["rig_type"] = in.RigType
		if in.MotionAssetID != "" {
			body["motion_asset_id"] = in.MotionAssetID
		} else {
			body["animations"] = in.Animations
		}
	}
	var out Receipt
	if err = c.request(ctx, "POST", "/v2/studio/operation/"+endpoint, body, &out, true); err != nil {
		return out, err
	}
	if out.ProjectID == "" {
		out.ProjectID = project
	}
	if out.OperatorID == "" {
		return out, uncertain(true, "", "")
	}
	return out, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
