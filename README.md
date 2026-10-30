# Things3-MCP

Serveur [MCP](https://modelcontextprotocol.io) pour [Things 3](https://culturedcode.com/things/),
avec un cœur unique et deux modes de déploiement :

- **local** (macOS, stdio) pour Claude Desktop et Claude Code : lecture SQLite en
  lecture seule, écriture via le URL scheme `things:///` et AppleScript ;
- **hébergé** (HTTP) pour claude.ai web et mobile, via Things Cloud (phase 4, à venir).

> ⚠️ Projet en cours de construction : le mode local est utilisable, le mode
> hébergé est à venir. Voir [`docs/plan.md`](docs/plan.md) et les décisions
> dans [`docs/adr/`](docs/adr/).

## Avertissements

- **Projet non affilié à Cultured Code.** « Things » est une marque de Cultured Code GmbH & Co. KG.
- Le mode hébergé reposera sur un **protocole Things Cloud non officiel**
  (rétro-ingénierie) qui peut changer sans préavis. Utilise un compte de test.
- Le serveur ne supprime jamais définitivement : la suppression envoie l'élément
  dans la Corbeille de Things, d'où il reste restaurable.

## Outils exposés

| Lecture | Écriture |
|---|---|
| `list_tasks` (vues inbox, today, upcoming, anytime, someday, logbook, trash ; filtres projet, aire, tag) | `create_task`, `create_project` (avec headings) |
| `get_task`, `list_projects`, `get_project` | `update_task`, `update_project` (modes « remplacer » et « ajouter » explicites) |
| `list_areas`, `list_tags`, `search` | `set_task_status` (terminer, annuler, rouvrir) |
| `show_in_things` (local) | `move_to_trash` (corbeille, restaurable) |

Chaque outil porte les annotations MCP (`readOnlyHint`, `destructiveHint`,
`idempotentHint`), pour que le client puisse demander confirmation avant une
action destructive.

## Mode local (macOS)

### Prérequis

- macOS avec Things 3, ouvert au moins une fois ;
- Things → Réglages → Général → **Activer les URL Things** ;
- Go 1.27+ pour compiler.

### Installation

```sh
git clone git@github.com:TitouanSC/Things3-MCP.git
cd Things3-MCP
go build -o bin/things-mcp ./cmd/things-mcp
```

### Token d'autorisation (facultatif)

Sans token, le serveur lit, crée et met à la corbeille, mais refuse les
modifications (`update_task`, `update_project`, `set_task_status`). Pour les
activer, copie le token depuis Things → Réglages → Général → Activer les URL
Things → **Gérer**, et passe-le dans la variable `THINGS_AUTH_TOKEN`.

> Le token donne un accès en écriture à toute ta base Things. Il est stocké en
> clair dans la configuration du client MCP : ne la partage pas.

### Claude Desktop

Dans `~/Library/Application Support/Claude/claude_desktop_config.json` :

```json
{
  "mcpServers": {
    "things": {
      "command": "/chemin/absolu/vers/Things3-MCP/bin/things-mcp",
      "env": { "THINGS_AUTH_TOKEN": "ton-token" }
    }
  }
}
```

Puis redémarre Claude Desktop.

### Claude Code

```sh
claude mcp add things --scope user \
  --env THINGS_AUTH_TOKEN=ton-token \
  -- /chemin/absolu/vers/Things3-MCP/bin/things-mcp
```

### Permissions macOS

- **Accès aux données d'autres apps** : au premier lancement, macOS peut demander
  l'autorisation de lire les données de Things.
- **Automatisation** : à la première mise à la corbeille, macOS demande si
  l'application hôte (Claude, Terminal…) peut contrôler Things. Elle reste
  révocable dans Réglages Système → Confidentialité et sécurité → Automatisation.

### Variables d'environnement

| Variable | Rôle |
|---|---|
| `THINGS_AUTH_TOKEN` | Token des URL Things, requis pour les modifications |
| `THINGS_DB_PATH` | Chemin de la base Things (détecté automatiquement sinon) |

### Limites connues du mode local

- Le URL scheme ne renvoie pas l'ID de ce qu'il crée : le serveur le relit dans
  la base. Si Things met plus de 5 s à répondre, la création réussit quand même,
  mais l'ID n'est pas renvoyé.
- Les tâches répétitives ne peuvent être ni terminées ni replanifiées via le URL
  scheme : le serveur le signale par une erreur explicite.
- Les tags, projets, aires et headings doivent exister : le serveur refuse une
  référence inconnue, alors que Things l'ignorerait sans prévenir.
- Les dates en langage naturel (« next monday ») sont refusées : seules les
  valeurs `today`, `evening`, `tomorrow`, `anytime`, `someday` et `AAAA-MM-JJ`
  (avec `@HH:MM` facultatif) sont acceptées.

## Développement

```sh
make test     # tests avec détecteur de concurrence
make lint     # golangci-lint (config stricte, .golangci.yml)
make vuln     # govulncheck : vulnérabilités connues des dépendances
make secrets  # gitleaks : secrets dans tout l'historique Git
make check    # tout ce que lance la CI
make hooks    # active le hook pre-commit (gitleaks sur les fichiers indexés)
```

La CI GitHub Actions ([`ci.yml`](.github/workflows/ci.yml)) lance le lint, les
tests avec détecteur de concurrence et un seuil de couverture de 80 %, la
compilation sur macOS, govulncheck et gitleaks sur tout l'historique. Les
actions sont épinglées par SHA de commit, et Dependabot les tient à jour avec
les modules Go.

Les tests n'utilisent jamais ta base Things :
- le backend local est testé sur une base SQLite synthétique ;
- un simulateur applique les URL et les scripts comme le ferait Things ;
- la même suite de contrat ([`internal/backend/backendtest`](internal/backend/backendtest))
  tourne sur tous les backends.

Avant une version, suis le plan de [tests manuels](docs/tests-manuels-local.md)
sur le vrai Things, avec des données jetables.

## Licence et attributions

MIT, voir [`LICENSE`](LICENSE). Aucun code tiers n'est copié. Projets dont celui-ci s'inspire :

- [thingsapi/things.py](https://github.com/thingsapi/things.py) (Apache-2.0) : connaissance du schéma SQLite de Things (encodage des dates, types, statuts) ;
- [arthursoares/things-cloud-sdk](https://github.com/arthursoares/things-cloud-sdk) (MIT) : protocole Things Cloud ;
- [hald/things-mcp](https://github.com/hald/things-mcp) (MIT), [mattydsmith/things-cloud-mcp](https://github.com/mattydsmith/things-cloud-mcp) (MIT) : serveurs MCP Things existants ;
- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) et [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) : dépendances.
