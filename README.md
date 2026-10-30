# Things3-MCP

Serveur [MCP](https://modelcontextprotocol.io) pour [Things 3](https://culturedcode.com/things/),
avec un cœur unique et deux modes de déploiement :

- **local** (macOS, stdio) pour Claude Desktop et Claude Code : lecture SQLite en
  lecture seule, écriture via le URL scheme `things:///` et AppleScript ;
- **hébergé** (HTTP) pour claude.ai web et mobile, via Things Cloud.

> ⚠️ Projet en cours de construction (phase 1 : cœur et contrat).
> Voir [`docs/plan.md`](docs/plan.md) et les décisions dans [`docs/adr/`](docs/adr/).

## Avertissements

- **Projet non affilié à Cultured Code.** « Things » est une marque de Cultured Code GmbH & Co. KG.
- Le mode hébergé repose sur un **protocole Things Cloud non officiel**
  (rétro-ingénierie) qui peut changer sans préavis. Utilise un compte de test.
- Le serveur ne supprime jamais définitivement : la suppression envoie l'élément
  dans la Corbeille de Things, d'où il reste restaurable.

## Développement

```sh
go test ./...
```

## Licence et attributions

MIT, voir [`LICENSE`](LICENSE). Projets dont celui-ci s'inspire (aucun code copié à ce stade) :

- [thingsapi/things.py](https://github.com/thingsapi/things.py) (Apache-2.0) : connaissance du schéma SQLite de Things ;
- [arthursoares/things-cloud-sdk](https://github.com/arthursoares/things-cloud-sdk) (MIT) : protocole Things Cloud ;
- [hald/things-mcp](https://github.com/hald/things-mcp) (MIT), [mattydsmith/things-cloud-mcp](https://github.com/mattydsmith/things-cloud-mcp) (MIT) : serveurs MCP Things existants.
