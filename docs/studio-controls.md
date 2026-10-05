# Contrôles Studio

Contrats observés le 5 octobre 2026 dans les fichiers publics de [Tripo Studio](https://studio.tripo3d.ai/), à partir du point d’entrée `CiZEDlkA.js` : `BOQlA7aJ.js` (versions), `B00y9j9b.js` (génération), `d0-kdlow.js` (panneaux), `BRpGFHL4.js` (texture), `BANs6Mft.js` (import/rig/animation/export) et `DJfzZG46.js` (formats). Les endpoints `/v2/studio/operation/*` appartiennent au service Studio ; aucune requête n’utilise `/v2/openapi/task`.

## Générateur

| Contrôle Studio | Champ MCP / JSON | CLI |
|---|---|---|
| AI Model H2.5 / H3.0 / H3.1 / P1.0 / P2.0 | `model` | `--model h3.1` ou `p2.0` |
| Ultra Mesh Quality | `geometry_quality: "detailed"` | `--geometry-quality detailed` |
| AI Complete | `image_autofix` | `--image-autofix` |
| Texture | `no_texture` inverse l’activation | `--no-texture` |
| Texture 2K / 4K / 8K | `texture_quality: standard/detailed/extreme` | `--texture-quality extreme` |
| PBR | `no_pbr` inverse l’activation | `--no-pbr` |
| Remove Lighting | `delight` | `--delight` |
| Alignement | `texture_alignment` | `--texture-alignment geometry` |
| Topology Quad / Triangle | `quad: true/false` | `--quad` / `--quad=false` |
| Polycount | `faces` | `--faces 20000` |
| Smart Poly (HD) | `smart_poly` | `--smart-poly` |
| Generate in Parts | `generate_parts` | `--generate-parts --no-texture` |
| Niveau des parties | `parts_level: simple/balanced/detailed` | `--parts-level detailed` |
| Privacy | `visibility: private/shareable/public` | `--visibility private` |
| Number of Generation (P2) | `count: 1/2/4` | `--count 4` |
| Polycount par variante (P2) | `variation_faces: [5000,10000]` | `--variation-faces 5000,10000` |
| Symétrie (P2) | `symmetry`, détection automatique si absent | `--symmetry=false` |
| Pose en T | `t_pose` | `--t-pose` |

Les modèles sont résolus vers leurs identifiants Studio exacts par `capabilities`. Les valeurs par défaut du CLI sont volontairement stables ; les valeurs mémorisées dans le navigateur ne sont pas importées.

Quatre modes d’entrée exclusifs :

- `prompt` : texte, avec `negative_prompt` facultatif.
- `image` : chemin absolu PNG/JPEG/WebP, 20 Mo maximum.
- `images` : quatre chemins avant/gauche/arrière/droite ; chaîne vide pour un emplacement absent, avant et au moins une autre vue requis. CLI : quatre occurrences de `--view`, ou `--params` pour les emplacements vides.
- `batch_images` : chemins d’images indépendantes. CLI : occurrences de `--batch-image`. Limite locale de 100 images, soumise aussi aux limites du compte.

Le téléversement est suivi de l’audit d’image de Studio. Son verdict `pass` est conservé dans la demande de génération ou de texture. `reject` produit `IMAGE_REJECTED` avec le verdict exact et indique qu’aucun motif détaillé n’a été fourni ; les images signalées `sensitive` ou `nsfw` demandent une vérification dans Studio (`IMAGE_REVIEW_REQUIRED`). Un verdict absent ou inconnu produit `PROTOCOL_CHANGED`, sans soumission payante. Une tâche `retryable: true` peut être reprise par une nouvelle demande explicite avec le même identifiant et les mêmes entrées. Un résultat de soumission incertain n’est jamais rejoué.

Le budget minimal est de 500 polygones, ou 10 000 en génération de parties. Maximum HD : 1 million de triangles en standard, 2 millions en H3.1 Ultra, 50 000 quadrangles. Smart Poly HD : 20 000 triangles / 10 000 quadrangles. P2 : 50 000 triangles / 25 000 quadrangles. P1 : 20 000.

P2 utilise les quadrangles et 5 000 polygones par défaut. Ses variantes partagent une seule soumission Studio, avec des reçus séparés. P2 ne reçoit pas les options de texture du générateur HD : appliquer une texture ensuite. La génération en parties est incompatible avec les quadrangles et les textures actives. Ultra Mesh Quality est réservé à H3.1. AI Complete s’applique aux images uniques et lots HD.

## Traitements d’un modèle

`tripo_edit` prend un `job_id`, un `request_id`, `confirm` et une `operation`. Le CLI correspondant est `edit JOB --operation ...`.

| Opération | Réglages |
|---|---|
| `texture` | `prompt` ou `image` ou `images`, `style_image`, `texture_quality`, `texture_alignment`, `delight`, `parts` |
| `upscale` | `texture_quality: detailed/extreme` ; modèle déjà texturé |
| `pbr` | génération des matériaux PBR |
| `remesh` | `faces`, `quad`, `smart_poly`, `bake` (true par défaut), `parts` |
| `segment` | `parts_level: simple/balanced/detailed` |
| `fill` / `complete` | noms des `parts` à refermer rapidement ou compléter par IA |
| `rig` | `rig_type: auto/biped`, `skeleton: mixamo/actorcore/unreal/unity/vrm` pour biped |
| `animate` | `rig_type`, `animations` ou `motion_asset_id` existant |

Les animations doivent utiliser les identifiants exacts de Studio pour le rig concerné. La création de mouvements à partir d’un prompt n’est pas incluse. Le rigging effectue le contrôle préalable de Studio et refuse une réponse négative. Les options sans effet pour l’opération choisie sont refusées. Texture et remesh détectent les noms de tous les maillages à partir du GLB ou FBX binaire source quand `parts` est omis ; si les noms ne peuvent pas être résolus, fournir `parts` explicitement.

Chaque édition agit sur la version courante du projet ; une source plus ancienne provoque `SOURCE_CHANGED`. Pour continuer depuis un projet modifié dans le navigateur, rattacher sa version actuelle avec `attach` et un nouvel identifiant local. Cela ne restaure ni ne modifie le projet distant.

## Import et export

`tripo_import_model` accepte `model_file`, `name`, `use_original_uv` et `transform` (matrice 4×4 column-major, identité par défaut). Formats : GLB, FBX, OBJ, STL. Limite locale 100 Mo. Les dépendances externes d’un OBJ/FBX ne sont pas téléversées.

Le contrat officiel envoie bien `use_original_uv`, y compris `false`. Le serveur ne garantit cependant pas leur reconstruction. Les GLB dont les UV sont absents ou tous sans surface sont refusés localement avec `UV_UNUSABLE`, avant import. Le contrôle est renouvelé sur le résultat avant texture. Si la compression empêche l’inspection et qu’aucun atlas source contrôlé n’est conservé, `UV_UNVERIFIED` demande une vérification dans un DCC ; `allow_unverified_uv` permet ensuite de déclarer cette vérification pour `texture`. Ce champ local n’est jamais envoyé à Studio. La normalisation possible de l’échelle est annoncée dans les avertissements d’import ; le client ne restaure pas les dimensions.

`tripo_estimate` et `--dry-run` utilisent les valeurs et formules de `BOQlA7aJ.js`, `D2TK3lHS.js` et `d0-kdlow.js`. Les montants sont des estimations datées, hors remises/quotas. `tripo_cost` / `cost JOB` lit `/v2/studio/txn/records?page_num=N&page_size=100`, comme Studio, et sélectionne uniquement les lignes de l’opération. Les statuts, débits et remboursements sont conservés sans attribuer les variations globales du portefeuille à une tâche.

`tripo_export` accepte `format` (GLB, FBX, OBJ, STL, 3MF, USDZ), `texture_size` (512/1024/2048/4096/8192), `packaging` (embedded/zip), `pack_uv`, `vertex_colors` (OBJ), `fbx_preset`, `with_animation`, `animations`, `animate_in_place`, `bake_animation` et `bake_frame`. L’orientation Studio `-y` et le nom technique `model` sont fixes ; le nom du fichier local se choisit avec `download --out`.

OBJ est toujours livré en ZIP pour garder ses fichiers associés. ZIP/3MF/USDZ sont contrôlés comme archives contenant un modèle reconnu, sans extraction. Le serveur reste responsable de la compatibilité du format avec les matériaux, rig et animations du modèle.

## Limites et validation

Les réglages des deux captures du générateur sont couverts. Cela ne reproduit pas toute l’interface Studio : peinture Magic Brush, manipulation de points/masques dans l’éditeur 3D, édition UV interactive, création d’images de référence et génération de mouvements restent hors de cette interface.

La disponibilité des fonctions, les essais gratuits, les droits d’équipe et les coûts sont décidés par Studio. Aucun essai, quota ou plafond tarifaire n’est garanti par le catalogue local. Une erreur d’accès n’entraîne aucun basculement vers l’API commerciale.

Les contrats et les reprises sont testés sans crédits. Les validations réelles couvrent texte HD → GLB et export FBX, puis texte P2.0 → FBX avec quadrangles → texture 8K → export GLB 8K. Les textures intégrées au FBX et au GLB ont été inspectées dans Blender : 8192 × 8192 pixels. Le FBX conserve sa topologie ; le GLB triangule les faces. Une première demande de texture sans noms de parties a été refusée par Studio sans débit ; leur résolution automatique corrige ce cas. Les autres combinaisons ne sont pas toutes testées sur le compte réel. Les interfaces internes peuvent évoluer.
