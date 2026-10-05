// Package mcpserver is a thin stdio adapter around the shared application engine.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/Kydaix/Tripo-MCP/internal/engine"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Empty struct{}
type JobInput struct {
	JobID string `json:"job_id" jsonschema:"Identifiant local retourné par generate ou export."`
}
type DownloadInput struct {
	JobID string `json:"job_id"`
	Out   string `json:"out,omitempty" jsonschema:"Chemin absolu de sortie ; défaut dans les données locales Tripo-MCP."`
}

func result(value any, err error) (*mcp.CallToolResult, any, error) {
	if err == nil {
		return nil, value, nil
	}
	b, _ := json.Marshal(map[string]any{"error": fault.Public(err), "result": value})
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

func New(e *engine.Engine, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "tripo-mcp", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_import_model", Description: "Téléverse un modèle local GLB/FBX/OBJ/STL et crée un projet Studio pour texturing ou édition. confirm=true et request_id stable requis."}, func(ctx context.Context, _ *mcp.CallToolRequest, in engine.ImportRequest) (*mcp.CallToolResult, any, error) {
		if in.RequestID == "" {
			return result(nil, fault.New("INVALID_ARGUMENT", "Fournir un request_id stable."))
		}
		v, err := e.Import(ctx, in)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_attach", Description: "Rattache la version courante terminée d'un projet Studio existant au journal local. Lecture distante seule, aucun crédit."}, func(ctx context.Context, _ *mcp.CallToolRequest, in engine.AttachRequest) (*mcp.CallToolResult, any, error) {
		if in.RequestID == "" {
			return result(nil, fault.New("INVALID_ARGUMENT", "Fournir un request_id stable."))
		}
		v, err := e.Attach(ctx, in)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_capabilities", Description: "Catalogue local des modèles Studio, paramètres, bornes et limites. Aucun accès réseau ni crédit."}, func(_ context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
		return result(studio.Capabilities(), nil)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_edit", Description: "Texture/retexture (texte, image, multivues, style, 2K/4K/8K), upscale, PBR, retopologie, segmentation, fermeture/complétion de parties, rigging ou animation. Modifie le projet source courant ; confirmer après autorisation de dépense. Utiliser un request_id stable. Pour un lot, choisir job_id:1 etc."}, func(ctx context.Context, _ *mcp.CallToolRequest, in engine.EditRequest) (*mcp.CallToolResult, any, error) {
		if in.RequestID == "" {
			return result(nil, fault.New("INVALID_ARGUMENT", "Fournir un request_id stable."))
		}
		v, err := e.Edit(ctx, in)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_status", Description: "Vérifie la session et les crédits Studio. Lecture seule. Si connexion requise, demander à l'utilisateur de lancer tripo-mcp login."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
		v, err := e.Status(ctx)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_generate", Description: "Génère un modèle via les crédits Studio. Nécessite l'autorisation de l'utilisateur (confirm=true). Retourne immédiatement une tâche ; utiliser un request_id stable pour éviter les doublons. Aucun retry payant automatique."}, func(ctx context.Context, _ *mcp.CallToolRequest, in engine.GenerateRequest) (*mcp.CallToolResult, any, error) {
		if in.RequestID == "" {
			return result(nil, fault.New("INVALID_ARGUMENT", "Fournir un request_id stable et unique pour cette demande."))
		}
		v, err := e.Generate(ctx, in)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_job", Description: "Lit la progression d'une tâche ; ne soumet jamais une nouvelle génération. Interroger à intervalles espacés."}, func(ctx context.Context, _ *mcp.CallToolRequest, in JobInput) (*mcp.CallToolResult, any, error) {
		v, err := e.Poll(ctx, in.JobID)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_jobs", Description: "Liste les tâches enregistrées localement, notamment après une interruption."}, func(_ context.Context, _ *mcp.CallToolRequest, _ Empty) (*mcp.CallToolResult, any, error) {
		v, err := e.List()
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_download", Description: "Télécharge le fichier d'une tâche réussie et retourne chemin, taille et SHA-256. Aucun export payant implicite."}, func(ctx context.Context, _ *mcp.CallToolRequest, in DownloadInput) (*mcp.CallToolResult, any, error) {
		v, err := e.Download(ctx, in.JobID, in.Out)
		return result(v, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_export", Description: "Export GLB/FBX/OBJ/STL/3MF/USDZ, textures jusqu'à 8K, UV et animations. Source : version courante d'un modèle terminé. Peut consommer des crédits Studio : confirm=true seulement après autorisation. Utiliser un request_id stable."}, func(ctx context.Context, _ *mcp.CallToolRequest, in engine.ExportRequest) (*mcp.CallToolResult, any, error) {
		if in.RequestID == "" {
			return result(nil, fault.New("INVALID_ARGUMENT", "Fournir un request_id stable."))
		}
		v, err := e.Export(ctx, in)
		return result(v, err)
	})
	return s
}

func Run(ctx context.Context, e *engine.Engine, version string) error {
	return New(e, version).Run(ctx, &mcp.StdioTransport{})
}
