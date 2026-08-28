# ct — clone-tree

Parallel git worktrees of one repo, each with its own ports via a generated `.env`.

## Install

```sh
go install github.com/luca-cattaneo/clone-tree/cmd/ct@latest   # needs Go, ~/go/bin on PATH
```

or clone the repo and then 
```sh
go install ./cmd/ct
export PATH="$HOME/go/bin:$PATH"
```

## Commands

| Command | Does |
|---|---|
| `ct create <name> [--branch b]` | slot → `git worktree add` → `.env` → `files:` (see below). Any failure rolls everything back. Branch defaults to `<name>`, created from HEAD if missing. |
| `ct remove <name\|slot>` | removes worktree, its `clone_cow` dirs, frees slot. `--force` defaults to `true` (the generated `.env` blocks git's safe remove). Slot 0 = main repo, not removable. |
| `ct list` | slot / name / branch / path. Main = slot 0. Unregistered worktree = `-`. |
| `ct exec <name> -- <cmd>` | run `<cmd>` in the worktree dir, exit code propagated. |

Global flag: `--config <path>` (skips lookup + scaffold).

## Where things go

```text
<repo>/.clone-tree/config.yaml   # config, generated on first run (ct then stops) — review & commit
../<repo>-worktrees/
  .slots                         # name:slot registry
  <sibling>/ -> ../<sibling>     # files.symlink_siblings (shared, never removed)
  <name>/                        # the worktree
    .env                         # main .env inherited (minus overridden keys) + a "ct (slot N)" block
    …                            # + files.copy / files.hardlink
<clone_cow dst>                  # e.g. ../docker_data/db_<name>, deleted by remove
```

## Config — generated if absent

When missing, ct writes it, prints a review checklist and **exits without running the command**.
Discovers the compose file list from `<repo>/.env`'s `COMPOSE_FILE` (`:`-separated), else
the first of `compose.yaml|compose.yml|docker-compose.yml|docker-compose.yaml` found plus
its matching `.override.*`. Ports come from `docker compose config --no-interpolate`
(merges `!override`/COMPOSE_FILE like a real `up` would); if docker is unavailable, ct
falls back to its own yaml.v3 merge of the same files.

Each `[ip:]host:container[/proto]` binding resolves to one `ports:` entry:

| Binding | name | base |
|---|---|---|
| `${VAR:-d}` / `${VAR-d}` | `VAR` | `.env[VAR]` ?? `d` |
| `${VAR}` (no default) | `VAR` | `.env[VAR]`; missing → `null` + a `# set me` comment |

A literal binding (e.g. `8443:443`, no `${VAR}`) gets **no** `ports:` entry and **no**
`.env` var — ct never rewrites compose YAML, so it can't be made slot-aware. Instead it
surfaces as a comment only, one per service, grouping every literal port on that service:
`# service <svc> cannot be replicated: non-parameterized host port 8443, 8444`.

Then base/step: `step` is `10` for every var, `base` is the original port (or `+10000` when
it's < 1024, a well-known port a proxy maps in from), and a collision pass multiplies a
var's step ×10 (up to 3 times, else a `# collision` comment) whenever its slots 1..max_slots
hit another var's base/slots **or a literal (non-parameterized) host port** — permanent
obstacles since both main and every worktree bind them identically; literal ports are never
bumped themselves, they're not entries.

`files:` is always the live, empty `{}` — every suggestion (dep dirs → `# hardlink:`,
`.idea`/`.vscode`/etc → their own `# ide:` bucket, everything else → `# copy:`) is a
comment underneath for a human to uncomment into a real entry. Paths collapse to the
topmost gitignored dir (`data/`, not `data/docker/x.bin`).

```yaml
version: 1
worktrees_dir: ../myrepo-worktrees             # {repo} (= repo dir name) also works
max_slots: 9
ports:
  DB_PORT: {base: 3306, step: 10}         # slot 2 → 3326
env:
  SERVER_NAME: "app-{name}"               # templated: {name} {slot} {repo} {projects_dir} {dns} + port vars
files:
  ide:       [.idea/]                     # same semantics as copy; own key since IDE dirs usually need per-worktree patching (post_create hook)
  copy:      [conf/local.php]             # relative to repo root, recursive; missing → warn+skip
  hardlink:  [vendor/]                    # same, but hardlinks; cross-device → symlinks the whole dir
  clone_cow: [{src: "{projects_dir}/docker_data/db", dst: "{projects_dir}/docker_data/db_{name}"}]
                                          # cp -Rc (APFS) / --reflink (Linux), fallback copy; dst must not exist
  symlink_siblings: [OtherRepo, docker_data]   # <worktrees_dir>/X -> <projects_dir>/X
```

`ct create` probes every configured port for the candidate slot (bind + connect) before
touching anything; a busy port aborts with nothing provisioned.

## Not yet

Hooks, DNS/hosts, `start`/`stop`, `doctor` — see `PLAN.md`.
