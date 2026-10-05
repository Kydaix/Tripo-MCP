// Package engine is the shared, durable CLI/MCP application layer.
package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
)

type Engine struct {
	Root   string
	Client func(auth.Session) *studio.Client
	Renew  auth.Renewer
}

func New(root string) *Engine { return &Engine{Root: root, Client: studio.New, Renew: auth.Renew} }

type Job struct {
	Retryable        bool                    `json:"retryable,omitempty"`
	Warnings         []string                `json:"warnings,omitempty"`
	Credits          *studio.Credits         `json:"credits,omitempty"`
	Inspection       *studio.ModelInspection `json:"inspection,omitempty"`
	ImportInfo       *studio.ImportInfo      `json:"import_info,omitempty"`
	UVSourceVerified bool                    `json:"uv_source_verified,omitempty"`
	Variants         []Job                   `json:"variants,omitempty"`
	ID               string                  `json:"id"`
	Kind             string                  `json:"kind"`
	State            string                  `json:"state"`
	Account          string                  `json:"account"`
	RequestHash      string                  `json:"request_hash"`
	Receipt          studio.Receipt          `json:"receipt"`
	Format           string                  `json:"format"`
	Progress         float64                 `json:"progress"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Error            *fault.Error            `json:"error,omitempty"`
	Artifact         *studio.Artifact        `json:"artifact,omitempty"`
	DownloadRef      []byte                  `json:"download_ref,omitempty"`
}

// View is the public job representation; credentials, URLs and internal identity never escape.
type View struct {
	Retryable  bool                    `json:"retryable"`
	Warnings   []string                `json:"warnings,omitempty"`
	Credits    *studio.Credits         `json:"credits,omitempty"`
	Inspection *studio.ModelInspection `json:"inspection,omitempty"`
	ImportInfo *studio.ImportInfo      `json:"import_info,omitempty"`
	Format     string                  `json:"format,omitempty"`
	Variants   []View                  `json:"variants,omitempty"`
	ID         string                  `json:"job_id"`
	Kind       string                  `json:"kind"`
	State      string                  `json:"state"`
	ProjectID  string                  `json:"project_id,omitempty"`
	OperatorID string                  `json:"operator_id,omitempty"`
	Progress   float64                 `json:"progress"`
	CreatedAt  time.Time               `json:"created_at"`
	Error      *fault.Error            `json:"error,omitempty"`
	Artifact   *studio.Artifact        `json:"artifact,omitempty"`
}

func (j Job) View() View {
	v := View{ID: j.ID, Kind: j.Kind, State: j.State, ProjectID: j.Receipt.ProjectID, OperatorID: j.Receipt.OperatorID, Progress: j.Progress, CreatedAt: j.CreatedAt, Error: j.Error, Artifact: j.Artifact, Retryable: j.canResume(), Warnings: j.Warnings, Credits: j.Credits, Inspection: j.Inspection, ImportInfo: j.ImportInfo}
	if j.Artifact != nil {
		v.Format = j.Artifact.Format
	}
	for _, child := range j.Variants {
		v.Variants = append(v.Variants, child.View())
	}
	return v
}

func (e *Engine) client(ctx context.Context) (auth.Session, *studio.Client, error) {
	s, err := auth.Resolve(ctx, e.Root, e.Renew)
	if err != nil {
		return s, nil, err
	}
	c := e.Client(s)
	c.Credentials = func(ctx context.Context) (auth.Session, error) {
		next, err := auth.Resolve(ctx, e.Root, e.Renew)
		if err == nil && next.Account != s.Account {
			return auth.Session{}, fault.New("ACCOUNT_CHANGED", "Le compte Studio a changé ; reprendre la commande avec le compte attendu.")
		}
		return next, err
	}
	return s, c, nil
}
func (e *Engine) save(j *Job) error {
	base, index, err := splitJobID(j.ID)
	if err != nil {
		return err
	}
	p, err := local.JobPath(e.Root, base)
	if err != nil {
		return err
	}
	j.UpdatedAt = time.Now().UTC()
	if index > 0 {
		parent, err := e.read(base)
		if err != nil {
			return err
		}
		if index > len(parent.Variants) {
			return fault.New("JOB_NOT_FOUND", "Variante introuvable.")
		}
		parent.Variants[index-1] = *j
		if parent.Error == nil || parent.Error.Code != "OUTCOME_UNKNOWN" {
			aggregate(&parent)
		}
		parent.UpdatedAt = j.UpdatedAt
		return local.WriteJSON(p, &parent)
	}
	return local.WriteJSON(p, j)
}
func (e *Engine) read(id string) (Job, error) {
	var j Job
	base, index, err := splitJobID(id)
	if err != nil {
		return j, err
	}
	p, err := local.JobPath(e.Root, base)
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
	if json.Unmarshal(b, &j) != nil || j.ID != base {
		return j, fault.New("LOCAL_ERROR", "Journal de tâche illisible.")
	}
	if index > 0 {
		if index > len(j.Variants) {
			return j, fault.New("JOB_NOT_FOUND", "Variante introuvable.")
		}
		return j.Variants[index-1], nil
	}
	return j, nil
}

// Variants share one atomic journal and lock. A crash cannot orphan siblings.
func splitJobID(id string) (string, int, error) {
	base, suffix, found := strings.Cut(id, ":")
	if !found {
		return id, 0, nil
	}
	i, err := strconv.Atoi(suffix)
	if err != nil || i < 1 {
		return "", 0, fault.New("INVALID_ARGUMENT", "Référence de variante invalide.")
	}
	return base, i, nil
}
func (e *Engine) jobPath(id string) (string, error) {
	base, _, err := splitJobID(id)
	if err != nil {
		return "", err
	}
	return local.JobPath(e.Root, base)
}
func aggregate(j *Job) {
	if len(j.Variants) == 0 {
		return
	}
	progress := 0.0
	done, ok := 0, 0
	unknown := false
	for _, v := range j.Variants {
		progress += v.Progress
		if studio.Terminal(v.State) || v.State == "downloaded" {
			done++
		}
		if v.State == "success" || v.State == "downloaded" {
			ok++
		}
		unknown = unknown || v.State == "outcome_unknown"
	}
	j.Progress = progress / float64(len(j.Variants))
	switch {
	case unknown:
		j.State = "outcome_unknown"
	case done < len(j.Variants):
		j.State = "running"
	case ok == len(j.Variants):
		j.State = "success"
	case ok == 0:
		j.State = "failed"
	default:
		j.State = "partial"
	}
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
	s := auth.Inspect(e.Root)
	return map[string]any{"authenticated": true, "expires_at": s.Expires, "renewable": s.Renewable, "needs_refresh": s.NeedsRefresh, "session_expires_at": s.SessionExpires, "studio": a}, nil
}

func (e *Engine) ImportSession(ctx context.Context, s auth.Session) (auth.Status, error) {
	verified, err := auth.Prepare(ctx, s, e.Renew)
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
	imageHash, err := imageHashes(params.FilePaths())
	if err != nil {
		return empty, err
	}
	s, c, err := e.client(ctx)
	if err != nil {
		return empty, err
	}
	hash, err := fingerprint(struct {
		Input     studio.GenerateInput
		ImageHash []string
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
	images, err := uploadImages(ctx, c, params.FilePaths())
	if err != nil {
		return e.failPreparing(&j, err)
	}
	if err = sameImages(params.FilePaths(), imageHash); err != nil {
		return e.failPreparing(&j, err)
	}
	// Persist uncertainty before dispatch; a crash at any later instruction cannot trigger a second charge.
	j.Retryable = false
	j.State = "outcome_unknown"
	j.Credits = &studio.Credits{Estimate: studio.EstimateGeneration(params)}
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	receipts, err := c.Generate(ctx, params, images)
	if len(receipts) == 1 && receipts[0].Accepted && err == nil {
		j.Receipt = receipts[0].Receipt
	} else if len(receipts) > 0 {
		for i, r := range receipts {
			state := "submitted"
			var failure *fault.Error
			if !r.Accepted {
				state = "failed"
				failure = fault.Public(fault.New("VARIATION_REJECTED", "Studio a refusé cette variante."))
			}
			if r.Accepted && (r.ProjectID == "" || r.OperatorID == "") {
				state = "outcome_unknown"
				failure = fault.Public(fault.New("OUTCOME_UNKNOWN", "Reçu de variante incomplet ; vérifier Studio."))
			}
			j.Variants = append(j.Variants, Job{ID: j.ID + ":" + strconv.Itoa(i+1), Kind: "generate", State: state, Account: j.Account, Receipt: r.Receipt, Format: "glb", CreatedAt: j.CreatedAt, Error: failure})
		}
	}
	if err != nil {
		j.Error = fault.Public(err)
		if !studio.IsUncertain(err) {
			j.State = "failed"
		}
	} else {
		j.State = "submitted"
	}
	if err == nil {
		aggregate(&j)
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
		if j.canResume() {
			j.State, j.Error, j.Retryable = "preparing", nil, true
			if err = e.save(&j); err != nil {
				unlock()
				return j, nil, false, err
			}
			return j, unlock, false, nil
		}
		return j, unlock, true, nil
	} else if !os.IsNotExist(err) {
		unlock()
		return Job{}, nil, false, err
	}
	j := Job{ID: id, Kind: kind, Format: format, State: "preparing", Retryable: true, Account: account, RequestHash: hash, CreatedAt: time.Now().UTC()}
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
	p, err := e.jobPath(id)
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
	if len(j.Variants) > 0 {
		var firstErr error
		for i := range j.Variants {
			if err = e.poll(ctx, &j.Variants[i]); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		// An incomplete submission stays uncertain even if its known children finish.
		if j.Error == nil || j.Error.Code != "OUTCOME_UNKNOWN" {
			aggregate(&j)
		}
		if err = e.save(&j); err != nil {
			return j.View(), err
		}
		return j.View(), firstErr
	}
	err = e.poll(ctx, &j)
	if err != nil {
		return j.View(), err
	}
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	return j.View(), nil
}

func (e *Engine) poll(ctx context.Context, j *Job) error {
	if j.State == "outcome_unknown" || j.Receipt.OperatorID == "" {
		return nil
	}
	terminal := j.State == "downloaded" || studio.Terminal(j.State)
	if terminal && j.Credits != nil && j.Credits.Actual != nil && j.Credits.Actual.Status == "recorded" {
		return nil
	}
	s, c, err := e.client(ctx)
	if err != nil {
		if terminal {
			if j.Credits == nil {
				j.Credits = &studio.Credits{}
			}
			j.Credits.Actual = &studio.Usage{Status: "unavailable", CheckedAt: time.Now().UTC(), Note: fault.Public(err).Message}
			return nil
		}
		return err
	}
	if s.Account != j.Account {
		return fault.New("ACCOUNT_CHANGED", "Cette tâche appartient à une autre session Studio.")
	}
	if terminal {
		e.refreshCredits(ctx, c, j)
		return nil
	}
	progress, err := c.Progress(ctx, j.Receipt.OperatorID)
	if err != nil {
		return err
	}
	j.Progress = progress.Progress
	j.State = progress.Status
	if j.State == "failed" {
		message := "Le traitement Studio a échoué ; aucune nouvelle soumission automatique."
		if progress.Reason != nil {
			message = fmt.Sprintf("Le traitement Studio a échoué (code %d) ; vérifier les paramètres avant toute nouvelle demande.", progress.Reason.Code)
		}
		j.Error = fault.Public(fault.New("STUDIO_TASK_FAILED", message))
	}
	if j.State == "" {
		return fault.New("PROTOCOL_CHANGED", "État de génération absent.")
	}
	if progress.ModelURL != "" {
		j.DownloadRef, err = local.Protect([]byte(progress.ModelURL))
		if err != nil {
			return err
		}
	}
	if studio.Terminal(j.State) {
		e.refreshCredits(ctx, c, j)
	}
	return nil
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
	p, err := e.jobPath(id)
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
	if len(j.Variants) > 0 {
		return j.View(), fault.New("INVALID_ARGUMENT", "Choisir une variante : job_id:1, job_id:2, etc.")
	}
	if j.State == "downloaded" && j.Artifact != nil {
		artifact, err := studio.ReuseArtifact(ctx, *j.Artifact, path)
		if err == nil {
			if j.Inspection == nil {
				inspectArtifact(&j, artifact.Path)
				if err = e.save(&j); err != nil {
					return j.View(), err
				}
			}
			if artifact.Path != j.Artifact.Path {
				j.Artifact = &artifact
				if err = e.save(&j); err != nil {
					return j.View(), err
				}
			}
			return j.View(), nil
		}
		if !os.IsNotExist(err) {
			return j.View(), err
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
	if j.Kind != "export" {
		project, err := c.Project(ctx, j.Receipt.ProjectID, j.Receipt.OperatorID)
		if err != nil {
			return j.View(), err
		}
		remote = studio.AssetURL(project)
		if remote != "" {
			j.Format, err = studio.ModelFormat(remote)
			if err != nil {
				return j.View(), err
			}
		}
	} else if j.Receipt.OperatorID != "" {
		// Signed URLs expire. Resolve a fresh link for the existing export on
		// every retrieval, without ever submitting another paid export.
		remote, err = c.ExportURL(ctx, j.Receipt.OperatorID)
		if err != nil {
			return j.View(), err
		}
	} else if len(j.DownloadRef) > 0 {
		b, err := local.Unprotect(j.DownloadRef)
		if err != nil {
			return j.View(), err
		}
		remote = string(b)
		clear(b)
	}
	if remote == "" {
		return j.View(), fault.New("ASSET_UNAVAILABLE", "Studio n'a pas fourni le fichier de ce modèle ; vérifier son état ou demander un export explicite.")
	}
	if path == "" {
		base, index, _ := splitJobID(id)
		folder := filepath.Join(e.Root, "downloads", base)
		if index > 0 {
			folder = filepath.Join(folder, "variant-"+strconv.Itoa(index))
		}
		path = filepath.Join(folder, "model."+j.Format)
	}
	artifact, err := studio.Download(ctx, remote, path, j.Format)
	if err != nil {
		return j.View(), err
	}
	j.Artifact = &artifact
	j.State = "downloaded"
	inspectArtifact(&j, artifact.Path)
	if j.Credits == nil || j.Credits.Actual == nil {
		e.refreshCredits(ctx, c, &j)
	}
	if err = e.save(&j); err != nil {
		return j.View(), err
	}
	return j.View(), nil
}

type ExportRequest struct {
	studio.ExportOptions
	JobID     string `json:"job_id"`
	RequestID string `json:"request_id,omitempty"`
	Confirm   bool   `json:"confirm"`
}

func (e *Engine) Export(ctx context.Context, in ExportRequest) (View, error) {
	if !in.Confirm {
		return View{}, fault.New("CONFIRM_REQUIRED", "L'export peut consommer des crédits Studio ; autorisation requise (--yes).")
	}
	opts, err := in.ExportOptions.Normalize()
	if err != nil {
		return View{}, err
	}
	source, err := e.source(in.JobID)
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
	input := studio.ExportInput{ExportOptions: opts, Project: source.Receipt.ProjectID}
	hash, err := fingerprint(struct {
		Input  studio.ExportInput
		Source studio.Receipt
	}{input, source.Receipt})
	if err != nil {
		return View{}, err
	}
	if in.RequestID == "" {
		in.RequestID = rand.Text()
	}
	j, unlock, existing, err := e.begin(in.RequestID, "export", opts.FileFormat(), s.Account, hash)
	if err != nil {
		return View{}, err
	}
	defer unlock()
	if existing {
		return j.View(), nil
	}
	projectUnlock, err := e.projectLock(s.Account, source.Receipt.ProjectID)
	if err != nil {
		return e.failPreparing(&j, err)
	}
	defer projectUnlock()
	if _, err = currentSource(ctx, c, source); err != nil {
		return e.failPreparing(&j, err)
	}
	j.State = "outcome_unknown"
	j.Retryable = false
	j.Warnings = source.Warnings
	j.ImportInfo = source.ImportInfo
	j.UVSourceVerified = source.UVSourceVerified
	j.Credits = &studio.Credits{Estimate: studio.EstimateExport(opts)}
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
