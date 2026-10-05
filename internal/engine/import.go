package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"io"
	"os"
)

type ImportRequest struct {
	studio.ImportInput
	RequestID string `json:"request_id,omitempty"`
	Confirm   bool   `json:"confirm"`
}

func modelHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, io.LimitReader(f, studio.MaxModelBytes+1))
	return hex.EncodeToString(h.Sum(nil)), err
}
func (e *Engine) Import(ctx context.Context, in ImportRequest) (View, error) {
	if !in.Confirm {
		return View{}, fault.New("CONFIRM_REQUIRED", "L'import crée un projet Studio ; autorisation requise (--yes).")
	}
	params, err := in.ImportInput.Normalize()
	if err != nil {
		return View{}, err
	}
	digest, err := modelHash(params.ModelFile)
	if err != nil {
		return View{}, err
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return View{}, err
	}
	hash, err := fingerprint(struct {
		Input  studio.ImportInput
		SHA256 string
	}{params, digest})
	if err != nil {
		return View{}, err
	}
	if in.RequestID == "" {
		in.RequestID = rand.Text()
	}
	j, unlock, exists, err := e.begin(in.RequestID, "import", "glb", s.Account, hash)
	if err != nil {
		return View{}, err
	}
	defer unlock()
	if exists {
		return j.View(), nil
	}
	model, err := c.UploadModel(ctx, params.ModelFile)
	if err != nil {
		return e.failPreparing(&j, err)
	}
	actual, err := modelHash(params.ModelFile)
	if err != nil {
		return e.failPreparing(&j, err)
	}
	if actual != digest {
		return e.failPreparing(&j, fault.New("INPUT_CHANGED", "Le modèle a changé pendant l'envoi ; import non soumis."))
	}
	j.State = "outcome_unknown"
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	j.Receipt, err = c.Import(ctx, params, model)
	return e.finish(&j, err)
}

type AttachRequest struct {
	ProjectID string `json:"project_id"`
	RequestID string `json:"request_id,omitempty"`
}

func (e *Engine) Attach(ctx context.Context, in AttachRequest) (View, error) {
	if in.ProjectID == "" || len(in.ProjectID) > 128 {
		return View{}, fault.New("INVALID_ARGUMENT", "Identifiant de projet Studio requis.")
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return View{}, err
	}
	hash, err := fingerprint(in.ProjectID)
	if err != nil {
		return View{}, err
	}
	if in.RequestID == "" {
		in.RequestID = rand.Text()
	}
	j, unlock, exists, err := e.begin(in.RequestID, "attach", "glb", s.Account, hash)
	if err != nil {
		return View{}, err
	}
	defer unlock()
	if exists {
		return j.View(), nil
	}
	detail, err := c.Project(ctx, in.ProjectID, "")
	if err != nil {
		return e.failPreparing(&j, err)
	}
	op, _ := detail["operator"].(map[string]any)
	operator, _ := op["operator_id"].(string)
	if operator == "" || studio.AssetURL(detail) == "" || projectRunning(detail) {
		return e.failPreparing(&j, fault.New("NOT_READY", "Le projet n'a pas de version terminée reconnue."))
	}
	j.Receipt = studio.Receipt{ProjectID: in.ProjectID, OperatorID: operator}
	j.State = "success"
	j.Progress = 100
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	return j.View(), nil
}
