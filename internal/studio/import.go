package studio

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
)

type ImportInput struct {
	ModelFile     string    `json:"model_file" jsonschema:"Chemin absolu GLB, FBX, OBJ ou STL ; limite locale 100 Mo."`
	Name          string    `json:"name,omitempty"`
	UseOriginalUV *bool     `json:"use_original_uv,omitempty" jsonschema:"Conserver les UV existants ; défaut true."`
	Transform     []float64 `json:"transform,omitempty" jsonschema:"Matrice 4x4 column-major ; identité par défaut."`
}

func (in ImportInput) Normalize() (ImportInput, error) {
	if _, err := ValidateInputModel(in.ModelFile); err != nil {
		return in, err
	}
	if in.Name == "" {
		in.Name = filepath.Base(in.ModelFile)
	}
	if len(in.Name) > 256 {
		return in, invalid("Nom de modèle trop long.")
	}
	if in.UseOriginalUV == nil {
		in.UseOriginalUV = Bool(true)
	}
	if len(in.Transform) == 0 {
		in.Transform = []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	}
	if len(in.Transform) != 16 {
		return in, invalid("La matrice doit contenir 16 nombres.")
	}
	for _, n := range in.Transform {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return in, invalid("Matrice invalide.")
		}
	}
	return in, nil
}
func (c *Client) Import(ctx context.Context, in ImportInput, model *Image) (Receipt, error) {
	in, err := in.Normalize()
	if err != nil {
		return Receipt{}, err
	}
	if model == nil {
		return Receipt{}, invalid("Modèle non téléversé.")
	}
	format, _ := ValidateInputModel(in.ModelFile)
	body := map[string]any{"format": format, "model": map[string]string{"bucket": model.Bucket, "key": model.Key}, "name": in.Name, "transform_matrix": in.Transform, "use_original_uv": *in.UseOriginalUV}
	var raw json.RawMessage
	if err = c.request(ctx, "POST", "/v2/studio/operation/import_user_model", body, &raw, true); err != nil {
		return Receipt{}, err
	}
	entries, err := decodeSubmission(raw)
	if err != nil {
		return Receipt{}, err
	}
	if len(entries) != 1 || !entries[0].Accepted {
		return Receipt{}, uncertain(true, "", "")
	}
	return entries[0].Receipt, nil
}
