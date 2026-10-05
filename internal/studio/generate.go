package studio

import (
	"context"
	"encoding/json"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"strings"
)

const HD31 = "v3.1-20260211"
const P20 = "Nexus-v2.0-20260801"
const P10 = "Nexus-v1.0-20260214"

type GenerateInput struct {
	Prompt           string   `json:"prompt,omitempty" jsonschema:"Une seule source parmi prompt, image, images ou batch_images."`
	Image            string   `json:"image,omitempty" jsonschema:"Chemin absolu PNG JPEG ou WebP (20 Mo maximum)."`
	Images           []string `json:"images,omitempty" jsonschema:"Multivues : quatre emplacements avant, gauche, arrière, droite ; chaîne vide pour une vue absente. Avant et une autre vue requis."`
	BatchImages      []string `json:"batch_images,omitempty" jsonschema:"Images indépendantes : une génération par image."`
	Model            string   `json:"model,omitempty" jsonschema:"h3.1 (défaut), h3.0, h2.5, p2.0 ou p1.0."`
	Faces            int      `json:"faces,omitempty" jsonschema:"Budget de polygones ; défaut HD 20000, Smart Mesh 5000. Bornes selon modèle et topologie."`
	NoTexture        bool     `json:"no_texture,omitempty"`
	Visibility       string   `json:"visibility,omitempty" jsonschema:"private par défaut, shareable ou public sur demande explicite."`
	Quad             *bool    `json:"quad,omitempty" jsonschema:"true pour quadrangles ; défaut true en P2.0, false en HD/P1.0."`
	SmartPoly        bool     `json:"smart_poly,omitempty" jsonschema:"Réduction Smart Poly du mode HD ; différent du générateur Smart Mesh P2.0."`
	GenerateParts    bool     `json:"generate_parts,omitempty"`
	PartsLevel       string   `json:"parts_level,omitempty" jsonschema:"simple, balanced ou detailed ; défaut balanced."`
	GeometryQuality  string   `json:"geometry_quality,omitempty" jsonschema:"standard ou detailed (Ultra Mesh Quality, H3.1)."`
	TextureQuality   string   `json:"texture_quality,omitempty" jsonschema:"standard (2K), detailed (4K), extreme (8K)."`
	TextureAlignment string   `json:"texture_alignment,omitempty" jsonschema:"original_image ou geometry."`
	NoPBR            bool     `json:"no_pbr,omitempty"`
	Delight          bool     `json:"delight,omitempty" jsonschema:"Retirer l'éclairage de la texture."`
	ImageAutofix     bool     `json:"image_autofix,omitempty" jsonschema:"AI Complete ; image unique ou lot HD."`
	NegativePrompt   string   `json:"negative_prompt,omitempty"`
	TPose            bool     `json:"t_pose,omitempty"`
	Count            int      `json:"count,omitempty" jsonschema:"Nombre de variantes P2.0 : 1, 2 ou 4 ; défaut 1."`
	VariationFaces   []int    `json:"variation_faces,omitempty" jsonschema:"Budgets individuels des 2 ou 4 variantes P2.0 ; remplace faces."`
	Symmetry         *bool    `json:"symmetry,omitempty" jsonschema:"P2.0 : choix explicite ; sinon vérification de symétrie de l'image avant génération."`
}

func invalid(message string) error { return fault.New("INVALID_ARGUMENT", message) }
func oneOf(value string, allowed ...string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}
func Bool(v bool) *bool                    { return &v }
func (in GenerateInput) IsSmartMesh() bool { return in.Model == P20 || in.Model == P10 }

