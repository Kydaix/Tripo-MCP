# Connexion renouvelable

Le navigateur établit la session Studio ; Tripo-MCP reçoit manuellement les en-têtes d’accès et le cookie `ory_kratos_session` dans son portail local. Il ne lit aucun profil de navigateur. Les deux secrets résident dans `session.dpapi`, protégé par Windows DPAPI.

## Contrat observé

Le bundle public Studio [`BOQlA7aJ.js`](https://tripo-webapp-assets.tripo3d.ai/studio-prod/_nuxt/BOQlA7aJ.js), vérifié le 5 octobre 2026, utilise :

- le cookie de production `ory_kratos_session` ;
- `GET https://api.tripo3d.ai/v2/studio/studio/whoami?tokenizeAs=default_jwt` ;
- le champ de réponse `tokenized` pour le JWT ;
- une marge de renouvellement de 60 secondes.

Studio s’appuie sur le mécanisme [session → JWT d’Ory](https://www.ory.com/docs/identities/session-to-jwt-cors). Ce n’est pas un flux OAuth avec un `refresh_token`. Le point d’entrée Studio est interne et peut changer.

## Parcours

1. Le transfert vérifie le cookie en demandant un jeton et en comparant le compte avec celui du jeton copié. Il vérifie ensuite la consultation du compte avant d’enregistrer la session.
2. Chaque requête métier relit la session locale. Si le jeton expire dans moins d’une minute, le processus prend `auth.lock`, relit le fichier, puis renouvelle si nécessaire. Les autres processus utilisent le résultat sauvegardé atomiquement.
3. La réponse peut inclure `active`, `expires_at` et un nouveau cookie. Le client refuse une session inactive/expirée, un changement de compte, une redirection ou une réponse inconnue. Il conserve une éventuelle rotation du cookie.
4. Les erreurs réseau et refus de vérification humaine ne suppriment pas les identifiants. Une session déclarée expirée/révoquée lors du renouvellement est supprimée. `logout` partage le même verrou ; un renouvellement ne peut pas ressusciter la session après déconnexion.

Le portail attend le transfert avec `login.lock`, puis prend brièvement `auth.lock` pour enregistrer la connexion. Laisser une page de connexion ouverte ne bloque donc pas le renouvellement de la session existante.

La page conserve les champs en mémoire pendant la vérification et après un refus, pour permettre une correction sans tout recopier. Elle les efface après réussite ou lorsqu'on quitte la page ; aucun secret n'est stocké dans le stockage web. Les doubles envois sont refusés. La vérification serveur dispose de 60 secondes et le navigateur interrompt son attente après 65 secondes. Une annulation détectée avant l'enregistrement empêche la création de la session.

Les opérations Studio ne reçoivent que le jeton d’accès et les en-têtes Tripo. Les téléchargements gardent leur transport séparé. Le cookie n’est transmis qu’à l’URL de connexion fixe, sans suivre de redirection. Aucune réponse publique, trace ou tâche enregistrée ne contient les secrets.

## Validation

Les tests couvrent le contrat HTTP, la rotation, la persistance chiffrée, les erreurs et changements de compte, les renouvellements concurrents, les clients déjà ouverts, la déconnexion, le mode temporaire et le refus de soumettre une génération si le renouvellement échoue. Le transfert et la consultation du compte ne consomment aucun crédit de génération.

La durée de validité n’est pas codée en dur. `session` décrit seulement les identifiants locaux ; seul un accès distant permet de détecter une révocation anticipée. La fermeture du navigateur ne supprime pas la copie locale, mais une déconnexion distante peut la révoquer. Les vérifications humaines restent manuelles.
