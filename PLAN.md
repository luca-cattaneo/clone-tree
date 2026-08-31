# clone-tree — Generalization Plan

> Generic manager for **parallel git worktrees, each with its own isolated runtime stack**
> (docker compose project, database, ports, DNS). Extracted from the lessons of
> `TagPay/devtools/tagpay-worktree.sh` (1240 → 1019 lines of bash, still repo-welded).

## 1. Problem

Running N branches of the same repo concurrently (parallel AI agents, parallel reviews,
long-lived feature envs) requires more than `git worktree add`:

| Layer | What breaks without isolation |
|-------|-------------------------------|
| Compose project | containers collide (`COMPOSE_PROJECT_NAME`) |
| Host ports | second stack can't bind 443/3306/… |
| Database | both stacks write the same datadir |
| DNS / TLS origin | OAuth redirect URIs, cookies, SSR URLs pinned to one hostname |
| Gitignored files | `.env`, conf/, vendor/, IDE config absent from a fresh worktree |

Existing worktree managers stop at the git layer. Nothing mainstream handles the
stack-isolation half. That is the gap clone-tree fills.

## 2. Prior art & lessons learned (from tagpay-worktree.sh)

1. **Never patch YAML with sed.** All per-instance variation goes through
   `${VAR:-default}` interpolation in the repo's OWN compose files (incl. its
   `docker-compose.override.yml`). No third "overlay" file: TagPay's
   `devtools/docker-compose.worktree.yml` was a workaround for a DevAnsible-generated
   override full of literals → fixed upstream by parameterizing that template.
   clone-tree only generates a `.env`.
2. **Slot model works.** Small integer slot (1..N) → deterministic port offsets,
   deterministic DNS, deterministic data dir. Persist name→slot in a flat file.
3. **Port math belongs in config, not code.** `base + slot * step` per variable.
4. **Busy-port detection needs both** `lsof` and a TCP connect probe (root-owned
   listeners are invisible to unprivileged lsof).
5. **CoW clone the database datadir** (`cp -Rc` on APFS, `cp --reflink=auto` on
   btrfs/xfs, fallback plain copy). Seconds instead of minutes.
6. **Relative `../sibling` mounts** in compose files are solved with symlinks in the
   worktrees dir — zero file rewriting.
7. **The idiosyncratic 20% cannot be generalized** (OAuth URI SQL patch, IDE config
   patching, cache flushes) → repo-local **hooks**, not features.
8. **Rollback on failed create is mandatory** — a half-provisioned instance is worse
   than none.

## 3. Architecture

**Language: Go.** Own repo → `go.mod` + cobra are fine here (unlike embedding in a
product repo). Single static binary, `text/tabwriter` for tables, cobra for
dispatch/help/completions. Unit tests on slots/ports/config.

**Split: generic core (this repo) + per-repo config + per-repo hooks.**

```
clone-tree (binary)
   reads  <repo>/.clone-tree/config.yaml     ← declarative, committed by consumer repo
   runs   <repo>/.clone-tree/hooks/*         ← imperative escape hatch, committed
   writes <worktrees-dir>/<name>/            ← the instance
   state  <worktrees-dir>/.slots             ← name:slot registry
```

### 3.0 Config resolution — AUTO-SCAFFOLD when missing

```
1. --config <path>                        ← escape hatch (test on a repo pre-merge)
2. <git-toplevel>/.clone-tree/config.yaml ← the committed contract
3. none found → GENERATE it by scanning the repo, then proceed
```

No pre-existing config is never an error. Scaffold = **read, never guess**:

**Ports.** Resolve like compose does: `docker compose config --no-interpolate`
(merges `COMPOSE_FILE`/override/`!override`, keeps `${VAR}` names; fallback: own
yaml.v3 parse of the same file list). For each host binding `[ip:]HOST:container[/proto]`:

