# Maintaining this fork of Kero

This repository is a fork of [egoist/kero](https://github.com/egoist/kero)
("upstream"). We track upstream closely and add our own features on top.
This document explains how to do that without the fork drifting into an
unmaintainable state.

Two remotes are configured:

- `origin` — https://github.com/egoist/kero.git — upstream, read-only for us
- `kuacode` — https://github.com/kuacode/kero.git — our fork, where `dev` is pushed

## Branch shape

- **`dev` is the source of truth.** All fork work lands here. It is never
  rebased onto upstream and never fast-forwarded to upstream; it only ever
  *merges* upstream in. This keeps our history intact and lets us re-resolve
  the same hotspot once, not once per rebase.
- **`main` mirrors upstream.** Keep it a pure tracking branch of
  `origin/main`. Do not commit to it. It exists so a plain `git checkout
  main && git pull` shows "what's new upstream".
- **Feature branches** (`feat/*`, `fix/*`) are short-lived. Branch off
  `dev`, merge back into `dev` when the work is done, then delete. Do not
  send fork-only branches to upstream.
- `kuacode/main` is allowed to lag upstream — we do not maintain it. If you
  want it current, fast-forward it to `origin/main`; never merge `dev` into
  it.

## Staying in sync

Run `scripts/sync-upstream.sh` to do all of this in one step. Manually:

```sh
# 1. Fetch both remotes.
git fetch origin --prune
git fetch kuacode --prune

# 2. Fast-forward local main so it tracks upstream exactly.
git checkout main
git merge --ff-only origin/main

# 3. Merge upstream into dev. Use a merge commit; never rebase.
git checkout dev
git merge origin/main --no-edit
```

If step 3 conflicts, resolve them, then:

```sh
git add -A
git commit --no-edit          # finishes the merge commit
gofmt -w .                    # gofmt is a separate commit, like upstream
go vet ./...
go test ./internal/...
GOOS=windows go build ./...   # cheap smoke check on the Windows-only chrome
```

Then push:

```sh
git push kuacode dev
```

Sync cadence: at least weekly, ideally every time upstream merges a PR you
care about. Small, frequent merges keep conflicts small.

## Conflict hotspots

These files conflict on almost every sync. Expect them and resolve them the
same way every time:

- **`locales/en.json`, `locales/ja.json`, `locales/zh-Hans.json`,
  `locales/catalog_gen.go`** — the most recurrent set (three of the last
  nine merges). Upstream regenerates strings; so do we. Take *both* sides:
  keep upstream's new keys, then re-add ours, then run whatever regenerates
  `catalog_gen.go` (see `mygo.json`'s `i18n` block) rather than merging it
  by hand.
- **`CHANGELOG.md`** — upstream keeps a `## Unreleased` section that fills
  up before each release; `dev` drops it. On conflict, take upstream's
  `## Unreleased` block *plus* our sections. Do not delete entries we don't
  recognize — upstream may have shipped a fix we want to read later.
- **`mygo.json`** — single-line version-bump conflicts. Take upstream's
  version; we do not cut releases from `dev`.
- **`go.mod`, `go.sum`** — both sides bump dependencies. On conflict, take
  upstream's versions as the base, then re-add any dependency only the
  fork needs. Never hand-edit `go.sum`; run `go mod tidy` after resolving
  `go.mod` to regenerate it.
- **`platform.go`, `view.go`, `settings_view.go`, `side.go`, `picker.go`**
  — the fork has substantial UI here (window chrome, picker quick jumps,
  sidebar toggles). Expect real merge work when upstream touches the same
  area. Prefer keeping upstream's structure and re-applying our deltas on
  top, so the diff between `dev` and upstream stays as small as possible.
- **`awake.go`, `app.go`, `internal/settings/config.go`** — keep-awake is
  implemented in both; ours adds a toolbar toggle. Keep upstream's API
  shape and layer our toggle over it.

## Adding fork-only features

Two rules keep fork additions from rotting:

1. **New files over edits.** When a feature can live in its own file, put
   it there. `internal/daemon/fs_roots_unix.go` and
   `internal/daemon/fs_roots_windows.go` are the model: build-tag-split,
   self-contained, zero upstream edits. These files essentially never
   conflict.
2. **Small, named deltas when editing is unavoidable.** When we must touch
   an upstream file (`platform.go`, `picker.go`, …), keep the fork's
   additions in clearly-named functions or types (`controlsMode`,
   `settingsWindowControls`, `mainControlsAny`) rather than sprinkling
   logic inline. A reviewer should be able to read the file's diff against
   upstream and see only our named additions.

For new strings, add them to all three `locales/*.json` files and
regenerate `locales/catalog_gen.go` — same as upstream.

## Gates before pushing

CI (`.github/workflows/ci.yml`) runs these on upstream PRs and on pushes to
upstream `main` — it does **not** run on `kuacode/dev`. Run them locally so
`dev` is always in a shippable state:

- `gofmt -w .` (as its own commit when it touches files)
- `go vet ./...`
- `go test ./internal/...`
- Cross-platform build: `for os in linux darwin windows; do GOOS=$os go
  build ./...; done`

There is no Makefile, husky, or lefthook config in this repo — these are
manual. `scripts/sync-upstream.sh` runs them for you after a merge.

## Releasing

We do not cut releases from this fork. `mygo.json`'s `version` and
`CHANGELOG.md`'s released sections are upstream's to manage; we merge them
in. If you need to ship a fork build, tag it on `dev` with a suffix (for
example `v2.1.33-kuacode.1`) so it can never be confused with an upstream
tag.

See [RELEASING.md](RELEASING.md) for how upstream does it, and
[CLAUDE.md](CLAUDE.md) for the short version agents read.