func (in GenerateInput) Normalize() (GenerateInput, error) {
	in.Prompt = strings.TrimSpace(in.Prompt)
	sources := 0
	for _, present := range []bool{in.Prompt != "", in.Image != "", len(in.Images) > 0, len(in.BatchImages) > 0} {
		if present {
			sources++
		}
	}
	if sources != 1 {
		return in, invalid("Fournir une seule source : prompt, image, images ou batch_images.")
	}
	if len(in.Prompt) > 8000 || len(in.NegativePrompt) > 8000 {
		return in, invalid("Prompt trop long (maximum 8000 octets).")
	}
	switch strings.ToLower(in.Model) {
	case "", "h3.1":
		in.Model = HD31
	case "h3.0":
		in.Model = "v3.0-20250812"
	case "h2.5":
		in.Model = "v2.5-20250123"
	case "p2.0":
		in.Model = P20
	case "p1.0":
		in.Model = P10
	}
	if !oneOf(in.Model, HD31, "v3.0-20250812", "v2.5-20250123", P20, P10) {
		return in, invalid("Modèle non reconnu ; consulter capabilities.")
	}
	if in.Visibility == "" {
		in.Visibility = "private"
	}
	if !oneOf(in.Visibility, "private", "shareable", "public") {
		return in, invalid("Visibilité attendue : private, shareable ou public.")
	}
	if in.Quad == nil {
		in.Quad = Bool(in.Model == P20)
	}
	if in.Faces == 0 {
		in.Faces = 20000
		if in.IsSmartMesh() {
			in.Faces = 5000
		}
	}
	if in.GeometryQuality == "" {
		in.GeometryQuality = "standard"
	}
	if in.TextureQuality == "" {
		in.TextureQuality = "standard"
	}
	if in.TextureAlignment == "" {
		in.TextureAlignment = "original_image"
	}
	if in.PartsLevel == "" {
		in.PartsLevel = "balanced"
	}
	if !oneOf(in.GeometryQuality, "standard", "detailed") || !oneOf(in.TextureQuality, "standard", "detailed", "extreme") || !oneOf(in.TextureAlignment, "original_image", "geometry") || !oneOf(in.PartsLevel, "simple", "balanced", "detailed") {
		return in, invalid("Qualité, alignement ou niveau de parties invalide ; consulter capabilities.")
	}
	if len(in.Images) > 0 {
		if err := ValidateViews(in.Images); err != nil {
			return in, err
		}
	}
	if len(in.BatchImages) > 100 {
		return in, invalid("Maximum 100 images par lot local ; les limites du compte Studio s'appliquent aussi.")
	}
	for _, p := range in.BatchImages {
		if p == "" {
			return in, invalid("Une image du lot est vide.")
		}
	}
	if in.Prompt == "" && (in.NegativePrompt != "" || in.TPose) {
		return in, invalid("negative_prompt et t_pose nécessitent un prompt.")
	}
	if in.ImageAutofix && in.Image == "" && len(in.BatchImages) == 0 {
		return in, invalid("AI Complete nécessite une image unique ou un lot HD.")
	}
	if in.IsSmartMesh() {
		if in.GenerateParts || in.SmartPoly || in.GeometryQuality != "standard" || in.TextureQuality != "standard" || in.TextureAlignment != "original_image" || in.Delight || in.ImageAutofix || in.PartsLevel != "balanced" {
			return in, invalid("Les réglages HD et texture ne s'appliquent pas à Smart Mesh ; utiliser texture ensuite.")
		}
		in.NoTexture = true
		in.NoPBR = true
	} else {
		if in.Model != HD31 && in.GeometryQuality != "standard" {
			return in, invalid("Ultra Mesh Quality nécessite H3.1.")
		}
		if in.GenerateParts && (*in.Quad || !in.NoTexture) {
			return in, invalid("Generate in Parts nécessite triangles et no_texture.")
		}
		if in.NoTexture && (in.TextureQuality != "standard" || in.Delight || in.TextureAlignment != "original_image") {
			return in, invalid("Ces réglages de texture nécessitent une texture active.")
		}
	}
	if in.Model != P20 && in.Symmetry != nil {
		return in, invalid("symmetry est réservé à P2.0.")
	}
	if len(in.VariationFaces) > 0 {
		if in.Count != 0 && in.Count != len(in.VariationFaces) {
			return in, invalid("count doit correspondre à variation_faces.")
		}
		in.Count = len(in.VariationFaces)
	}
	if in.Count == 0 {
		in.Count = 1
	}
	if in.Count != 1 && in.Count != 2 && in.Count != 4 {
		return in, invalid("count doit valoir 1, 2 ou 4.")
	}
	if in.Count > 1 && (in.Model != P20 || len(in.BatchImages) > 0) {
		return in, invalid("Les variantes nécessitent P2.0 hors lot d'images.")
	}
	if len(in.VariationFaces) == 1 {
		return in, invalid("variation_faces nécessite 2 ou 4 budgets.")
	}
	low, high := in.FaceRange()
	for _, f := range append([]int{in.Faces}, in.VariationFaces...) {
		if f < low || f > high {
			return in, invalid("Budget hors limites du modèle/topologie ; consulter capabilities.")
		}
	}
	return in, nil
}

func (in GenerateInput) FaceRange() (int, int) {
	low, high := 500, 1000000
	if in.GeometryQuality == "detailed" {
		high = 2000000
	}
	if in.Quad != nil && *in.Quad {
		high = 50000
	}
	if in.SmartPoly {
		high = 20000
		if *in.Quad {
			high = 10000
		}
	}
	if in.Model == P10 {
		high = 20000
	}
	if in.Model == P20 {
		high = 50000
		if *in.Quad {
			high = 25000
		}
	}
	if in.GenerateParts {
		low = 10000
	}
	return low, high
}

func ValidateViews(paths []string) error {
	count := 0
	for _, p := range paths {
		if p != "" {
			count++
		}
	}
	if len(paths) != 4 || paths[0] == "" || count < 2 {
		return invalid("Multivues : quatre emplacements avant/gauche/arrière/droite, dont avant et au moins une autre vue.")
	}
	return nil
}

func (in GenerateInput) FilePaths() []string {
	if in.Image != "" {
		return []string{in.Image}
	}
	if len(in.Images) > 0 {
		return in.Images
	}
	return in.BatchImages
}