| Binding | name | value |
|---|---|---|
| `${VAR:-dflt}` | VAR | `.env[VAR]` ?? dflt |
| `${VAR}` | VAR | `.env[VAR]`; missing → `base: null` + `# set me` |
| literal `8443` | — (no entry, no `.env` var) | comment only: `# service X cannot be replicated: non-parameterized host port 8443` (ct never rewrites YAML) |

Base/step rule (slot 0 = main keeps originals): `base = original` (`< 1024` → `10000 + original`,
unprivileged), `step = 10` for every var. Collision pass over slots 1..max_slots: a var's series
must not hit any other var's base or series, **nor any literal (non-parameterized) port** — those
are bound identically by main and every worktree, permanent obstacles. Offender's step ×10 until
clean (cap 3 bumps → `# collision` comment).

Runtime safety net: `ct create` probes busy ports (lsof + TCP connect) first.

**files: — all COMMENTED proposals, human uncomments.** Buckets from `git status --ignored`:
dep dirs by name (`vendor node_modules .venv venv target .gradle`) → `hardlink`, removed
from copy; IDE dirs (`.idea .vscode .fleet .vs`) → own `ide:` key (copy semantics, patching = hook);
rest → `copy`. Excluded only `.git/ .clone-tree/`.

**.env model** (create, not scaffold): `cp main/.env` → strip keys ct overrides → append
`# ── ct (slot N) ──` block with port vars, `env:`, `CT_NAME`, `CT_SLOT`. Inherits secrets.
Main has no `.env` → generate from scratch.

**hardlink fallback** = symlink (not copy). Defaults: `worktrees_dir ../{repo}-worktrees`,
`max_slots 9`, `dns_pattern ""`. Generated file printed with a "review & commit" notice.

### 3.1 Consumer config — `.clone-tree/config.yaml`

```yaml
version: 1
worktrees_dir: ../{repo}-worktrees        # {repo} = repo dir name
dns_pattern: "local-{name}.dev.tagpay.fr" # empty → no DNS/hosts management
max_slots: 9

ports:                                     # env var → base + slot*step
  PROXY_HTTPS_PORT: {base: 10443, step: 100}
  PROXY_HTTP_PORT:  {base: 10080, step: 100}
  DB_PORT:          {base: 3306,  step: 10}
  # ...

env:                                       # extra .env lines (templated)
  PHP_SERVER_NAME: "tagpay-{name}"
  WORKTREE_DB_DATA_DIR: "{projects_dir}/docker_data/tagpay_mariadb_{name}"

files:
  ide:       [.idea/]                      # IDE config dirs: copied verbatim; per-worktree patching → post_create hook
  copy:      [docker-compose.override.yml, conf/config.local.php, data/config.bin, data/docker/, .claude/settings.local.json]
  hardlink:  [vendor/]
  clone_cow: [{src: "{projects_dir}/docker_data/tagpay_mariadb", dst: "{projects_dir}/docker_data/tagpay_mariadb_{name}"}]
  symlink_siblings: [TagWebPartners, TagWebClient, SepaGateway, TagAid, ModSecurity, sftp, docker_data]

hooks:                                     # optional executables, receive env: CT_NAME, CT_SLOT, CT_DNS, CT_WT_PATH + all port vars
  post_create: .clone-tree/hooks/post-create.sh   # e.g. OAuth URI SQL patch, .idea patching
  pre_remove:  .clone-tree/hooks/pre-remove.sh
```

Template vars available everywhere: `{name}`, `{slot}`, `{dns}`, `{repo}`,
`{projects_dir}`, plus every port var.

### 3.2 Commands (parity with the bash tool)

| Command | Notes |
|---------|-------|
| `ct create <name> [--branch b] [--start]` | slot alloc → worktree → .env → files → hooks → rollback on any failure (`defer`) |
| `ct remove <name\|slot>` | compose down -v, volumes, data dirs, worktree, slot, hosts entry, pre_remove hook |
| `ct list` | table: slot, name, branch, running containers, key ports |
| `ct start / stop <name\|slot>` | compose up -d / down |
| `ct ports / hosts [name]` | tables from config, not hardcoded prints |
| `ct exec <name> -- <cmd>` | run anything in the worktree dir (replaces `open`/`claude` — generic) |
| `ct doctor` | validate config, literal (non-`${VAR}`) port bindings, busy ports, orphan slots |
| `ct completion` | cobra-generated |

