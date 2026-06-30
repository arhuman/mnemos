# Où mnemos range sa KB : analyse architecturale

Statut : proposition de design (non implémentée). Répond à la question « doit-on
pouvoir spécifier l'emplacement par défaut de la KB, p. ex. `~/.mnemos/kb/`, avec
un `mnemos.toml` par défaut dans `~/.mnemos/` — et est-ce que ça règle le footgun
du README ? »

## TL;DR

- **Oui** à un emplacement par défaut indépendant du cwd, mais **en mode de
  repli**, pas en remplacement du modèle projet-scoped. Le mode projet reste
  prioritaire quand un `.mnemos.toml` est découvert ; `~/.mnemos/kb/` n'est utilisé
  que lorsque rien d'autre n'est trouvé.
- **Oui** à `~/.mnemos/mnemos.toml` : ça unifie le home avec `~/.mnemos/models/`
  qui **existe déjà** (`internal/embed/embedder.go:53`). Aujourd'hui la config home
  est le dotfile `~/.mnemos.toml`, à côté mais distinct du répertoire `~/.mnemos/`.
  Cette dualité dossier/dotfile est une dette de cohérence.
- Le « footgun » est en réalité **trois footguns distincts**. La proposition en
  règle deux, en aggrave légèrement un troisième (collisions d'URI) qu'il faut
  documenter, pas masquer.
- Recommandation : **phase 1 surgicale** (débloque immédiatement le cas `~/MEMORY`)
  + **phase 2 architecturale** (mode global + clé `tree_root` explicite).

## Le modèle actuel, en trois concepts qu'on a conflés

`mnemos` est **délibérément projet-scoped** : « mnemos operates inside one project
directory », « there is no global/shared store » (`docs/paths-and-indexing.md`).
Trois notions sont aujourd'hui dérivées les unes des autres :

| Concept | Rôle | Résolution actuelle |
|---|---|---|
| **Config** | d'où viennent les clés | `--config`, sinon `~/.mnemos.toml` + `./.mnemos.toml` |
| **Tree root** | namespace des URI + confinement des écritures | `filepath.Dir(--config)`, sinon `.` (cwd) |
| **Storage / capture** | où vit la DB et les notes | `[storage].path` / `[capture].dir`, **relatifs au tree root** |

La chaîne est : **config → tree root → storage**. Élégant en mono-projet (tout
s'ancre à côté du `.mnemos.toml`), mais **rigide** dès qu'on veut une KB située
ailleurs que le cwd ou le dossier de la config — exactement le cas `~/MEMORY`.

Code de référence :
- `config.Resolve` (`internal/config/config.go:164`) — `--config` ⇒ tree root =
  son dossier ; sinon tree root = `"."`.
- `app.Load` (`internal/app/app.go:63`) — résout un `[storage].path` relatif
  contre le tree root, **une seule fois**, pour qu'il soit absolu partout.
- `serve` (`internal/cli/serve.go:40`) — **rejette tout `capture.dir` absolu**.

## Les trois footguns (être précis)

1. **Footgun cwd-MCP** (README, section « Why the `--config` path must be
   absolute »). Claude Code ne garantit pas le cwd du serveur ⇒ obligation de
   passer un `--config` **absolu**. Sans lui, `serve` ne trouve la DB que si le cwd
   est par hasard la racine projet.

2. **Footgun `capture.dir` absolu** — *celui qu'on vient de heurter*. La config
   `~/MEMORY/mnemosAI.toml` avait `dir = "/Users/arnaud/MEMORY/.mnemos/capture"`.
   Or ce chemin est **exactement** `treeRoot + "/.mnemos/capture"`, soit le défaut,
   juste exprimé en absolu. `serve` l'a rejeté quand même. **Faux positif pur** :
   la valeur est sémantiquement valide, seule sa forme déplaît.

3. **Footgun collision d'URI** (README, « Mind the URI footgun »). L'identité d'un
   doc est son chemin **relatif à la racine de scan** ; deux arbres avec `index.md`
   collisionnent, le second `ingest` écrase le premier.

Ce que la proposition de l'utilisateur adresse :

| Footgun | Réglé par la proposition ? |
|---|---|
| 1 — cwd-MCP | **Oui** : un défaut ancré sur `~/.mnemos/` ne dépend plus du cwd |
| 2 — capture.dir absolu | **Oui** : patch chirurgical (accepter si à l'intérieur du root) |
| 3 — collision d'URI | **Non**, et une KB globale unique **l'aggrave** ⇒ à documenter |

## La vraie tension : projet-scoped vs KB personnelle unique

Il existe deux topologies de déploiement légitimes :

- **A. KB par projet** (le design documenté) : `.mnemos/` dans chaque dépôt, URI
  relatifs au projet, confinement net. C'est le défaut sain.
- **B. KB personnelle unique** (le setup `~/MEMORY` de l'utilisateur) : une seule
  racine contenant plusieurs sous-arbres (`pro/epfl/accred/...`), utilisée quel
  que soit le cwd. C'est ce qui déclenche les footguns 1 et 2, parce que la KB
  n'est *jamais* dans le cwd et qu'on bricole un `--config` absolu pour compenser.

La topologie B est aujourd'hui **non reconnue** : on l'obtient en détournant le
mécanisme `--config`. La proposition de l'utilisateur revient à **promouvoir B en
mode first-class**.

## Proposition : un home unifié + trois modes + une échappatoire

### 1. Unifier le home sur `~/.mnemos/` (overridable par `$MNEMOS_HOME`)

`~/.mnemos/` est déjà réel (`models/`). On y ajoute :

```
~/.mnemos/
  mnemos.toml      # config du mode global (créée par `mnemos init --global`)
  kb/              # tree root par défaut du mode global
  models/          # déjà là
```

`~/.mnemos.toml` (dotfile) **reste supporté** comme couche d'override
cross-projet, avec dépréciation douce (warning si présent, lecture conservée).
Rôles distincts, donc cohabitation propre :
- `~/.mnemos.toml` = *réglages appliqués à tout projet* (couche d'override).
- `~/.mnemos/mnemos.toml` = *config de la KB globale* (utilisée en mode global).

### 2. Précédence de résolution à trois modes

1. **Mode explicite** — `--config <path>` (ou `$MNEMOS_CONFIG`). Inchangé. Tree
   root = dossier de la config, ou `[storage].tree_root` si défini (voir §3).
   *C'est ce que fait `~/MEMORY` aujourd'hui.*
2. **Mode projet** — pas de `--config`, mais un `.mnemos.toml` découvert dans le
   cwd (option : en remontant jusqu'à la racine git, comme `.git`). Tree root = ce
   dossier. **Reste prioritaire** : on ne casse pas le design projet-scoped.
3. **Mode global** *(nouveau)* — pas de `--config`, aucun `.mnemos.toml` projet.
   Config = `~/.mnemos/mnemos.toml`, tree root = `~/.mnemos/kb/`. **Indépendant du
   cwd** ⇒ tue le footgun 1 pour le cas par défaut : un `mnemos serve` nu, lancé
   depuis n'importe où, trouve la KB personnelle.

### 3. Clé `[storage].tree_root` explicite (échappatoire)

Découple *location de la config* et *tree root*. Absolu autorisé. Quand elle est
posée, elle prime sur l'inférence « tree root = dossier de la config ». Permet :

```toml
# ~/.config/mnemos/mnemos.toml
[storage]
tree_root = "/Users/arnaud/MEMORY"   # KB ailleurs que la config
path = ".mnemos/mnemos.db"           # toujours relatif au tree_root
```

C'est l'expression *propre* du setup `~/MEMORY` actuel : plus besoin de gymnastique
relative, plus de faux positif sur `capture.dir`.

### 4. Patch chirurgical du footgun 2 (indépendant, livrable seul)

Dans `serve` (`internal/cli/serve.go:40`), **ne plus rejeter** un `capture.dir`
absolu d'office. À la place :

1. le résoudre relativement au tree root (`filepath.Rel(treeRoot, captureDir)`) ;
2. rejeter **uniquement** s'il s'échappe du root (préfixe `..` ou hors confinement,
   réutiliser le garde de `internal/security/paths.go`).

Mieux : déplacer cette validation dans une méthode `Config.Validate(treeRoot)`
appelée par `app.Load`, pour que **toutes** les commandes partagent le contrôle
(et pas seulement `serve`), avec le tree root dans le message d'erreur.

Effet : la config `~/MEMORY/mnemosAI.toml` d'origine (avec son `dir` absolu)
**fonctionnerait telle quelle**, sans édition.

## Réponses directes aux questions posées

- **« La KB par défaut doit-elle être à `~/.mnemos/kb/` ? »** — Oui, **comme défaut
  du mode global (repli)**, jamais en écrasant le mode projet. Le projet-scoped
  reste le défaut quand un `.mnemos.toml` existe ; `~/.mnemos/kb/` ne sert que
  lorsqu'aucune config projet/explicite n'est trouvée.

- **« Un `mnemos.toml` par défaut doit-il être créé/lu dans `~/.mnemos/` ? »** —
  Oui. Ça unifie avec `~/.mnemos/models/` déjà présent et donne au mode global sa
  propre config. `mnemos init --global` la scaffolde. `~/.mnemos.toml` (dotfile)
  reste lu en couche d'override, déprécié en douceur.

- **« Ça règle le footgun du README ? »** — Ça règle **2 footguns sur 3** : le
  cwd-MCP (mode global ancré sur `~/.mnemos/`) et le `capture.dir` absolu (patch
  §4 + clé `tree_root`). Le **footgun de collision d'URI n'est pas réglé** et une
  KB globale unique l'**accentue** : deux projets avec `README.md` collisionnent.
  Atténuation obligatoire : en mode global, namespacer par sous-dossier
  (`pro/epfl/accred/...`, ce que `~/MEMORY` fait déjà) et **logguer clairement** le
  mode actif (« using global KB at ~/.mnemos/kb »).

## Plan de livraison recommandé

- **Phase 1 (petit, faible risque, débloque tout de suite)** : patch §4 — accepter
  un `capture.dir`/`storage.path` absolu qui résout *à l'intérieur* du tree root ;
  porter la validation dans `Config.Validate(treeRoot)` ; meilleur message
  d'erreur. ~quelques lignes + tests. Rend la config `~/MEMORY` actuelle valide.
- **Phase 2 (architecture)** : home unifié `~/.mnemos/`, **mode global**
  (`~/.mnemos/mnemos.toml` + `~/.mnemos/kb/`), clé **`[storage].tree_root`**,
  `mnemos init --global`. Mettre à jour `docs/paths-and-indexing.md` (les trois
  modes) et le caveat collision d'URI pour KB globale. Mérite un **ADR** (cf.
  `docs/adr/`) car ça touche le contrat de localisation de l'état.

## Annexe : cohérence du vocabulaire de chemins

Recensement du code : ~13 graphies pour seulement **4 concepts réels**. Le reste
est soit des synonymes accidentels, soit une même valeur sous plusieurs noms.

| Concept | Définition | Graphies trouvées | Verdict |
|---|---|---|---|
| **tree root** | racine writable : base des URI + frontière de confinement ; ancre les chemins relatifs | `treeRoot`/`TreeRoot`/`tree root`/`tree-root` (159×), + `project root`, `workspace root` (7×) | canonique `tree root` ; tuer project/workspace root |
| **scan root** | le `<path>` passé à `ingest` ; URI relatif à lui | `scan root` (5×) | garder — distinct |
| **storage path** | le fichier DB | `[storage].path` (18×) ; dossier parent : `StorageDir`/`storage dir`/`dbDir` (39×) | garder `storage path` ; le dossier = `dir(storagePath)`, 1 identifiant |
| **capture dir** | notes auto-nommées (sous-ensemble du tree root) | `capture.dir`/`Capture.Dir`/`captureDir`/`capture_dir` (37×) | garder, 1 orthographe par contexte |

**Distinction à préserver** : `tree root` ≠ `scan root`. `cd ~/project` (tree root)
puis `mnemos ingest docs` (scan root = `~/project/docs`) ⇒ URI relatifs à `docs/`
mais écritures confinées à `~/project`. Cette divergence **est** le footgun 3
(collision d'URI). Les garder distincts est correct ; resserrer leur relation
(scan root par défaut = tree root) est une amélioration séparée.

**À tuer (synonymes accidentels)** :
- `project root`, `workspace root` → `tree root` partout.
- `StorageDir` / `storage dir` / `dbDir` → un seul `storageDir` (valeur dérivée,
  pas un concept).
- `capture_dir` / `captureDir` / `Capture.Dir` → règle d'orthographe **par
  contexte**, pas trois synonymes.

**Règle canonique** — une racine lexicale par concept, déclinée par casse :

| Concept | TOML | Go | Prose & erreurs |
|---|---|---|---|
| tree root | `storage.tree_root` (cf. §3) | `TreeRoot()` / `treeRoot` | « tree root » |
| scan root | — (arg CLI) | `scanRoot` | « scan root » |
| storage path | `storage.path` | `Storage.Path` / `storagePath` | « storage path » |
| capture dir | `capture.dir` | `Capture.Dir` / `captureDir` | « capture dir » |

## Annexe : ingérer du contenu externe (règle copy-first)

Constat : `mnemos ingest` (bulk) n'a **aucune** notion de tree root — pas de
confinement. Pourtant `okfy` confine déjà sa source au tree root
(`internal/ingest/okfy.go:65`, `security.ResolveWithin`). **Cette asymétrie est le
trou** : l'invariant « rester dans l'arbre » est appliqué pour okfy, pas pour
ingest. Et le MCP n'expose pas `ingest` (CLI-only), donc c'est un workflow
humain/CLI.

Deux problèmes distincts derrière « pb d'URI » :

| Problème | Statut | Fix |
|---|---|---|
| **A.** source hors tree root → URI pendants (illisibles par URI, invisibles à `ls`/`move`) | invariant universel, **non gardé** pour ingest | garde code : réutiliser `security.ResolveWithin` comme okfy |
| **B.** scan root = sous-dossier → URI courts qui ne matchent pas le chemin disque (`ls` dit « no ») | choix de design assumé (`ingest docs` → URI courts) | URI tree-root-relatif, **ou** commande `import` |

**Où spécifier la méthode (copy-into-tree puis ingest)** — par ordre de force :

1. **Code (primaire)** : `ingest` doit confiner la racine de scan au tree root et
   échouer proprement si dehors (comme `OpenStore` pour la DB, comme le garde
   d'écriture pour remember/forget/move). Mieux : une commande first-class
   `mnemos import <external> --into <sous-chemin> --collection X` qui copie **puis**
   ingère atomiquement (URI tree-root-relatif) — méthode *correcte par
   construction*.
2. **Docs (secondaire)** : section « Adding external content to the tree » dans
   `docs/paths-and-indexing.md`.
3. **Skill (tertiaire, 1 ligne)** : près de `SKILL.md:92`, noter que le bulk
   externe se copie dans l'arbre avant `mnemos ingest` ; rappeler que `remember`
   et `okfy` sont déjà confinés. Pas plus — l'agent n'ingère pas via MCP.

Le problème B rejoint le mode global (§2) : une KB perso unique *veut* l'URI =
chemin complet, donc tree-root-relatif y est le bon défaut.

## Risques / points de vigilance

- **Surprise de découverte** : un `mnemos search` nu dans un dossier quelconque
  taperait soudain la KB globale au lieu d'échouer. C'est *mieux* que l'erreur
  actuelle « no database », mais doit être **loggué** explicitement.
- **Sécurité du confinement** : le garde d'écriture (`internal/security/paths.go`)
  suppose un tree root bien défini ; `tree_root` absolu doit passer par le même
  garde, sans exception.
- **Compat ascendante** : ne pas casser `~/.mnemos.toml` ni le mode `--config`
  absolu déjà documenté ; les deux restent des chemins valides.
- **Le mode global ne doit pas redevenir un “global store” implicite** : il reste
  *un* tree root explicite (`~/.mnemos/kb`), pas un agrégat de toutes les DB
  projet. Le principe « one store per tree root » tient.
