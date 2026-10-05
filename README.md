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
4. Pour activer le renouvellement automatique, copier la valeur du cookie `ory_kratos_session` dans le champ « Cookie de connexion ». Il se trouve dans **Application → Cookies** sur Chrome/Edge, ou **Stockage → Cookies** sur Firefox, sous le domaine Tripo.

La copie n’est jamais exécutée. La page extrait uniquement les en-têtes Tripo nécessaires et le cookie de connexion indiqué ; les autres cookies sont ignorés. Le client vérifie le renouvellement et le compte auprès de Studio avant l’enregistrement. Aucun mot de passe n’est demandé. Le récepteur écoute seulement sur `127.0.0.1`, avec une autorisation temporaire par transfert, et s’arrête après réussite ou après 15 minutes. Ne jamais transmettre la requête ou le cookie dans une conversation, un argument de commande ou un dépôt.

**Avec le cookie, les jetons se renouvellent automatiquement, même navigateur fermé.** Le CLI et le MCP utilisent le mécanisme de Studio avant les requêtes, avec une marge d’une minute et un verrou partagé entre processus. Aucun service permanent ni extension n’est nécessaire. Le cookie est envoyé uniquement au point de connexion Studio ; les opérations et les téléchargements ne le reçoivent pas.

`tripo-mcp session` inspecte les données locales sans accès réseau. `renewable: true` indique que la session principale peut renouveler les jetons ; `expires_at` concerne le jeton d’accès et `session_expires_at`, lorsqu’il est fourni par Studio, concerne la connexion principale. `needs_refresh` indique qu’un nouveau jeton sera demandé lors du prochain accès. `tripo-mcp status` vérifie le compte en ligne et renouvelle au besoin.

Un nouveau transfert reste nécessaire lorsque la connexion principale expire ou est révoquée. Sa durée est décidée par Studio et peut être écourtée côté serveur. Une erreur réseau ne supprime pas la connexion enregistrée ; une révocation confirmée lors du renouvellement la supprime. Une vérification humaine reste à terminer dans le navigateur. **Les anciens transferts sans cookie restent temporaires** : relancer `login` une fois avec le cookie pour les remplacer. Une requête payante n’est jamais rejouée automatiquement pour renouveler la connexion.

Les données résident dans `%LOCALAPPDATA%\Tripo-MCP` : session chiffrée par Windows DPAPI, journal et modèles téléchargés. `logout` efface la session en conservant les modèles et leur journal. La variable facultative `TRIPO_MCP_HOME` choisit un autre chemin absolu de données.

Le [fonctionnement de la connexion](docs/authentication.md) décrit le renouvellement, les verrous et les limites du mode manuel.

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

Par défaut : H3.1, 20 000 triangles, textures standard 2K, visibilité privée. `--dry-run` valide les paramètres et les fichiers localement ; ce n’est pas un devis de crédits. `tripo-mcp capabilities` donne le catalogue et `generate --help` liste les options.

Les contrôles du générateur Studio sont exposés : modèles H2.5/H3.0/H3.1 et Smart Mesh P1.0/P2.0, Ultra Mesh Quality, AI Complete, triangles/quadrangles, budget de polygones, génération en parties avec niveau de détail, textures 2K/4K/8K, PBR, retrait d’éclairage, alignement, pose en T et visibilité. Les entrées peuvent être un texte, une image, quatre emplacements multivues ou un lot d’images indépendantes.

```powershell
tripo-mcp generate --prompt "Un robot stylisé" --model h3.1 --geometry-quality detailed --faces 2000000 --texture-quality extreme --request-id robot-hd --yes
tripo-mcp generate --image C:\Images\reference.png --model p2.0 --quad --variation-faces 5000,10000 --request-id robot-p2 --yes
tripo-mcp wait robot-p2
tripo-mcp download robot-p2:1
```

P2.0 produit de la géométrie ; utiliser ensuite `edit --operation texture`. Studio peut livrer un FBX pour les quadrangles : le téléchargement détecte le format natif, sans le renommer GLB. Pour obtenir un autre format, demander un export explicite. Le nombre de variantes est explicite : `--count 1|2|4`, ou `--variation-faces` pour des budgets individuels. Chaque variante conserve son reçu dans un journal commun ; une réussite partielle ne perd pas les modèles acceptés. Utiliser les identifiants `job:1`, `job:2`, etc. pour les traiter séparément. La visibilité privée reste le défaut.

