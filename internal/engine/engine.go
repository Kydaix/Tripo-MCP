// Package engine is the shared, durable CLI/MCP application layer.
package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
)

type Engine struct {
	Root   string
	Client func(auth.Session) *studio.Client
}

func New(root string) *Engine { return &Engine{Root: root, Client: studio.New} }

type Job struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	State       string           `json:"state"`
	Account     string           `json:"account"`
	RequestHash string           `json:"request_hash"`
	Receipt     studio.Receipt   `json:"receipt"`
	Format      string           `json:"format"`
	Progress    float64          `json:"progress"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Error       *fault.Error     `json:"error,omitempty"`
	Artifact    *studio.Artifact `json:"artifact,omitempty"`
	DownloadRef []byte           `json:"download_ref,omitempty"`
}

// View is the public job representation; credentials, URLs and internal identity never escape.
type View struct {
	ID         string           `json:"job_id"`
	Kind       string           `json:"kind"`
	State      string           `json:"state"`
	ProjectID  string           `json:"project_id,omitempty"`
	OperatorID string           `json:"operator_id,omitempty"`
	Progress   float64          `json:"progress"`
	CreatedAt  time.Time        `json:"created_at"`
	Error      *fault.Error     `json:"error,omitempty"`
	Artifact   *studio.Artifact `json:"artifact,omitempty"`
}

func (j Job) View() View {
	return View{ID: j.ID, Kind: j.Kind, State: j.State, ProjectID: j.Receipt.ProjectID, OperatorID: j.Receipt.OperatorID, Progress: j.Progress, CreatedAt: j.CreatedAt, Error: j.Error, Artifact: j.Artifact}
}

func (e *Engine) client(ctx context.Context) (auth.Session, *studio.Client, error) {
	s, err := auth.Load(e.Root)
	if err != nil {
		return s, nil, err
	}
	return s, e.Client(s), nil
}
func (e *Engine) save(j *Job) error {
	p, err := local.JobPath(e.Root, j.ID)
	if err != nil {
		return err
	}
	j.UpdatedAt = time.Now().UTC()
	return local.WriteJSON(p, j)
}
func (e *Engine) read(id string) (Job, error) {
	var j Job
	p, err := local.JobPath(e.Root, id)
	if err != nil {
		return j, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return j, fault.New("JOB_NOT_FOUND", "Tâche locale introuvable.")
	}
	if err != nil {
		return j, err
	}
	if json.Unmarshal(b, &j) != nil || j.ID != id {
		return j, fault.New("LOCAL_ERROR", "Journal de tâche illisible.")
	}
	return j, nil
}

func (e *Engine) Status(ctx context.Context) (any, error) {
	_, c, err := e.client(ctx)
	if err != nil {
		return map[string]any{"authenticated": false, "login_command": "tripo-mcp login"}, err
	}
	a, err := c.Account(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"authenticated": true, "expires_at": c.Session.Expires, "studio": a}, nil
}

func (e *Engine) ImportSession(ctx context.Context, s auth.Session) (auth.Status, error) {
	verified, err := auth.FromHeader("Bearer "+s.Token, s.DeviceID, s.Region)
	if err != nil {
		return auth.Status{}, err
	}
	if _, err = e.Client(verified).Account(ctx); err != nil {
		return auth.Status{}, err
	}
	unlock, err := local.Lock(filepath.Join(e.Root, "auth.lock"))
	if err != nil {
		return auth.Status{}, err
	}
	defer unlock()
	if err = auth.Save(e.Root, verified); err != nil {
		return auth.Status{}, err
	}
	return auth.Inspect(e.Root), nil
}

func (e *Engine) Login(ctx context.Context, ready func(string)) (auth.Status, error) {
	return auth.Login(ctx, e.Root, ready, func(ctx context.Context, s auth.Session) error { _, err := e.Client(s).Account(ctx); return err })
}

type GenerateRequest struct {
	studio.GenerateInput
	RequestID string `json:"request_id,omitempty" jsonschema:"Identifiant stable de la demande. Réutiliser le même après une interruption ; jamais pour une nouvelle variante."`
	Confirm   bool   `json:"confirm" jsonschema:"true uniquement si l'utilisateur a autorisé cette génération sur ses crédits Studio."`
}

func fingerprint(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (e *Engine) Generate(ctx context.Context, in GenerateRequest) (View, error) {
	var empty View
	if !in.Confirm {
		return empty, fault.New("CONFIRM_REQUIRED", "La génération consomme des crédits Studio ; autorisation requise (--yes).")
	}
	params, err := in.GenerateInput.Normalize()
	if err != nil {
		return empty, err
	}
	imageHash := ""
	if params.Image != "" {
		if _, err = studio.ValidateImage(params.Image); err != nil {
			return empty, err
		}
		f, err := os.Open(params.Image)
		if err != nil {
			return empty, err
		}
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(f, studio.MaxImageBytes+1))
		f.Close()
		if err != nil {
			return empty, err
		}
		imageHash = hex.EncodeToString(h.Sum(nil))
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return empty, err
	}
	hash, err := fingerprint(struct {
		Input     studio.GenerateInput
		ImageHash string
		Account   string
	}{params, imageHash, s.Account})
	if err != nil {
		return empty, err
	}
	if in.RequestID == "" {
		in.RequestID = rand.Text()
	}
	j, unlock, existing, err := e.begin(in.RequestID, "generate", "glb", s.Account, hash)
	if err != nil {
		return empty, err
	}
	defer unlock()
	if existing {
		return j.View(), nil
	}
	var image *studio.Image
	if params.Image != "" {
		image, err = c.Upload(ctx, params.Image)
		if err != nil {
			j.State = "failed"
			j.Error = fault.Public(err)
			if saveErr := e.save(&j); saveErr != nil {
				return j.View(), saveErr
			}
			return j.View(), err
		}
	}
	// Persist uncertainty before dispatch; a crash at any later instruction cannot trigger a second charge.
	j.State = "outcome_unknown"
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	j.Receipt, err = c.Generate(ctx, params, image)
	if err != nil {
		j.Error = fault.Public(err)
		if !studio.IsUncertain(err) {
			j.State = "failed"
		}
	} else {
		j.State = "submitted"
	}
	if saveErr := e.save(&j); saveErr != nil {
		return j.View(), fault.New("OUTCOME_UNKNOWN", "Réponse reçue mais journal non enregistré ; vérifier Studio avant toute nouvelle demande.")
	}
	return j.View(), err
}

func (e *Engine) begin(id, kind, format, account, hash string) (Job, func(), bool, error) {
	p, err := local.JobPath(e.Root, id)
	if err != nil {
		return Job{}, nil, false, err
	}
	unlock, err := local.Lock(p + ".lock")
	if err != nil {
		return Job{}, nil, false, err
	}
	if _, err = os.Stat(p); err == nil {
		j, err := e.read(id)
		if err == nil && (j.RequestHash != hash || j.Account != account || j.Kind != kind) {
			err = fault.New("REQUEST_CONFLICT", "Cet identifiant appartient déjà à une autre demande.")
		}
		if err != nil {
			unlock()
			return j, nil, false, err
		}
		return j, unlock, true, nil
	} else if !os.IsNotExist(err) {
		unlock()
		return Job{}, nil, false, err
	}
	j := Job{ID: id, Kind: kind, Format: format, State: "preparing", Account: account, RequestHash: hash, CreatedAt: time.Now().UTC()}
	if err = e.save(&j); err != nil {
		unlock()
		return j, nil, false, err
	}
	return j, unlock, false, nil
}

func (e *Engine) List() ([]View, error) {
	files, err := filepath.Glob(filepath.Join(e.Root, "jobs", "*.json"))
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, f := range files {
		id := filepath.Base(f)
		id = id[:len(id)-5]
		j, err := e.read(id)
		if err != nil {
			return nil, err
		}
		out = append(out, j.View())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (e *Engine) Poll(ctx context.Context, id string) (View, error) {
	p, err := local.JobPath(e.Root, id)
	if err != nil {
		return View{}, err
	}
	unlock, err := local.Lock(p + ".lock")
	if err != nil {
		return View{}, err
	}
	defer unlock()
	j, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	if j.State == "downloaded" || studio.Terminal(j.State) || j.State == "outcome_unknown" || j.Receipt.OperatorID == "" {
		return j.View(), nil
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return j.View(), err
	}
	if s.Account != j.Account {
		return j.View(), fault.New("ACCOUNT_CHANGED", "Cette tâche appartient à une autre session Studio.")
	}
	progress, err := c.Progress(ctx, j.Receipt.OperatorID)
	if err != nil {
		return j.View(), err
	}
	j.Progress = progress.Progress
	j.State = progress.Status
	if j.State == "" {
		return j.View(), fault.New("PROTOCOL_CHANGED", "État de génération absent.")
	}
	if progress.ModelURL != "" {
		j.DownloadRef, err = local.Protect([]byte(progress.ModelURL))
		if err != nil {
			return j.View(), err
		}
	}
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	return j.View(), nil
}

func (e *Engine) Wait(ctx context.Context, id string) (View, error) {
	for {
		v, err := e.Poll(ctx, id)
		if err != nil {
			return v, err
		}
		if studio.Terminal(v.State) || v.State == "downloaded" {
			return v, nil
		}
		if v.State == "outcome_unknown" || v.State == "preparing" {
			return v, fault.New("OUTCOME_UNKNOWN", "Vérifier cette tâche dans Studio ; aucune nouvelle soumission automatique.")
		}
		select {
		case <-ctx.Done():
			return v, fault.New("WAIT_TIMEOUT", "Attente interrompue ; la génération distante continue. Reprendre avec wait ou status.")
		case <-time.After(5 * time.Second):
		}
	}
}

func (e *Engine) Download(ctx context.Context, id, path string) (View, error) {
	p, err := local.JobPath(e.Root, id)
	if err != nil {
		return View{}, err
	}
	unlock, err := local.Lock(p + ".lock")
	if err != nil {
		return View{}, err
	}
	defer unlock()
	j, err := e.read(id)
	if err != nil {
		return View{}, err
	}
	if j.State == "downloaded" && j.Artifact != nil {
		if info, err := os.Stat(j.Artifact.Path); err == nil && info.Size() == j.Artifact.Bytes {
			return j.View(), nil
		}
		j.State = "success"
		j.Artifact = nil
	}
	if j.State != "success" {
		return j.View(), fault.New("NOT_READY", "La tâche doit avoir réussi ; lancer status ou wait avant download.")
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return j.View(), err
	}
	if s.Account != j.Account {
		return j.View(), fault.New("ACCOUNT_CHANGED", "Cette tâche appartient à un autre compte Studio.")
	}
	remote := ""
	if j.Kind == "generate" {
		project, err := c.Project(ctx, j.Receipt.ProjectID, j.Receipt.OperatorID)
		if err != nil {
			return j.View(), err
		}
		remote = studio.AssetURL(project)
	} else if len(j.DownloadRef) > 0 {
		b, err := local.Unprotect(j.DownloadRef)
		if err != nil {
			return j.View(), err
		}
		remote = string(b)
		clear(b)
	}
	if j.Kind == "export" && remote == "" {
		remote, err = c.ExportURL(ctx, j.Receipt.OperatorID)
		if err != nil {
			return j.View(), err
		}
	}
	if remote == "" {
		return j.View(), fault.New("ASSET_UNAVAILABLE", "Studio n'a pas fourni le fichier de ce modèle ; vérifier son état ou demander un export explicite.")
	}
	if path == "" {
		path = filepath.Join(e.Root, "downloads", id, "model."+j.Format)
	}
	artifact, err := studio.Download(ctx, remote, path, j.Format)
	if err != nil {
		return j.View(), err
	}
	j.Artifact = &artifact
	j.State = "downloaded"
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	return j.View(), nil
}

type ExportRequest struct {
	JobID     string `json:"job_id"`
	RequestID string `json:"request_id,omitempty"`
	Format    string `json:"format"`
	Confirm   bool   `json:"confirm"`
}

func (e *Engine) Export(ctx context.Context, in ExportRequest) (View, error) {
	if !in.Confirm {
		return View{}, fault.New("CONFIRM_REQUIRED", "L'export peut consommer des crédits Studio ; autorisation requise (--yes).")
	}
	if in.Format != "glb" && in.Format != "fbx" {
		return View{}, fault.New("INVALID_ARGUMENT", "Format attendu : glb ou fbx.")
	}
	source, err := e.read(in.JobID)
	if err != nil {
		return View{}, err
	}
	if source.State != "success" && source.State != "downloaded" {
		return source.View(), fault.New("NOT_READY", "Le modèle doit être terminé avant l'export.")
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return View{}, err
	}
	if s.Account != source.Account {
		return View{}, fault.New("ACCOUNT_CHANGED", "Cette tâche appartient à un autre compte.")
	}
	input := studio.ExportInput{Format: in.Format, Project: source.Receipt.ProjectID}
	hash, err := fingerprint(input)
	if err != nil {
		return View{}, err
	}
	if in.RequestID == "" {
		in.RequestID = rand.Text()
	}
	j, unlock, existing, err := e.begin(in.RequestID, "export", in.Format, s.Account, hash)
	if err != nil {
		return View{}, err
	}
	defer unlock()
	if existing {
		return j.View(), nil
	}
	j.State = "outcome_unknown"
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	var remote string
	j.Receipt, remote, err = c.Export(ctx, input)
	if err != nil {
		j.Error = fault.Public(err)
		if !studio.IsUncertain(err) {
			j.State = "failed"
		}
	} else {
		j.State = "submitted"
		if remote != "" {
			j.State = "success"
			j.DownloadRef, err = local.Protect([]byte(remote))
		}
	}
	if saveErr := e.save(&j); saveErr != nil {
		return j.View(), fault.New("OUTCOME_UNKNOWN", "Export envoyé mais journal non enregistré ; vérifier Studio.")
	}
	return j.View(), err
}
