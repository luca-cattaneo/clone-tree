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
| `ct create <name> [--branch b]` | slot → `git worktree add` → `.env` → `files:` → `hosts:` entry (if `dns_pattern` set) → `post_create` hook (see below). Any failure rolls everything back. Branch defaults to `<name>`, created from HEAD if missing. |
| `ct remove <name\|slot>` | `pre_remove` hook (if configured) → `docker compose down -v` (best-effort if the worktree has no compose file, else propagated) → removes worktree, its `clone_cow` dirs, its hosts entry, frees slot. `--force` defaults to `true` (the generated `.env` blocks git's safe remove). Slot 0 = main repo, not removable. |
| `ct list` | slot / name / branch / path / running container count. Plus a `VAR=value` column for the first sorted `ports:` var, when configured. Main = slot 0. Unregistered worktree = `-`. |
| `ct exec <name> -- <cmd>` | run `<cmd>` in the worktree dir, exit code propagated. |
| `ct start <name\|slot>` / `ct stop <name\|slot>` | `docker compose up -d` / `down` in the worktree dir, `COMPOSE_PROJECT_NAME=<repo>-<name>`. Requires a `.clone-tree` config. |
| `ct ports [name|slot]` | no arg: table of every registered worktree (+ main at slot 0) × every configured port var. With `name` or slot: `Var`/`Value` table for that worktree. |
| `ct hosts [name|slot]` | table of every registered worktree: Slot, Name, DNS (expanded `dns_pattern`), and whether it's currently present in the hosts file (`✓` clone-tree-owned, `✓ (unmanaged)` present but not ct-managed, `-` absent). With `name`/slot and a non-empty `urls:`, also prints a Service/URL table underneath (see Config below). |

Global flag: `--config <path>` (skips lookup + scaffold).

Only `ct create` ever auto-scaffolds `.clone-tree/config.yaml`. Every other command reads an
existing config only: `ct list` still works with no config at all (bare mode — plain worktree
listing, no ports/urls columns); `ct ports`/`ct hosts`/`ct start`/`ct stop`/`ct remove`/`ct exec`
error cleanly instead ("no .clone-tree/config.yaml — run `ct create` to scaffold one").

## Shell completion

```sh
echo 'source <(ct completion zsh)' >> ~/.zshrc
```

`remove`, `start`, `stop`, `exec`, `ports`, and `hosts` complete their `<name|slot>` argument
from the slot registry (silently no completions if there's no config yet, or any other error).

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

`urls:` is likewise always the live, empty `{}` — one commented `label: "template"`
suggestion per host-exposed `ports:` var (a literal, non-parameterized binding never gets
one, same as it never gets a `ports:` entry): label is the owning compose service name
(deduped `service`, `service-2`, … when one service exposes more than one port var), scheme
is `https` when the container port is `443`/`8443` else `http`, host is `{dns}` when
`dns_pattern` is non-empty else `localhost`.

```yaml
version: 1
worktrees_dir: ../myrepo-worktrees             # {repo} (= repo dir name) also works
max_slots: 9
ports:
  DB_PORT: {base: 3306, step: 10}         # slot 2 → 3326
urls:
  Web Client: "https://{dns}:{PROXY_HTTPS_PORT}/"   # rendered by `ct hosts <name>` as a Service/URL table
env:
  SERVER_NAME: "app-{name}"               # templated: {name} {name_lower} {slot} {dns} {repo} {projects_dir} + port vars
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

## Hooks

Optional repo-local executables, declared under `hooks:` in `config.yaml`, paths relative
to the repo root:

```yaml
hooks:
  post_create: .clone-tree/hooks/post-create.sh   # runs after files:/hosts:, before create returns
  pre_remove:  .clone-tree/hooks/pre-remove.sh     # runs first in `ct remove`, before compose down/deletion
```

Both receive the instance's env contract: `CT_NAME`, `CT_SLOT`, `CT_DNS`, `CT_WT_PATH`, plus
one `VAR=value` entry per configured port var. `cwd` is the worktree dir. A missing hook
path is an error; an empty (unset) one is a no-op. A `post_create` failure fails `ct create`
and triggers rollback (see below); a `pre_remove` failure aborts `ct remove` before anything
is torn down.

## DNS / `/etc/hosts`

When `dns_pattern` is non-empty, `ct create` appends a `127.0.0.1 <dns>  # clone-tree:<name>`
line to `/etc/hosts` (writing to a root-owned file falls back to `sudo tee`), and `ct remove`
drops it. If `dns` is already bound by an *unmanaged* line (no `# clone-tree:` marker — a
hand-added entry, or a leftover from before the worktree was ct-managed), `ct create` takes
ownership of that line in place instead of appending a duplicate binding. `ct hosts [name]`
lists every registered worktree's DNS and whether it's currently present: `✓` when the line
is clone-tree-owned, `✓ (unmanaged)` when `dns` resolves via some other line, `-` when it's
absent entirely. Only clone-tree-owned lines (marked by the trailing comment) are ever
written to — every other line in the file is preserved verbatim and in place.

## Rollback

`ct create` pushes an undo for every side effect onto a LIFO stack as it succeeds: slot
release, worktree removal (which also removes the generated `.env` and every `files.copy`
/ `files.ide` / `files.hardlink` entry, since they live inside the worktree), each
`files.clone_cow` destination, and the `/etc/hosts` entry. Any failure — including the
`post_create` hook — unwinds that stack in reverse order, so a half-provisioned instance
never survives a failed create. `files.symlink_siblings` is the one exception: it's shared
across every worktree in `worktrees_dir`, so it's created idempotently but never undone —
removing it on one worktree's rollback would break every other worktree's sibling mount.

## Not yet

`doctor` — see `PLAN.md`.