Tous les paramètres sont aussi utilisables dans un fichier JSON via `--params C:\options.json`, avec les mêmes noms de champs que le MCP. Le fichier est exclusif avec les options métier et n’inclut ni `confirm`, ni `request_id`, ni `job_id` ; ces derniers restent des arguments de commande.

## Texturer et retravailler

```powershell
tripo-mcp edit robot-p2:1 --operation texture --prompt "Métal peint rouge, articulations en acier" --texture-quality extreme --delight --request-id robot-texture --yes
tripo-mcp wait robot-texture
tripo-mcp download robot-texture
tripo-mcp export robot-texture --format fbx --texture-size 8192 --request-id robot-fbx --yes
tripo-mcp wait robot-fbx
tripo-mcp download robot-fbx
tripo-mcp edit robot-texture --operation remesh --faces 5000 --quad --request-id robot-remesh --yes
```

Les opérations `texture`, `upscale`, `pbr`, `remesh`, `segment`, `fill`, `complete`, `rig` et `animate` passent par `edit`. La texture accepte un prompt, une image, des multivues et une image de style ; le remesh peut reprojeter les textures. Les noms de parties permettent de cibler la texture ou la retopologie ; par défaut, ils sont lus automatiquement dans le GLB/FBX source. Le remplissage rapide (`fill`) ou par IA (`complete`) cible les parties indiquées.

**Chaque édition modifie la version courante du projet Studio.** Exporter/télécharger une version avant de l’éditer, puis utiliser l’identifiant de la nouvelle tâche. Si Studio possède une autre version courante, l’édition ou l’export est refusé avec `SOURCE_CHANGED` ; aucune restauration n’est faite automatiquement.

Pour un modèle local : `tripo-mcp import --model-file C:\Models\objet.glb --request-id objet-import --yes`, puis attendre et utiliser cette tâche avec `edit`. Formats d’entrée : GLB, FBX, OBJ et STL ; limite locale de 100 Mo. Un OBJ doit contenir sa géométrie ; les fichiers MTL et textures annexes ne sont pas téléversés. Pour un projet déjà présent dans Studio : `tripo-mcp attach --project-id ID --request-id objet-existant` (lecture distante seule).

Les exports proposent GLB, FBX, OBJ, STL, 3MF et USDZ, textures de 512 à 8192 pixels, packaging intégré ou ZIP, regroupement UV, presets FBX et options d’animation. Une archive ZIP est téléchargée telle quelle, sans extraction automatique. Son chemin de sortie doit finir en `.zip`.

La [correspondance détaillée des paramètres](docs/studio-controls.md) précise les limites et les fonctions qui restent dans l’éditeur visuel.

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
| `tripo_capabilities` | Modèles, réglages, bornes et limites locales |
| `tripo_generate` | Génération HD/Smart Mesh, texte/image/multivues/lots et variantes |
| `tripo_edit` | Texture, upscale, PBR, remesh, segmentation, fermeture/complétion de parties, rigging et animation |
| `tripo_import_model` | Téléversement d’un modèle local dans Studio |
| `tripo_attach` | Rattachement d’un projet Studio existant |
| `tripo_job` | Progression d’une tâche |
| `tripo_jobs` | Journal local |
| `tripo_download` | Chemin du fichier, taille et SHA-256 |
| `tripo_export` | Export explicite avec format, textures, UV et animations |

Les opérations payantes et l’import exigent un `request_id` stable et `confirm: true` lorsque l’utilisateur a autorisé l’opération. Une demande explicite autorise l’opération dans le périmètre donné. `tripo_attach` exige un identifiant stable mais ne consomme pas de crédits.

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

Tests sans crédits : contrats HTTP HD/P2/multivues/lots/texture/import/rig/export, variantes partiellement acceptées, reprises, doubles soumissions, sources remplacées, verrouillage concurrent, protection de session, validation des fichiers, CLI et échanges MCP. Les validations réelles couvrent texte HD → GLB et export FBX, puis texte P2.0 → FBX avec quadrangles → texture 8K → export GLB 8K. Les images intégrées aux fichiers ont été vérifiées dans Blender à 8192 × 8192 pixels. Les autres combinaisons restent vérifiées par contrats ; cela ne prouve pas leur acceptation par le serveur ni les droits d’un abonnement.

Architecture : `internal/auth` (connexion), `internal/studio` (transport), `internal/engine` (tâches), `internal/mcpserver` (adaptateur). CLI et MCP appellent les mêmes opérations. Les tags `v*` publient les deux exécutables Windows et leurs empreintes après les tests.

Licence MIT pour ce code ; les dépendances et les modèles générés conservent leurs propres conditions.
