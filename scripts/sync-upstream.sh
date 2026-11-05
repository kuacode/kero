#!/usr/bin/env bash
# Sync this fork's dev branch with upstream (egoist/kero) and run the CI
# gates. See FORKING.md for the strategy this script implements.
#
# Usage:
#   scripts/sync-upstream.sh            # fetch, merge, gate, prompt to push
#   scripts/sync-upstream.sh --push     # as above, and push without asking
#   scripts/sync-upstream.sh --no-gate  # fetch and merge, skip vet/test/build
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

UPSTREAM=origin
FORK=kuacode
DEV=dev
PUSH=0
GATE=1

for arg in "$@"; do
  case "$arg" in
    --push) PUSH=1 ;;
    --no-gate) GATE=0 ;;
    -h|--help)
      sed -n '2,8p' "$0"
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

say() { printf '\n== %s\n' "$*"; }

# The script assumes both remotes are configured. Fail loudly if not.
for r in "$UPSTREAM" "$FORK"; do
  if ! git remote get-url "$r" >/dev/null 2>&1; then
    echo "remote '$r' is not configured" >&2
    exit 1
  fi
done

say "Working tree"
if [ -n "$(git status --porcelain)" ]; then
  echo "working tree is dirty; commit or stash before syncing" >&2
  git status --short >&2
  exit 1
fi
echo "clean"

say "Fetch $UPSTREAM and $FORK"
git fetch "$UPSTREAM" --prune --tags
git fetch "$FORK" --prune

say "Fast-forward local main to $UPSTREAM/main"
current=$(git rev-parse --abbrev-ref HEAD)
git checkout main
git merge --ff-only "$UPSTREAM/main"

say "Merge $UPSTREAM/main into $DEV"
git checkout "$DEV"
if git merge --no-edit "$UPSTREAM/main"; then
  echo "merge clean"
else
  echo "merge conflicts; resolve them, then:" >&2
  echo "  git add -A && git commit --no-edit" >&2
  echo "  $0 --push   # to run gates and push" >&2
  exit 1
fi

if [ "$GATE" -eq 1 ]; then
  say "gofmt (report only; commit changes as their own commit if any)"
  gofmt -l . || true

  say "go vet ./..."
  go vet ./...

  say "go test ./internal/..."
  go test ./internal/...

  say "Cross-platform build"
  for os in linux darwin windows; do
    GOOS="$os" go build ./...
  done
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/kero-server
fi

say "State"
git log --oneline -5
echo
git status --short --branch

if [ "$PUSH" -eq 1 ]; then
  say "Push $DEV to $FORK"
  git push "$FORK" "$DEV"
else
  echo
  printf 'Push %s to %s now? [yN] ' "$DEV" "$FORK"
  read -r answer
  case "$answer" in
    y|Y) git push "$FORK" "$DEV" ;;
    *) echo "skipped; push with: git push $FORK $DEV" ;;
  esac
fi

git checkout "$current" >/dev/null 2>&1 || true
