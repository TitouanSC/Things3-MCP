# ADR 001 : Langage et structure du dépôt

- **Statut** : Proposé (en attente de validation)
- **Date** : 2026-10-29

## Contexte

Le serveur doit exposer le même cœur métier via deux transports (stdio local,
HTTP streamable hébergé) et deux backends (Mac local, Things Cloud). Le langage
par défaut prévu est TypeScript avec le SDK MCP officiel, sauf si l'étude montre
que le backend Cloud est bien plus simple ailleurs.

L'étude de phase 0 (§4 de [`phase-0-etude.md`](../phase-0-etude.md)) établit
que :

- la seule implémentation sérieuse du protocole Things Cloud (non officiel) est
  [`arthursoares/things-cloud-sdk`](https://github.com/arthursoares/things-cloud-sdk),
  en **Go**, sous licence MIT, active, avec environ 650 Ko de code et de tests
  (sync de l'historique, replay d'état, UUID Base58, payloads Task7) ;
- ce protocole comporte des pièges qui **font planter Things.app** sur les
  appareils de l'utilisateur en cas d'écriture mal formée ;
- un **SDK MCP officiel en Go** existe
  ([`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk),
  v1.8.0, maintenu avec Google) avec stdio, HTTP streamable et des primitives
  OAuth ;
- côté local, la lecture SQLite et l'ouverture d'URL `things:///` sont aussi
  simples dans les deux langages.

## Options

### A. Go pour tout le projet (recommandé)

- Le backend Cloud dépend directement de `things-cloud-sdk` (version épinglée).
- Le SDK MCP Go officiel gère stdio et HTTP.
- SQLite en Go pur (`modernc.org/sqlite`), sans CGO, ce qui facilite la
  compilation croisée.
- Un binaire statique unique et une image Docker minimale (distroless ou
  scratch), donc une surface d'attaque réduite.
- Outillage de qualité standard : `go test -race`, `go vet`, `golangci-lint`,
  `govulncheck`.

**Contre** : s'éloigne du profil « fullstack TypeScript ». Il faut installer Go
(absent de la machine). Le SDK MCP TS reste plus utilisé que le SDK Go.

### B. TypeScript avec un portage du protocole Cloud

**Pour** : cohérence avec le profil, et le SDK MCP le plus mature.
**Contre** : coût élevé et risque élevé de corrompre les données ou de faire
planter Things. On réécrit environ 650 Ko de logique de rétro-ingénierie sans
valeur démonstrative supplémentaire, et le code porté devient du code à
maintenir soi-même.

### C. TypeScript avec un sidecar Go (`things-cli` en sous-processus)

**Pour** : le cœur reste en TS.
**Contre** : deux toolchains, un contrat fragile (parsing de sortie CLI), une
image Docker plus lourde, des erreurs plus difficiles à remonter. C'est de la
complexité accidentelle.

## Décision (proposée)

**Option A : Go.** Le critère décisif est le risque sur les données : réutiliser
une implémentation testée du protocole plutôt que la réécrire. Pour un dossier
d'école d'ingénieurs, savoir justifier un changement de langage par une analyse
coût/risque a plus de valeur que de rester sur un langage par défaut.

## Structure du dépôt (option A)

```
cmd/things-mcp/            point d'entrée : --transport=stdio|http, --backend=local|cloud
internal/domain/           modèles : Task, Project, Area, Tag, Heading, ChecklistItem, erreurs
internal/backend/          interface TaskBackend et Capabilities
internal/backend/fake/     FakeBackend en mémoire (tests)
internal/backend/local/    LocalMacBackend : sqlite/ (lecture) et urlscheme/ (écriture), applescript/ (corbeille)
internal/backend/cloud/    CloudBackend : adaptateur vers things-cloud-sdk
internal/mcpserver/        enregistrement des outils MCP, schémas, annotations
internal/httpserver/       transport HTTP : auth, limitation de débit, logs
internal/config/           chargement de la configuration depuis l'environnement, validation
testdata/                  fixtures synthétiques (base SQLite générée, sans données réelles)
docs/adr/                  décisions d'architecture
docs/                      étude, plan, modèle de menaces, tests manuels
```

`internal/` empêche l'import externe : l'API publique, c'est le binaire.

## Conséquences

- Installer Go (version stable courante) et l'épingler dans `go.mod` et la CI.
- Le README et `NOTICE` attribuent `things-cloud-sdk` (MIT), `things.py`
  (Apache-2.0) pour la connaissance du schéma SQLite et le SDK MCP Go. Aucun code
  n'est copié de `hildersantos/things-mcp` (sans licence).
- Si l'option B ou C était retenue à la place, seuls les répertoires changent :
  l'architecture de l'[ADR 002](002-backends-derriere-une-interface.md) reste
  valable.