**UX rule — mimic `tagpay-worktree.sh` output rendering, always.** Reference:
`cmd_list` in `TagPay/devtools/tagpay-worktree.sh` (~l.689). Table spec: leading
blank line; 2-space left margin; 2 spaces between columns; left-aligned; column
width = max(header, longest value); **bold** headers; `─` underline per column at
exact column width; trailing blank line. First column = **Slot** (`-` in bare mode,
real slot once the registry exists), sorted by slot. Any future tabular output
(`ports`, `hosts`) follows the same spec.

### 3.3 Core packages

```
cmd/            cobra commands (thin)
internal/config loading + validation + templating
internal/slots  registry, allocation, busy-port probe
internal/ports  offset math
internal/provision  create/remove pipelines, rollback via defer-stack
internal/fsops  copy / hardlink / symlink / CoW-clone (platform detection)
internal/hosts  /etc/hosts add/remove (sudo)
```

## 4. What clone-tree deliberately does NOT do

- ❌ Parse or rewrite compose/YAML files — consumer repos must be `${VAR}`-parameterized (one-time migration, guided by `ct doctor`)
- ❌ Database seeding/migrations — that's the repo's job (hook if needed)
- ❌ Machine provisioning (installing docker, go, certs) — not this tool's job
- ❌ Windows (macOS + Linux only, v1)

## 5. Milestones

| # | Deliverable | Acceptance |
|---|------------|------------|
| M1 | **worktree core = v1**: `create/remove/list/exec`, works on any git repo | ✅ done — `ct create x && ct list && ct remove x` with no `.clone-tree/` |
| M2 | config load/validate + **auto-scaffold** (compose port scan, gitignored-files scan), slots registry, real slot column in `list`, ports, `.env` gen | first run on a config-less repo generates a sane `.clone-tree/config.yaml`; create+remove round-trip on a fixture repo |
| M3 | fsops: copy/hardlink/symlink_siblings/CoW clone with platform fallbacks | ✅ done — fixture with fake datadir; APFS clone verified on macOS |
| M4 | hooks, /etc/hosts, rollback pipeline, `start/stop/ports/hosts` | ✅ done — failing post_create hook rolls back slot+worktree+CoW+hosts (tested) |
| M5 | **TagPay adoption**: write `.clone-tree/config.yaml` + port OAuth/.idea logic into `post-create.sh`; shim `tagpay-worktree.sh` → `ct` | existing worktrees still listed; new worktree boots, client portal login works |
| M6 | `ct doctor`, completions, README with `go install` instructions | ✅ done — `ct doctor` checks config/literal ports/busy ports/orphan slots/hooks/dns, exit 1 on any ✗; `ct completion` (cobra default) + README zsh one-liner; README Install section verified against go.mod's real module path |

**Doc rule — README.md is the concise human-readable doc and MUST be updated in the
same iteration as any behavior change** (new command/flag, file location, config
key, generated output). It documents only what is implemented; roadmap stays here.

M5 is the real test: **if TagPay's script doesn't shrink to config + a ~60-line hook,
the abstraction is wrong — fix the core, don't add TagPay-isms to it.**

## 6. Open questions

1. Slot registry location: per-repo `.slots` file (current) vs global `~/.clone-tree/state` — global would let `ct list` span repos, but complicates paths. → start per-repo.
2. ~~`!override` / compose ≥ 2.24.4~~ — moot, no overlay file anymore.
3. Distribution: `go install` from repo (clone + `go install ./cmd/ct`, or direct `@latest`). Resolved.
4. Name collision: `ct` is short but check PATH conflicts; fallback full name `clone-tree`.
