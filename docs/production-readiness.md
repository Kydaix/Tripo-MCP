# Fiabilisation du client

Audit du 5 octobre 2026, à partir de la version 0.3.0. Périmètre : connexion locale, renouvellement, journal, transferts de fichiers, validation des entrées, CLI et MCP. Aucun test de génération payante n'a été lancé pour cet audit.

## Corrections

- Les exports conservaient parfois un lien signé périmé. Le téléchargement résout maintenant un lien récent à partir de l'opération existante ; il ne lance aucun nouvel export.
- Le cache des modèles était vérifié uniquement par sa taille. Le client compare désormais son SHA-256 et signale un fichier modifié. Un autre chemin de sortie crée une copie locale vérifiée, sans écraser un fichier existant.
- Le rattachement d'un projet traitait `running_operator: false` ou une chaîne vide comme une opération en cours. Il applique désormais la même règle que l'édition.
- Les fichiers image sont contrôlés par leur signature PNG/JPEG/WebP en plus de l'extension et de la taille. Cette vérification ne remplace pas le décodage et l'audit réalisés par Studio.
- Les en-têtes contenant des caractères de contrôle, les noms de périphériques Windows utilisés comme identifiants de tâche, les prompts de texture vides et les noms d'animation invalides sont refusés avant envoi.
- Les fichiers de paramètres n'acceptent plus `null`, plusieurs objets JSON ou une entrée dépassant la limite locale de 1 Mo.
- Une réponse Studio sans données ne peut plus être traitée comme un succès. Un HTTP 429 sur une lecture produit une erreur explicite ; après une soumission payante, le résultat reste incertain et n'est pas rejoué.
- Le formulaire conserve les champs après une erreur, indique le champ à corriger et distingue connexion renouvelable et temporaire. Les champs sont effacés après succès ou à la sortie de la page. Le serveur refuse les transferts concurrents et borne leur durée.

## Validation

Les tests Go couvrent la persistance DPAPI, les verrous, l'absence de double soumission, les changements de compte et de version source, ainsi que les régressions ci-dessus. Les tests JavaScript couvrent la copie de requêtes, les erreurs, les corrections, les doubles envois, le délai d'attente et l'effacement des champs.

La page a été exercée dans un navigateur Chromium isolé, avec un serveur local et des données fictives : largeurs 1440, 390 et 320 pixels, états initial/erreur/succès, focus des erreurs et contraste forcé Windows. Aucun accès au navigateur personnel ni à une session Studio n'est nécessaire à cette vérification. Firefox et WebKit n'ont pas fait l'objet d'un test visuel dans cet audit.

Les workflows de PR et de release exécutent `go vet`, les tests Go, le détecteur de courses et `govulncheck`, puis les tests JavaScript. Les builds Windows amd64 et arm64 sont vérifiés avant publication. Node et le navigateur de test ne sont pas des dépendances de l'exécutable distribué.

## Limites restantes

Studio n'offre pas de contrat public pour ces interfaces. Une évolution de ses endpoints, des contrôles humains, des droits ou des tarifs peut interrompre le client. Les échecs sont signalés sans basculement vers l'API commerciale ni contournement d'une vérification.

Les validations payantes réalisées avant cet audit couvrent les parcours texte HD → GLB/FBX et texte P2.0 → FBX quad → texture 8K → GLB 8K. Les autres opérations restent couvertes par des contrats simulés, sans garantie que toutes leurs combinaisons soient acceptées sur chaque abonnement.

La protection contre les doublons repose sur le journal de ce poste et un `request_id` stable. Elle ne coordonne pas plusieurs PC. Un résultat `outcome_unknown` doit être vérifié dans l'historique Studio avant une nouvelle demande. Une modification depuis le navigateur peut aussi intervenir entre le dernier contrôle de version et l'opération distante ; le service Studio reste responsable de l'atomicité de cette dernière.