type Submitted struct {
	Receipt
	Accepted bool `json:"accepted"`
}

// Retain every accepted receipt even when siblings failed. An incomplete reply
// remains uncertain and never becomes an automatic retry.
func decodeSubmission(raw json.RawMessage) ([]Submitted, error) {
	var list []Submitted
	if len(raw) > 0 && raw[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil, uncertain(true, "", "")
		}
		var incomplete error
		for _, item := range items {
			entries, err := decodeSubmission(item)
			list = append(list, entries...)
			if err != nil {
				incomplete = err
			}
		}
		if incomplete != nil {
			return list, incomplete
		}
	} else {
		var obj struct {
			Receipt
			Variations []Submitted `json:"variations"`
		}
		if json.Unmarshal(raw, &obj) != nil {
			return nil, uncertain(true, "", "")
		}
		if obj.Variations != nil {
			list = obj.Variations
		} else if obj.ProjectID != "" && obj.OperatorID != "" {
			list = []Submitted{{Receipt: obj.Receipt, Accepted: true}}
		}
	}
	if len(list) == 0 {
		return nil, uncertain(true, "", "")
	}
	for _, v := range list {
		if v.Accepted && (v.ProjectID == "" || v.OperatorID == "") {
			return list, uncertain(true, "", "")
		}
	}
	return list, nil
}

func (c *Client) Generate(ctx context.Context, in GenerateInput, images []*Image) ([]Submitted, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, err
	}
	body := map[string]any{"model_version": in.Model, "face_limit": in.Faces, "quad": *in.Quad, "visibility": in.Visibility}
	if in.Count > 1 {
		variations := []map[string]int{}
		for i := 0; i < in.Count; i++ {
			f := in.Faces
			if len(in.VariationFaces) > 0 {
				f = in.VariationFaces[i]
			}
			variations = append(variations, map[string]int{"face_limit": f})
		}
		body["variations"] = variations
		delete(body, "face_limit")
	}
	if !in.IsSmartMesh() {
		body["texture"] = !in.NoTexture
		body["generate_parts"] = in.GenerateParts
		body["smart_poly"] = in.SmartPoly
		if in.GenerateParts {
			body["segmentation_granularity"] = in.PartsLevel
		}
		if in.Model == HD31 {
			body["geometry_quality"] = in.GeometryQuality
		}
		if !in.NoTexture {
			body["pbr"] = !in.NoPBR
			body["delight"] = in.Delight
			body["texture_quality"] = in.TextureQuality
			body["texture_alignment"] = in.TextureAlignment
		}
		if in.Image != "" || len(in.BatchImages) > 0 {
			body["enable_image_autofix"] = in.ImageAutofix
		}
	}
	path := "/v2/studio/operation/text_to_model"
	if in.Prompt != "" {
		body["prompt"] = in.Prompt
		body["negative_prompt"] = in.NegativePrompt
		body["gen_image_model_version"] = "flux.1_dev"
		body["t_pose"] = in.TPose
		body["sketch_to_render"] = false
	} else {
		if len(images) != len(in.FilePaths()) {
			return nil, invalid("Images non téléversées.")
		}
		for i, p := range in.FilePaths() {
			if p != "" && images[i] == nil {
				return nil, invalid("Image non téléversée.")
			}
		}
		if in.Image != "" {
			path = "/v2/studio/operation/image_to_model"
			body["image"] = images[0]
		} else if len(in.Images) > 0 {
			path = "/v2/studio/operation/multiview_to_model"
			body["image"] = images
		} else {
			path = "/v2/studio/operation/batch_image_to_model"
			batch := []map[string]any{}
			for _, im := range images {
				entry := map[string]any{"bucket": im.Bucket, "key": im.Key, "image_audit_result": im.Audit, "image_source": im.Source}
				if !in.IsSmartMesh() {
					entry["texture_quality"] = in.TextureQuality
				}
				batch = append(batch, entry)
			}
			body["image"] = batch
		}
	}
	if in.Model == P20 {
		symmetric := false
		if in.Symmetry != nil {
			symmetric = *in.Symmetry
		} else if len(images) > 0 && images[0] != nil {
			var out struct {
				Symmetry bool `json:"symmetry"`
			}
			err = c.request(ctx, "POST", "/v2/studio/operation/symmetry_check", map[string]any{"image": map[string]string{"bucket": images[0].Bucket, "key": images[0].Key}}, &out, false)
			if err != nil {
				return nil, err
			}
			symmetric = out.Symmetry
		}
		body["symmetry"] = symmetric
	}
	var raw json.RawMessage
	if err = c.request(ctx, "POST", path, body, &raw, true); err != nil {
		return nil, err
	}
	entries, err := decodeSubmission(raw)
	expected := in.Count
	if len(in.BatchImages) > 0 {
		expected = len(in.BatchImages)
	}
	if err == nil && len(entries) != expected {
		err = uncertain(true, "", "")
	}
	return entries, err
}
