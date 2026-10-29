# Phase 0 : étude de cadrage et de faisabilité

_Date : 2026-10-29. Statut : livré, en attente de validation._

Ce document résume l'étude préalable : capacités du URL scheme officiel, projets
existants, faisabilité du backend Things Cloud, risques, et liste d'outils MCP
proposée. Les décisions structurantes sont dans [`adr/`](adr/), le plan des
phases suivantes dans [`plan.md`](plan.md).

---

## 1. URL scheme officiel de Things

Source : <https://culturedcode.com/things/support/articles/2803573/> (version 2 du scheme).

| Commande | Rôle | `auth-token` | Retour x-callback |
|---|---|---|---|
| `add` | Créer une ou plusieurs tâches | non | `x-things-id` |
| `add-project` | Créer un projet (avec tâches) | non | `x-things-id` |
| `update` | Modifier une tâche | **oui** | `x-things-id` |
| `update-project` | Modifier un projet | **oui** | `x-things-id` |
| `json` | Création / mise à jour en masse (to-do, project, heading, checklist-item) | si `operation: update` | `x-things-ids` |
| `show` / `search` | Naviguer dans l'UI | non | aucun |
| `version` | Versions du scheme et de l'app | non | versions |

Points qui contraignent la conception :

- **Pas de suppression.** Aucune commande ne supprime ni ne met à la corbeille.
  Mettre à la corbeille en local passe donc par AppleScript (`delete` place
  l'élément dans la corbeille de Things), comme le fait `hald/things-mcp` pour les
  aires.
- **Pas de création d'aire ni de tag.** Les tags inexistants sont ignorés en
  silence.
- **Écriture « fire-and-forget ».** Pour récupérer l'ID créé, il faudrait un
  gestionnaire de x-callback-url, ce qui est lourd pour un processus CLI.
  Mitigation proposée : après l'écriture, interroger la base SQLite en lecture
  seule (titre et date de création postérieure à t0, avec un timeout court) pour
  retrouver l'ID et le renvoyer.
- **Tâches répétitives** : impossible de modifier `when`, `deadline`, `completed`
  ou `canceled`, ou de les dupliquer.
- **Projets** : ne peuvent être terminés que si tous les enfants sont terminés
  ou annulés et les headings archivés.
- **Limites** : 4 000 caractères par chaîne, 10 000 pour les notes, 100 éléments
  de checklist, 250 créations par fenêtre de 10 s.
- **Formats** : `when` ∈ `today | tomorrow | evening | anytime | someday |
  yyyy-mm-dd | date@heure` (une heure crée un rappel). Une valeur vide efface
  le champ.
- **Sécurité** : le token donne accès en écriture à toute la base. Il ne doit
  jamais apparaître dans un log, y compris dans l'URL `things:///update?...`
  (masquage obligatoire). L'URL est ouverte via `open -g` sans passer par un
  shell, pour éviter toute injection de commande.

## 2. Lecture locale (SQLite)

La base se trouve sous
`~/Library/Group Containers/JLMPQHK86H.com.culturedcode.ThingsMac/ThingsData-*/Things Database.thingsdatabase/main.sqlite`
(tables `TMTask`, `TMArea`, `TMTag`, `TMTaskTag`, `TMChecklistItem`). Le schéma
n'est pas documenté. La référence de facto est
[`thingsapi/things.py`](https://github.com/thingsapi/things.py) (Apache-2.0,
actif) :

- ouverture en `mode=ro` (et non `immutable=1`, qui figerait des données périmées
  pendant que Things écrit en WAL) ;
- dates `startDate` et `deadline` encodées en entier « bit-packed », dates de
  création et de modification en timestamp Unix flottant ;
- type (`0` tâche, `1` projet, `2` heading), statut (`0` ouvert, `2` annulé,
  `3` terminé), `trashed`, `start` (inbox, anytime, someday).

Risques : le schéma peut changer à chaque mise à jour de Things (mitigation :
couche de requêtes isolée et tests sur une fixture SQLite synthétique). macOS
peut demander l'autorisation « accéder aux données d'autres apps » au premier
accès au Group Container.

## 3. Projets existants

| Projet | Langage | Licence | Lecture / écriture | Transport | Remarques |
|---|---|---|---|---|---|
| [hald/things-mcp](https://github.com/hald/things-mcp) | Python | MIT | things.py (SQLite) / URL scheme + AppleScript (aires) | stdio, HTTP optionnel | La référence (environ 630 ★, actif). Environ 25 outils, un par vue (`get-inbox`, `get-today`…). |
| [hildersantos/things-mcp](https://github.com/hildersantos/things-mcp) | TypeScript | **aucune licence** | AppleScript + URL scheme / JSON | stdio | Sans licence, donc **aucun code réutilisable**, inspiration conceptuelle seulement. Inactif depuis 06/2025. |
| [Chevalicious/things3-mcp-gtd](https://github.com/Chevalicious/things3-mcp-gtd) | Python | MIT (à revérifier, un annuaire indique Apache-2.0) | URL scheme | streamable HTTP local | Couche « méthode GTD » (contextes, revue hebdo). |
| [mattydsmith/things-cloud-mcp](https://github.com/mattydsmith/things-cloud-mcp) | Go | MIT | Things Cloud via things-cloud-sdk | HTTP `/mcp` | **Archivé** (03/2026). 36 outils, Docker + Fly.io. Auth de `/mcp` non documentée (seul `/api/*` a un Bearer). |
| [arthursoares/things-cloud-sdk](https://github.com/arthursoares/things-cloud-sdk) | Go | MIT | Things Cloud (rétro-ingénierie) | n/a | SDK et CLI, actif (dernier push 09/2026), ~90 fichiers Go (~650 Ko avec tests). |

Positionnement : aucun de ces projets ne combine **un cœur unique et deux
backends interchangeables**, une **authentification sérieuse de l'endpoint
public**, un **modèle de menaces** et une CI de qualité. C'est cette
combinaison que le projet met en avant, pas la nouveauté fonctionnelle.

Leçon de conception tirée de l'existant : un outil par vue (`get-inbox`,
`get-today`…) gonfle le contexte du modèle. Je propose plutôt quelques outils
paramétrés (voir §6).

## 4. Faisabilité du backend Things Cloud

Faits établis (README et code de things-cloud-sdk) :

- Endpoint `https://cloud.culturedcode.com`, authentification par **e-mail et
  mot de passe du compte** (pas de token délégué ni d'OAuth côté Cultured Code).
- Modèle « historique » : la lecture consiste à synchroniser l'historique des
  commits et à rejouer l'état. Chaque écriture est un commit avec un
  `ancestor-index`. Les entités sont `Task6/Task7`, `Area3`, `Tag4`,
  `ChecklistItem3` et `Tombstone2`, avec des champs abrégés (`ss`, `st`, `sr`,
  `tir`, `md`, `rr`/`rp`/`rt`…).
- **Pièges qui font planter Things.app** chez l'utilisateur : UUID qui ne sont
  pas en Base58 canonique, heading avec `st=0`, `md` non nul à la création,
  payloads incomplets. Le README recommande lui-même la CLI plutôt que l'API
  brute de l'SDK.
- Récurrences incomplètement vérifiées, notes en écriture delta non supportées.

Évaluation :

| Option | Coût | Risque | Commentaire |
|---|---|---|---|
| Réutiliser `things-cloud-sdk` (Go) comme dépendance | **faible** | moyen (dépendance d'un mainteneur unique, protocole non officiel) | Version épinglée, MIT avec attribution, fork possible si abandon. |
| Porter le protocole en TypeScript | **élevé** (sync, replay d'état, Base58, payloads Task7, pièges) | **élevé** : un bug peut corrompre la base ou faire planter Things sur tous mes appareils | Gros travail de rétro-ingénierie sans valeur ajoutée pour l'objectif portfolio. |
| TS + sidecar Go (`things-cli` en sous-processus) | moyen | moyen | Deux toolchains, parsing de sortie CLI, image Docker plus lourde. |

**Conclusion** : le backend Cloud est faisable, et **nettement plus simple en
Go**. C'est le cas prévu dans les règles du projet. La proposition est
détaillée dans l'[ADR 001](adr/001-langage-et-structure.md) et attend ta
validation.

## 5. Contraintes pour le mode hébergé (à trancher en phase 4)

- **claude.ai (web et mobile)** : les connecteurs personnalisés supportent
  OAuth. Les en-têtes statiques (`Authorization: Bearer …`) existent en **bêta à
  déploiement progressif** et peuvent être absents du compte. Un simple token
  statique risque donc de ne pas suffire pour le mobile. Il faudra probablement
  un **serveur d'autorisation OAuth 2.1 minimal** (mono-utilisateur, PKCE,
  enregistrement dynamique de client), ou un IdP externe. Ce choix fera l'objet
  d'un ADR en phase 4.
- Le serveur hébergé détient le **mot de passe Things Cloud**, ce qui donne un
  accès total au compte. D'où un compte de test dédié, des secrets gérés par
  l'hébergeur, aucun log de contenu et une limitation de débit.

## 6. Outils MCP proposés

Principes : peu d'outils, bien paramétrés. Descriptions précises (formats,
limites, effets). Annotations MCP renseignées. Les outils dépendant d'une
capacité du backend ne sont enregistrés que si elle est disponible (par exemple,
`show_in_things` n'existe qu'en local).

### Lecture (`readOnlyHint: true`)

| Outil | Paramètres principaux | Retour |
|---|---|---|
| `list_tasks` | `view` (inbox, today, upcoming, anytime, someday, logbook, trash), `project_id`, `area_id`, `tag`, `limit` (50 par défaut, 200 max), `cursor` | tâches résumées et curseur suivant |
| `get_task` | `id` | tâche complète : notes, checklist, tags, projet, heading, aire |
| `list_projects` | `area_id`, `status` (open, completed, canceled), `limit`, `cursor` | projets |
| `get_project` | `id` | projet avec headings et tâches |
| `list_areas` | n/a | aires |
| `list_tags` | n/a | tags (avec hiérarchie) |
| `search` | `query` (titre et notes), `kind` (task, project), `status`, `limit` | résultats |

### Écriture

| Outil | Annotations | Notes |
|---|---|---|
| `create_task` | `destructiveHint: false` | titre, notes, when, deadline, tags, checklist, `project_id` ou `area_id`, `heading_id`. Renvoie l'ID. |
| `create_project` | `destructiveHint: false` | avec headings et tâches initiales |
| `update_task` | `destructiveHint: true` | mise à jour partielle. Les modes « remplacer » (`notes`, `tags`, `checklist`) et « ajouter » (`append_notes`, `add_tags`, `append_checklist`) sont séparés et explicites. |
| `update_project` | `destructiveHint: true` | idem, sans checklist |
| `set_task_status` | `destructiveHint: false`, `idempotentHint: true` | open, completed ou canceled (réversible) |
| `move_to_trash` | `destructiveHint: true`, `idempotentHint: true` | **corbeille uniquement**, restaurable. En local via AppleScript (permission Automatisation). |
| `show_in_things` | `readOnlyHint: true` | local uniquement : ouvre l'élément dans l'app |

### Exclus volontairement

- suppression définitive et vidage de la corbeille (règle du projet) ;
- configuration des répétitions (interdite par le URL scheme, non vérifiée côté
  Cloud) ;
- création d'aires et de tags en v1 (impossible via le URL scheme), envisageable
  plus tard derrière une capacité.

## 7. Registre des risques

| Risque | Impact | Mitigation |
|---|---|---|
| Protocole Cloud non officiel qui change | backend hébergé cassé | Version épinglée, tests de contrat, détection de version, avertissement dans le README |
| Écriture Cloud mal formée | corruption ou crash de Things | Passer uniquement par l'API validée du SDK, compte de test, aucun test sur le vrai compte |
| Schéma SQLite modifié | lecture locale cassée | Requêtes isolées, fixture synthétique, message d'erreur explicite |
| Fuite du token Things ou du mot de passe Cloud | accès total au compte | Variables d'environnement et secrets de l'hébergeur, masquage dans les logs, gitleaks en CI |
| Prompt injection via le contenu des tâches | actions non voulues par le LLM | Pas de suppression définitive, annotations destructives (confirmation côté client), modèle de menaces |
| Endpoint public non authentifié | accès par n'importe qui | Auth obligatoire au démarrage (refus de démarrer sans configuration), limitation de débit |
| Dépendance `things-cloud-sdk` abandonnée | maintenance | MIT, donc fork possible. Le backend Cloud reste isolé derrière l'interface. |
