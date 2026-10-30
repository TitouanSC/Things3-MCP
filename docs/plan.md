# Plan détaillé des phases

_Langage retenu : Go ([ADR 001](adr/001-langage-et-structure.md), accepté le 2026-10-30)._

Chaque phase se termine par un résumé, la liste de ce qui reste, et un arrêt
en attente de feu vert. Commits au format Conventional Commits.

## Phase 1 : cœur et contrat

1. `chore: init go module` : `go.mod`, `.gitignore`, `.env.example`, `LICENSE`
   (MIT, à confirmer), `README` minimal avec la mention de non-affiliation à
   Cultured Code.
2. `feat(domain)` : modèles (Task, Project, Area, Tag, Heading, ChecklistItem,
   Status, When), validation (limites, formats de date) et erreurs typées.
3. `feat(backend)` : interface `TaskBackend`, `Capabilities`, types de requêtes,
   `Page[T]` avec curseur.
4. `feat(backend/fake)` : FakeBackend en mémoire, déterministe (horloge et
   générateur d'ID injectés).
5. `test(contract)` : suite de contrat réutilisable, exécutée sur le
   FakeBackend.
6. `feat(mcpserver)` : les 14 outils de [`phase-0-etude.md` §6](phase-0-etude.md),
   avec schémas JSON, descriptions précises, annotations et enregistrement
   conditionné aux capacités.
7. `test(mcpserver)` : tests des outils via un client MCP en mémoire (schémas,
   annotations, erreurs, pagination).

## Phase 2 : mode local macOS (stdio)

1. `feat(local/sqlite)` : localisation de la base, ouverture en `mode=ro`,
   requêtes, décodage des dates bit-packed. Tests sur une **fixture SQLite
   synthétique** générée par un script (aucune donnée réelle).
2. `feat(local/urlscheme)` : construction des URLs (encodage, `json` pour les
   créations riches), exécution `open -g` sans shell, token lu depuis
   `THINGS_AUTH_TOKEN` et masqué dans les logs. Tests unitaires sur les URLs
   produites, avec un exécuteur injecté.
3. `feat(local)` : relecture de l'ID après création (polling borné), corbeille
   via AppleScript, `show_in_things`.
4. `feat(cmd)` : `things-mcp --transport=stdio --backend=local`.
5. `docs` : configuration de Claude Desktop et Claude Code dans le README, et
   `docs/tests-manuels-local.md` (plan d'intégration sur Things réel, avec un
   projet jetable « MCP-TEST » créé puis mis à la corbeille).

## Phase 3 : qualité et CI

1. `golangci-lint` (config stricte), `gofmt` et `goimports`, `go vet`.
2. GitHub Actions : lint, `go test -race -cover` (Linux), build macOS, rapport
   de couverture avec un seuil, et `govulncheck`.
3. Secrets : `gitleaks` en CI et en pre-commit, scan complet de l'historique.
4. Dependabot (modules Go et Actions), Actions épinglées par SHA.

## Phase 4 : mode hébergé (HTTP)

1. **ADR 003, authentification** : token statique, OAuth 2.1 intégré
   mono-utilisateur (PKCE et enregistrement dynamique), ou IdP externe. À
   évaluer selon le support réel des connecteurs claude.ai (les en-têtes
   statiques sont en bêta).
2. `feat(cloud)` : CloudBackend sur `things-cloud-sdk` (version épinglée), avec
   sync au démarrage et en tâche de fond. Tests de contrat sur un **compte de
   test** uniquement (build tag `cloudintegration`, exécution manuelle).
3. `feat(http)` : transport streamable HTTP, middleware d'auth, limitation de
   débit, timeouts, taille de requête bornée, logs structurés sans contenu de
   tâches ni secrets, `/healthz`.
4. `docs/threat-model.md` : STRIDE court (endpoint public, secrets Cloud,
   prompt injection via le contenu des tâches, dépendance non officielle).

## Phase 5 : déploiement et documentation

1. **ADR 004, hébergeur** : Fly.io ou AWS Lightsail (coût, secrets, TLS,
   simplicité).
2. Dockerfile multi-stage (binaire statique, image distroless, utilisateur non
   root).
3. Pipeline de déploiement (GitHub Actions avec environnement protégé), secrets
   gérés par l'hébergeur.
4. README final : schéma d'architecture, installation locale et hébergée,
   limites connues (protocole non officiel), avertissement de sécurité,
   attributions et licences.
