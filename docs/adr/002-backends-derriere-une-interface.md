# ADR 002 : Deux backends derrière une interface `TaskBackend`

- **Statut** : Accepté (2026-10-30)
- **Date** : 2026-10-29

## Contexte

Les mêmes outils MCP doivent fonctionner :

1. en local sur macOS : écriture via le URL scheme `things:///` (et AppleScript
   pour la corbeille), lecture SQLite en lecture seule ;
2. en hébergé : Things Cloud via un protocole non officiel.

Les deux mécanismes diffèrent fortement. Le local est asynchrone et sans retour
d'ID ; le Cloud est synchrone mais demande une synchronisation préalable. Les
capacités diffèrent aussi : `show_in_things` n'existe qu'en local. Les tests ne
doivent jamais toucher un vrai compte ni de vraies données.

## Options

1. **Outils MCP écrits directement contre chaque backend** : deux jeux d'outils,
   donc des schémas, descriptions et validations dupliqués, et des
   comportements qui divergent.
2. **Interface unique `TaskBackend` avec des adaptateurs (ports et
   adaptateurs)** : une seule couche d'outils, testée une fois.
3. **Interface minimale (lecture seule) avec des écritures spécifiques par
   backend** : plus simple au départ, mais réintroduit la duplication côté
   écriture.

## Décision

**Option 2.** La couche `mcpserver` ne dépend que de l'interface :

```go
type TaskBackend interface {
    Capabilities() Capabilities

    // Lecture
    ListTasks(ctx, TaskQuery) (Page[Task], error)
    GetTask(ctx, id) (Task, error)
    ListProjects(ctx, ProjectQuery) (Page[Project], error)
    GetProject(ctx, id) (ProjectDetail, error)
    ListAreas(ctx) ([]Area, error)
    ListTags(ctx) ([]Tag, error)
    Search(ctx, SearchQuery) (Page[SearchHit], error)

    // Écriture (aucune suppression définitive dans l'interface)
    CreateTask(ctx, NewTask) (Task, error)
    CreateProject(ctx, NewProject) (Project, error)
    UpdateTask(ctx, id, TaskPatch) (Task, error)
    UpdateProject(ctx, id, ProjectPatch) (Project, error)
    SetTaskStatus(ctx, id, Status) error
    MoveToTrash(ctx, id) error
}

type Capabilities struct {
    Trash       bool // MoveToTrash disponible
    ShowInApp   bool // ouvrir l'élément dans Things (local uniquement)
    ReturnsIDs  bool // l'ID créé est connu de façon fiable
}
```

Trois implémentations :

| Adaptateur | Lecture | Écriture | Usage |
|---|---|---|---|
| `FakeBackend` | mémoire | mémoire | tests unitaires et de contrat |
| `LocalMacBackend` | SQLite `mode=ro` | URL scheme (`open -g`) et AppleScript (`osascript`) | stdio, macOS |
| `CloudBackend` | état synchronisé par `things-cloud-sdk` | commits via le SDK | HTTP hébergé |

### Mécanismes du backend local (décidé le 2026-10-30)

Le `LocalMacBackend` combine trois mécanismes, chacun là où il est le meilleur :

| Mécanisme | Usage | Pourquoi |
|---|---|---|
| **SQLite** (lecture seule) | toutes les lectures | rapide, aucune permission d'automatisation |
| **URL scheme** `things:///` | créations riches (checklists, headings, `json`) et mises à jour | officiel, le plus complet pour l'écriture |
| **AppleScript** (`osascript`) | mise à la corbeille, créations simples qui doivent renvoyer l'ID | seul moyen de mettre à la corbeille. Réponse synchrone avec l'ID créé. |

Contraintes liées à AppleScript :

- permission macOS « Automatisation » à accorder une fois ;
- aucune valeur insérée dans le texte du script : les valeurs passent en
  arguments (`on run argv`), avec validation stricte du format des ID, contre
  l'injection ;
- jamais de `empty trash` ni de suppression définitive.

Règles associées :

- **Pas de méthode de suppression définitive** dans l'interface. La règle est
  garantie par le typage, pas seulement par la documentation.
- **Erreurs typées** du domaine (`ErrNotFound`, `ErrUnsupported`,
  `ErrValidation`, `ErrBackendUnavailable`), traduites en erreurs d'outil MCP
  lisibles. Aucun détail interne ni secret ne fuit.
- **Validation dans le domaine**, indépendante du backend : limites du URL
  scheme (4 000 et 10 000 caractères, 100 éléments de checklist) et formats de
  `when` et `deadline`. Tous les backends se comportent ainsi de la même façon.
- **Suite de tests de contrat partagée** : les mêmes scénarios tournent contre
  `FakeBackend` (CI), contre `LocalMacBackend` sur une fixture SQLite pour la
  lecture, et contre `CloudBackend` sur un compte de test uniquement, avec un
  build tag et une exécution manuelle.
- **Outils conditionnés aux capacités** : un outil non supporté n'est pas
  enregistré, plutôt qu'enregistré puis en échec.

## Conséquences

- (+) Outils, schémas et annotations écrits et testés une seule fois. Ajouter un
  backend ne touche pas la couche MCP.
- (+) La CI (Linux) teste toute la logique MCP sans macOS ni compte Things.
- (−) L'interface doit viser le plus petit dénominateur commun. Les
  fonctionnalités propres à un seul backend passent par `Capabilities`.
- (−) En local, les créations riches via URL scheme ne renvoient pas l'ID. Il
  faut le retrouver par relecture SQLite (délai, ambiguïté si deux titres sont
  identiques). Les créations simples passent par AppleScript, qui renvoie l'ID
  directement.
- (−) Le mode local dépend de la permission « Automatisation » de macOS, à
  documenter dans le README.
