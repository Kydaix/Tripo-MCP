// Tripo-MCP is a native Studio subscription client with CLI and MCP front ends.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/Kydaix/Tripo-MCP/internal/auth"
	"github.com/Kydaix/Tripo-MCP/internal/engine"
	"github.com/Kydaix/Tripo-MCP/internal/fault"
	"github.com/Kydaix/Tripo-MCP/internal/local"
	"github.com/Kydaix/Tripo-MCP/internal/mcpserver"
	"github.com/Kydaix/Tripo-MCP/internal/studio"
)

var version = "dev"

const usage = `Tripo-MCP — crédits de l'abonnement Tripo Studio, sans clé API commerciale.

  tripo-mcp login [--no-open]    Transfert manuel de session dans une page locale, tout navigateur
  tripo-mcp session              État local de la connexion (aucun accès réseau)
  tripo-mcp status [job-id]      Compte Studio ou progression d'une tâche
  tripo-mcp generate --prompt "..." --yes [--request-id ID] [--faces 20000]
  tripo-mcp generate --image C:\image.png --yes [--request-id ID]
  tripo-mcp generate --prompt "..." --dry-run
  tripo-mcp capabilities         Modèles, réglages et limites pris en charge
  tripo-mcp edit JOB --operation texture --prompt "..." --yes
  tripo-mcp edit JOB --operation remesh --faces 5000 --quad --yes
  tripo-mcp import --model-file C:\model.glb --request-id imported-001 --yes
  tripo-mcp attach --project-id ID --request-id existing-001
  tripo-mcp jobs                 Tâches connues, sans accès réseau
  tripo-mcp wait JOB             Attendre une tâche ; Ctrl+C ne l'annule pas chez Tripo
  tripo-mcp download JOB [--out C:\model.glb]
  tripo-mcp export JOB --format fbx --yes [--request-id ID]
  tripo-mcp logout               Effacer la session locale protégée
  tripo-mcp mcp                  Serveur MCP sur stdin/stdout
  tripo-mcp version

Résultats JSON ; --json accepté pour les scripts. Diagnostics sur stderr.
Les identifiants --request-id sont stables : une répétition ne soumet pas une seconde génération.
Studio utilise des interfaces non publiques ; les conditions du service s'appliquent.
`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, in io.Reader, out, errout io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(out, "tripo-mcp", version)
		return 0
	}
	root, err := local.Root()
	if err != nil {
		return report(out, nil, err)
	}
	e := engine.New(root)
	if args[0] == "mcp" {
		if err := mcpserver.Run(ctx, e, version); err != nil {
			fmt.Fprintln(errout, fault.Public(err))
			return 1
		}
		return 0
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(errout)
	_ = f.Bool("json", false, "sortie JSON")
	yes := f.Bool("yes", false, "autoriser cette opération sur les crédits Studio")
	dry := f.Bool("dry-run", false, "valider les paramètres sans requête")
	noOpen := f.Bool("no-open", false, "afficher le lien local sans ouvrir le navigateur")
	var generation studio.GenerateInput
	var edit studio.EditInput
	var export studio.ExportOptions
	var imported studio.ImportInput
	projectID := f.String("project-id", "", "projet Studio existant (attach)")
	switch args[0] {
	case "generate":
		generationFlags(f, &generation)
	case "edit":
		editFlags(f, &edit)
	case "export":
		exportFlags(f, &export)
	case "import":
		f.StringVar(&imported.ModelFile, "model-file", "", "modèle local")
		f.StringVar(&imported.Name, "name", "", "nom du modèle")
		f.Var(optionalBool{&imported.UseOriginalUV}, "use-original-uv", "conserver les UV ; défaut true")
	}
	paramsFile := f.String("params", "", "fichier JSON de paramètres ; exclusif avec les options métier")
	requestID := f.String("request-id", "", "identifiant stable de demande")
	dest := f.String("out", "", "chemin du fichier")
	timeout := f.Duration("timeout", 20*time.Minute, "durée maximale d'attente")
	// Positional IDs may precede flags, like `download ID --out file`.
	rest := args[1:]
	id := ""
	if len(rest) > 0 && len(rest[0]) > 0 && rest[0][0] != '-' {
		id = rest[0]
		rest = rest[1:]
	}
	if err = f.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if id == "" && f.NArg() == 1 {
		id = f.Arg(0)
	} else if f.NArg() > 0 {
		return report(out, nil, fault.New("INVALID_ARGUMENT", "Arguments positionnels inattendus."))
	}
	var result any
	invalidFlag := ""
	f.Visit(func(v *flag.Flag) {
		allowed := true
		switch v.Name {
		case "project-id":
			allowed = args[0] == "attach"
		case "no-open":
			allowed = args[0] == "login"
		case "out":
			allowed = args[0] == "download"
		case "timeout":
			allowed = args[0] == "wait"
		case "yes":
			allowed = args[0] == "generate" || args[0] == "edit" || args[0] == "export" || args[0] == "import"
		case "request-id":
			allowed = args[0] == "generate" || args[0] == "edit" || args[0] == "export" || args[0] == "import" || args[0] == "attach"
		}
		if !allowed {
			invalidFlag = v.Name
		}
	})
	if invalidFlag != "" {
		return report(out, nil, fault.New("INVALID_ARGUMENT", "Option --"+invalidFlag+" incompatible avec cette commande."))
	}
	if id != "" && args[0] != "status" && args[0] != "doctor" && args[0] != "wait" && args[0] != "download" && args[0] != "export" && args[0] != "edit" {
		return report(out, nil, fault.New("INVALID_ARGUMENT", "Cette commande n'accepte pas d'identifiant positionnel."))
	}
	if *paramsFile != "" {
		conflict := false
		f.Visit(func(v *flag.Flag) {
			switch v.Name {
			case "params", "yes", "request-id", "dry-run", "json":
			default:
				conflict = true
			}
		})
		if conflict {
			return report(out, nil, fault.New("INVALID_ARGUMENT", "--params est exclusif avec les options métier."))
		}
		var target any
		switch args[0] {
		case "generate":
			target = &generation
		case "edit":
			target = &edit
		case "export":
			target = &export
		case "import":
			target = &imported
		default:
			return report(out, nil, fault.New("INVALID_ARGUMENT", "--params nécessite generate, edit ou export."))
		}
		if err = readParams(*paramsFile, target); err != nil {
			return report(out, nil, fault.New("INVALID_ARGUMENT", "Fichier de paramètres JSON invalide ou champ inconnu."))
		}
	}
	if *dry && args[0] != "generate" && args[0] != "edit" && args[0] != "export" && args[0] != "import" {
		return report(out, nil, fault.New("INVALID_ARGUMENT", "--dry-run nécessite generate, edit, export ou import."))
	}
	switch args[0] {
	case "login":
		result, err = e.Login(ctx, func(url string) {
			fmt.Fprintln(errout, "Transfert de session dans votre navigateur habituel (lien local valable 15 minutes) :\n"+url)
			if !*noOpen {
				if err := auth.OpenPage(url); err != nil {
					fmt.Fprintln(errout, "Ouvrez ce lien dans le navigateur de votre choix.")
				}
			}
		})
	case "session":
		result = auth.Inspect(root)
	case "logout":
		var unlock func()
		unlock, err = local.Lock(root + "/auth.lock")
		if err == nil {
			err = auth.Logout(root)
			unlock()
		}
		result = map[string]bool{"authenticated": false}
	case "status", "doctor":
		if id == "" {
			result, err = e.Status(ctx)
		} else {
			result, err = e.Poll(ctx, id)
		}
	case "jobs":
		result, err = e.List()
	case "capabilities":
		result = studio.Capabilities()
	case "attach":
		result, err = e.Attach(ctx, engine.AttachRequest{ProjectID: *projectID, RequestID: *requestID})
	case "import":
		if *dry {
			result, err = imported.Normalize()
		} else {
			result, err = e.Import(ctx, engine.ImportRequest{ImportInput: imported, RequestID: *requestID, Confirm: *yes})
		}
	case "generate":
		params := engine.GenerateRequest{GenerateInput: generation, RequestID: *requestID, Confirm: *yes}
		if *dry {
			result, err = params.GenerateInput.Normalize()
			if err == nil {
				err = validateImages(generation.FilePaths())
			}
		} else {
			result, err = e.Generate(ctx, params)
		}
	case "wait":
		if *timeout <= 0 {
			err = fault.New("INVALID_ARGUMENT", "Durée d'attente invalide.")
			break
		}
		waitCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		result, err = e.Wait(waitCtx, id)
	case "download":
		result, err = e.Download(ctx, id, *dest)
	case "export":
		if *dry {
			result, err = export.Normalize()
		} else {
			result, err = e.Export(ctx, engine.ExportRequest{ExportOptions: export, JobID: id, RequestID: *requestID, Confirm: *yes})
		}
	case "edit":
		if *dry {
			result, err = edit.Normalize()
			if err == nil {
				err = validateImages(edit.FilePaths())
			}
		} else {
			result, err = e.Edit(ctx, engine.EditRequest{EditInput: edit, JobID: id, RequestID: *requestID, Confirm: *yes})
		}
	case "session-import":
		// Migration channel: stdin only, never arguments, files in a repository, or MCP tool input.
		var s auth.Session
		decoder := json.NewDecoder(io.LimitReader(in, 32768))
		if decoder.Decode(&s) != nil {
			err = fault.New("AUTH_REQUIRED", "Session d'entrée illisible.")
		} else {
			result, err = e.ImportSession(ctx, s)
		}
	default:
		err = fault.New("INVALID_ARGUMENT", "Commande inconnue ; tripo-mcp help.")
	}
	return report(out, result, err)
}

func validateImages(paths []string) error {
	for _, p := range paths {
		if p != "" {
			if _, err := studio.ValidateImage(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func report(out io.Writer, result any, err error) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err == nil {
		if encoder.Encode(result) != nil {
			return 1
		}
		return 0
	}
	f := fault.Public(err)
	_ = encoder.Encode(map[string]any{"error": f, "result": result})
	switch f.Code {
	case "INVALID_ARGUMENT", "CONFIRM_REQUIRED":
		return 2
	case "AUTH_REQUIRED", "AUTH_EXPIRED":
		return 3
	case "INSUFFICIENT_CREDITS":
		return 4
	case "OUTCOME_UNKNOWN":
		return 5
	default:
		return 1
	}
}
