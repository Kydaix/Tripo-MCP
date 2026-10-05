package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

func imageHashes(paths []string) ([]string, error) {
	hashes := make([]string, len(paths))
	for i, p := range paths {
		if p == "" {
			continue
		}
		if _, err := studio.ValidateImage(p); err != nil {
			return nil, err
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(f, studio.MaxImageBytes+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		hashes[i] = hex.EncodeToString(h.Sum(nil))
	}
	return hashes, nil
}
func uploadImages(ctx context.Context, c *studio.Client, paths []string) ([]*studio.Image, error) {
	out := make([]*studio.Image, len(paths))
	for i, p := range paths {
		if p == "" {
			continue
		}
		image, err := c.Upload(ctx, p)
		if err != nil {
			return nil, err
		}
		out[i] = image
	}
	return out, nil
}
func sameImages(paths, expected []string) error {
	actual, err := imageHashes(paths)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return fault.New("INPUT_CHANGED", "Une image a changé pendant le téléversement ; aucune génération envoyée.")
	}
	return nil
}

type EditRequest struct {
	studio.EditInput
	JobID     string `json:"job_id" jsonschema:"Tâche source terminée, ou variante job:1. L'opération modifie la version courante du projet Studio."`
	RequestID string `json:"request_id,omitempty"`
	Confirm   bool   `json:"confirm"`
}

func (e *Engine) source(id string) (Job, error) {
	source, err := e.read(id)
	if err != nil {
		return source, err
	}
	if len(source.Variants) > 0 {
		return source, fault.New("INVALID_ARGUMENT", "Choisir une variante du lot, par exemple job:1.")
	}
	if source.State != "success" && source.State != "downloaded" {
		return source, fault.New("NOT_READY", "Le modèle source doit être terminé.")
	}
	if source.Receipt.ProjectID == "" || source.Receipt.OperatorID == "" {
		return source, fault.New("NOT_READY", "Le modèle source n'a pas de reçu complet.")
	}
	return source, nil
}

func currentSource(ctx context.Context, c *studio.Client, source Job) (map[string]any, error) {
	detail, err := c.Project(ctx, source.Receipt.ProjectID, "")
	if err != nil {
		return nil, err
	}
	op, _ := detail["operator"].(map[string]any)
	if op["operator_id"] != source.Receipt.OperatorID {
		return nil, fault.New("SOURCE_CHANGED", "Le projet Studio a une autre version courante ; utiliser la tâche correspondant à cette version. Aucune restauration automatique.")
	}
	if projectRunning(detail) {
		return nil, fault.New("NOT_READY", "Une opération est déjà en cours sur ce projet Studio.")
	}
	return op, nil
}

func projectRunning(detail map[string]any) bool {
	v := detail["running_operator"]
	return v != nil && v != false && v != ""
}
func (e *Engine) projectLock(account, project string) (func(), error) {
	key := sha256.Sum256([]byte(account + ":" + project))
	return local.Lock(filepath.Join(e.Root, "projects", hex.EncodeToString(key[:])+".lock"))
}

func (e *Engine) Edit(ctx context.Context, in EditRequest) (View, error) {
	if !in.Confirm {
		return View{}, fault.New("CONFIRM_REQUIRED", "Cette opération utilise les crédits Studio ; autorisation requise (--yes).")
	}
	params, err := in.EditInput.Normalize()
	if err != nil {
		return View{}, err
	}
	hashes, err := imageHashes(params.FilePaths())
	if err != nil {
		return View{}, err
	}
	source, err := e.source(in.JobID)
	if err != nil {
		return View{}, err
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return View{}, err
	}
	if s.Account != source.Account {
		return View{}, fault.New("ACCOUNT_CHANGED", "Cette tâche appartient à un autre compte.")
	}
	hash, err := fingerprint(struct {
		Input  studio.EditInput
		Source studio.Receipt
		Images []string
	}{params, source.Receipt, hashes})
	if err != nil {
		return View{}, err
	}
	if in.RequestID == "" {
		in.RequestID = rand.Text()
	}
	j, unlock, exists, err := e.begin(in.RequestID, params.Operation, "glb", s.Account, hash)
	if err != nil {
		return View{}, err
	}
	defer unlock()
	if exists {
		return j.View(), nil
	}
	projectUnlock, err := e.projectLock(s.Account, source.Receipt.ProjectID)
	if err != nil {
		return e.failPreparing(&j, err)
	}
	defer projectUnlock()
	op, err := currentSource(ctx, c, source)
	if err != nil {
		return e.failPreparing(&j, err)
	}
	// Inspect the actual Studio result, even when callers supply part names.
	// Passing use_original_uv=false does not prove the service rebuilt it.
	if params.Operation == "texture" && (source.Kind == "import" || source.ImportInfo != nil) {
		model, err := e.Download(ctx, source.ID, "")
		if err != nil {
			return e.failPreparing(&j, err)
		}
		if err = studio.CheckTextureUV(model.Inspection); err != nil {
			return e.failPreparing(&j, err)
		}
		verifiedOriginal := source.UVSourceVerified
		if (model.Inspection == nil || model.Inspection.UVStatus != "checked") && !verifiedOriginal && !params.AllowUnverifiedUV {
			return e.failPreparing(&j, fault.New("UV_UNVERIFIED", "Les UV de ce modèle importé ne sont pas inspectables localement. Réimporter un GLB avec un atlas validé et use_original_uv=true, ou vérifier les UV dans un DCC puis utiliser allow_unverified_uv=true. Aucun texturage envoyé."))
		}
	}
	if len(params.Parts) == 0 && (params.Operation == "texture" || params.Operation == "remesh") {
		model, err := e.Download(ctx, source.ID, "")
		if err != nil {
			return e.failPreparing(&j, err)
		}
		if model.Artifact == nil {
			return e.failPreparing(&j, fault.New("NOT_READY", "Fichier source non disponible."))
		}
		params.Parts, err = studio.Parts(model.Artifact.Path)
		if err != nil {
			return e.failPreparing(&j, err)
		}
	}
	if (params.Operation == "remesh" || params.Operation == "segment" || params.Operation == "fill" || params.Operation == "complete") && op["is_rigged"] == true {
		return e.failPreparing(&j, fault.New("INVALID_ARGUMENT", "Cette opération ne prend pas en charge les modèles déjà riggés."))
	}
	if params.Operation == "upscale" && op["is_textured"] != true {
		return e.failPreparing(&j, fault.New("INVALID_ARGUMENT", "Upscale nécessite un modèle texturé."))
	}
	if params.Operation == "animate" && op["is_rigged"] != true {
		return e.failPreparing(&j, fault.New("INVALID_ARGUMENT", "L'animation nécessite un modèle riggé."))
	}
	images, err := uploadImages(ctx, c, params.FilePaths())
	if err != nil {
		return e.failPreparing(&j, err)
	}
	if err = sameImages(params.FilePaths(), hashes); err != nil {
		return e.failPreparing(&j, err)
	}
	// Downloads, part resolution and uploads can outlive a change in the browser.
	if _, err = currentSource(ctx, c, source); err != nil {
		return e.failPreparing(&j, err)
	}
	j.State = "outcome_unknown"
	j.Retryable = false
	j.Warnings = source.Warnings
	j.ImportInfo = source.ImportInfo
	// Geometry/UV-changing operations invalidate the original-atlas evidence.
	j.UVSourceVerified = source.UVSourceVerified && (params.Operation == "texture" || params.Operation == "upscale" || params.Operation == "pbr")
	j.Credits = &studio.Credits{Estimate: studio.EstimateEdit(params)}
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	j.Receipt, err = c.Edit(ctx, source.Receipt.ProjectID, params, images)
	return e.finish(&j, err)
}
func (e *Engine) failPreparing(j *Job, err error) (View, error) {
	j.State = "failed"
	j.Retryable = true
	j.Error = fault.Public(err)
	if saveErr := e.save(j); saveErr != nil {
		return j.View(), saveErr
	}
	return j.View(), err
}

// Resume only an explicit identical call whose journal proves no submission.
// Legacy audit/upload errors also occurred exclusively before dispatch.
func (j Job) canResume() bool {
	if j.Receipt.ProjectID != "" || j.Receipt.OperatorID != "" || len(j.Variants) != 0 {
		return false
	}
	if j.State == "preparing" {
		return true
	}
	if j.State != "failed" {
		return false
	}
	if j.Retryable {
		return true
	}
	if j.Error != nil {
		switch j.Error.Code {
		case "IMAGE_REJECTED", "IMAGE_REVIEW_REQUIRED", "UPLOAD_FAILED", "INPUT_CHANGED":
			return true
		}
	}
	return false
}
func (e *Engine) finish(j *Job, err error) (View, error) {
	if err != nil {
		j.Error = fault.Public(err)
		if !studio.IsUncertain(err) {
			j.State = "failed"
		}
	} else {
		j.State = "submitted"
	}
	if saveErr := e.save(j); saveErr != nil {
		return j.View(), fault.New("OUTCOME_UNKNOWN", "Réponse reçue mais journal non enregistré ; vérifier Studio avant toute nouvelle demande.")
	}
	return j.View(), err
}
