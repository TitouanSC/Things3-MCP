# Plan de tests manuels : mode local

Les tests automatisés valident le serveur contre un **simulateur** de Things.
Ce plan vérifie ce que seul le vrai Things peut confirmer : la façon dont
l'app interprète réellement les URL et les scripts.

## Règles

- **Données jetables uniquement** : tout se passe dans un projet `MCP-TEST`
  créé pour l'occasion, puis mis à la corbeille.
- Un tag `mcp-test` créé à la main dans Things avant de commencer.
- Le token `THINGS_AUTH_TOKEN` est configuré dans le client.
- Noter la version de Things (Things → À propos) et la date du test.

## Scénarios

Les demandes ci-dessous sont à formuler en langage naturel dans Claude
Desktop ou Claude Code. Pour chaque ligne, on vérifie le résultat dans l'app.

| # | Demande à Claude | Vérification dans Things | ✓ |
|---|---|---|---|
| 1 | « Liste mes tags » | `mcp-test` apparaît | |
| 2 | « Crée le projet MCP-TEST avec une tâche "a" et un heading "Phase 1" contenant "b" et "c" » | Projet créé, "a" en haut, "b" et "c" sous "Phase 1". Claude renvoie un ID. | |
| 3 | « Ajoute dans MCP-TEST une tâche "rappel" pour demain 9h30, tag mcp-test, checklist x, y » | Date de demain, rappel 09:30, tag, checklist à 2 éléments | |
| 4 | « Ajoute "suite" à la fin des notes de "rappel" » | Les notes contiennent la nouvelle ligne, **sans ligne vide en trop** (vérifie la convention du saut de ligne) | |
| 5 | « Enlève la date de "rappel" » | La tâche n'a plus de date (noter la vue où elle atterrit) | |
| 6 | « Déplace "a" sous le heading Phase 1 » | "a" est sous "Phase 1" | |
| 7 | « Marque "b" comme terminée », puis « rouvre-la » | Cochée et visible dans le Logbook, puis décochée | |
| 8 | « Annule "c" » | Barrée (annulée) | |
| 9 | « Mets "a" à la corbeille » | macOS demande l'autorisation « Automatisation » (première fois). "a" est dans la Corbeille et **restaurable**. | |
| 10 | « Montre-moi MCP-TEST dans Things » | Things passe au premier plan sur le projet | |
| 11 | « Crée une tâche avec le tag "inexistant" » | Claude reçoit une erreur claire, rien n'est créé | |
| 12 | Sur une tâche répétitive de test : « termine-la » | Erreur explicite (non supporté), la tâche est inchangée | |
| 13 | Sans `THINGS_AUTH_TOKEN` : « renomme "rappel" » | Erreur explicite qui mentionne `THINGS_AUTH_TOKEN` | |
| 14 | Things fermé : « crée une tâche "à froid" » | Things se lance en arrière-plan, la tâche est créée et l'ID renvoyé | |

## Nettoyage

« Mets le projet MCP-TEST à la corbeille », puis vide la corbeille à la main
si tu le souhaites (le serveur ne le fait jamais). Supprime le tag `mcp-test`.

## Résultats

| Date | Version de Things | Version du serveur | Résultat | Remarques |
|---|---|---|---|---|
| | | | | |
