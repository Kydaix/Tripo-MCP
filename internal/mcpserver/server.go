// Package mcpserver is a thin stdio adapter around the shared application engine.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/Kydaix/Tripo-MCP/internal/engine"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
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
	mcp.AddTool(s, &mcp.Tool{Name: "tripo_export", Description: "Demande un export GLB/FBX d'un modèle terminé. Peut consommer des crédits Studio : confirm=true seulement après autorisation. Utiliser un request_id stable."}, func(ctx context.Context, _ *mcp.CallToolRequest, in engine.ExportRequest) (*mcp.CallToolResult, any, error) {
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
