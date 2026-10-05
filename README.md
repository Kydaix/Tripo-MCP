# Tripo-MCP

Un exécutable Windows pour générer des modèles 3D avec **les crédits de l’abonnement Tripo Studio**, depuis un terminal ou un agent MCP.

Le CLI et le MCP partagent le même moteur Go : requêtes HTTP directes, tâches persistantes, téléchargements GLB/FBX. Pas de Node.js, Python, extension Playwright ou bridge DCC nécessaire à la génération.

Projet indépendant, non affilié à Tripo. Il utilise les interfaces HTTP internes de Studio, **pas l’API commerciale facturée séparément**. Ces interfaces ne sont pas publiques et peuvent changer. Les conditions de Tripo et les droits du compte s’appliquent.

## Installer et se connecter

Télécharger l’exécutable Windows amd64 ou arm64 depuis les [releases](https://github.com/Kydaix/Tripo-MCP/releases/latest), vérifier son empreinte avec `SHA256SUMS.txt`, puis le renommer `tripo-mcp.exe` dans un dossier du PATH.

Avec [ai-setup](https://github.com/Kydaix/ai-setup) : `ai-setup install tripo`, puis `ai-setup login tripo`.

```powershell
tripo-mcp login
tripo-mcp status
```

`login` ouvre une page locale dans le navigateur par défaut, sans le piloter. Le lien affiché peut être ouvert dans un autre navigateur, et `login --no-open` affiche seulement ce lien. Chrome, Firefox, Edge et leurs dérivés utilisent le même transfert manuel ; aucune extension n’est nécessaire.

1. Se connecter normalement à Studio dans son navigateur habituel.
2. Ouvrir les outils de développement, onglet Réseau, filtrer `team/list`, puis actualiser Studio.
3. Copier la requête `api.tripo3d.ai/v2/studio/team/list` comme fetch ou cURL et la coller dans la page locale. On peut aussi renseigner les en-têtes `Authorization` et `x-tripo-device-id` à la main.

La copie n’est jamais exécutée. La page extrait uniquement les en-têtes de session nécessaires ; le client vérifie le compte auprès de Studio avant de les enregistrer. Aucun mot de passe n’est demandé. Le récepteur écoute seulement sur `127.0.0.1`, avec une autorisation temporaire par transfert, et s’arrête après réussite ou après 15 minutes.

**À l’expiration du jeton Studio, refaire le transfert avec `login`.** Il n’y a pas de renouvellement automatique dans ce mode manuel. L’expiration est affichée après connexion et dans `status` ; les durées sont décidées par Studio.

Les données résident dans `%LOCALAPPDATA%\Tripo-MCP` : session chiffrée par Windows DPAPI, journal et modèles téléchargés. `logout` efface la session en conservant les modèles et leur journal. La variable facultative `TRIPO_MCP_HOME` choisit un autre chemin absolu de données.

## Générer un modèle

```powershell
tripo-mcp generate --prompt "Une petite statuette de hibou en bronze" --request-id hibou-001 --yes
tripo-mcp wait hibou-001
tripo-mcp download hibou-001 --out C:\Models\hibou.glb

tripo-mcp export hibou-001 --format fbx --request-id hibou-fbx-001 --yes
tripo-mcp wait hibou-fbx-001
tripo-mcp download hibou-fbx-001 --out C:\Models\hibou.fbx
```

Une image locale PNG, JPEG ou WebP de 20 Mo maximum peut remplacer le prompt avec `--image C:\Images\reference.png`.

Par défaut : modèle `v3.1-20260211`, 20 000 faces, textures standard, visibilité privée. Options : `--faces`, `--model`, `--no-texture`, `--visibility private|shareable|public`. `--dry-run` valide les paramètres localement ; ce n’est pas un devis de crédits.

Les résultats sont en JSON et les diagnostics sur stderr. `generate` rend la main dès l’acceptation. `status JOB`, `wait JOB` et `jobs` permettent de reprendre plus tard. Fermer le terminal n’annule pas une génération distante.

Le téléchargement valide le format et la taille, calcule le SHA-256, puis finalise le fichier sans écraser une destination existante. Aucun export payant n’est lancé implicitement. L’import dans Blender, Roblox Studio ou un autre logiciel reste optionnel et passe par ses outils habituels.

## MCP

Serveur stdio : `tripo-mcp mcp`. Exemple de configuration :

```json
{
  "mcpServers": {
    "tripo": {
      "command": "C:\\Tools\\tripo-mcp.exe",
      "args": ["mcp"]
    }
  }
}
```

| Outil | Résultat |
|---|---|
| `tripo_status` | Connexion, abonnement et crédits Studio |
| `tripo_generate` | Soumission texte/image et identifiant de tâche |
| `tripo_job` | Progression d’une tâche |
| `tripo_jobs` | Journal local |
| `tripo_download` | Chemin du fichier, taille et SHA-256 |
| `tripo_export` | Export explicite GLB ou FBX |

`tripo_generate` et `tripo_export` exigent un `request_id` stable et `confirm: true` lorsque l’utilisateur a autorisé l’opération. Une demande explicite de génération autorise cette génération dans le périmètre donné.

## Crédits et interruptions

Le moteur verrouille le journal et enregistre l’intention avant chaque requête payante. Réutiliser un identifiant avec les mêmes paramètres renvoie la tâche existante. Des paramètres différents provoquent `REQUEST_CONFLICT`.

Une réponse incertaine produit `outcome_unknown` : vérifier l’historique Studio avant toute nouvelle soumission. Le journal empêche les resoumissions automatiques sur ce poste ; il ne garantit pas une exécution exactement une fois entre plusieurs postes.

Les exports peuvent consommer des crédits. Aucun coût fixe n’est garanti. `--yes` autorise l’opération, ce n’est pas un plafond de facturation.

Codes de sortie : `0` commande réussie, `1` erreur, `2` paramètres/autorisation manquants, `3` connexion requise, `4` crédits insuffisants, `5` résultat incertain. Lire aussi l’état retourné : une consultation réussie peut décrire une génération échouée.

## Développement

Go 1.27 sur Windows :

```powershell
go test ./...
go vet ./...
go build -o dist/tripo-mcp.exe .
```

Tests sans crédits : contrats HTTP, doubles soumissions, réponses incertaines, verrouillage concurrent, protection de session, validation des fichiers et échange MCP en mémoire. La validation réelle initiale couvre texte → modèle → GLB et export FBX. Le parcours image nécessite encore une validation réelle de l’upload.

Architecture : `internal/auth` (connexion), `internal/studio` (transport), `internal/engine` (tâches), `internal/mcpserver` (adaptateur). CLI et MCP appellent les mêmes opérations. Les tags `v*` publient les deux exécutables Windows et leurs empreintes après les tests.

Licence MIT pour ce code ; les dépendances et les modèles générés conservent leurs propres conditions.
