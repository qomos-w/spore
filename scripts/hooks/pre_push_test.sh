#!/usr/bin/env bash
# Simulated release sequence for scripts/hooks/pre-push.
#
# Reproduces the v0.4.0 tag-first incident against a throwaway bare remote and
# asserts the hook's verdicts:
#
#   1. push main                        -> allowed
#   2. push a tag on published main     -> allowed
#   3. push a tag the remote lacks      -> REFUSED (the incident)
#   4. push main, then the tag          -> allowed
#   5. push main and tag in one command -> allowed
#   6. push an annotated tag            -> allowed
#   7. delete a tag                     -> allowed
#
# Usage: scripts/hooks/pre_push_test.sh

set -euo pipefail

hook_dir=$(cd "$(dirname "$0")" && pwd)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

say() { printf '%s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# ---- throwaway remote + work repo ---------------------------------------
git init -q --bare "$work/remote.git"

git init -q "$work/work"
git -C "$work/work" symbolic-ref HEAD refs/heads/main

cd "$work/work"
git config user.email pre-push-test@example.invalid
git config user.name "pre-push test"
git config commit.gpgsign false
# Use the repo's versioned hooks, overriding any global core.hooksPath.
git config core.hooksPath "$hook_dir"

printf 'one\n' > f.txt
git add f.txt
git commit -qm "initial"
git remote add origin "$work/remote.git"

# ---- 1. push main --------------------------------------------------------
git push -q origin main || fail "pushing main should succeed"
say "ok: push main"

# ---- 2. tag on published main -------------------------------------------
git tag v0.1.0
git push -q origin v0.1.0 || fail "pushing a tag on remote main should succeed"
say "ok: push tag on published main"

# ---- 3. tag-first: the incident -----------------------------------------
printf 'two\n' >> f.txt
git commit -qam "second"
git tag v0.2.0

err=$(mktemp)
if git push origin v0.2.0 >"$err" 2>&1; then
	cat "$err" >&2
	fail "hook let a tag-first push through (v0.4.0 incident reproduced)"
fi
grep -q "REFUSED" "$err" || { cat "$err" >&2; fail "expected a REFUSED diagnostic"; }
if git ls-remote --tags origin refs/tags/v0.2.0 | grep -q .; then
	fail "the refused tag v0.2.0 still reached the remote"
fi
say "ok: tag-first push refused"

# ---- 4. push main, then the tag -----------------------------------------
git push -q origin main || fail "pushing main should succeed"
git push -q origin v0.2.0 || fail "pushing the tag after main should succeed"
git ls-remote --tags origin refs/tags/v0.2.0 | grep -q . || fail "tag v0.2.0 missing on remote"
say "ok: push main then tag"

# ---- 5. main + tag in one push ------------------------------------------
printf 'three\n' >> f.txt
git commit -qam "third"
git tag v0.3.0
git push -q origin main v0.3.0 || fail "pushing main and tag together should succeed"
say "ok: push main and tag together"

# ---- 6. annotated tag on already-published main -------------------------
git tag -a v0.4.0 -m "annotated"
git push -q origin v0.4.0 || fail "annotated tag on published main should succeed"
say "ok: push annotated tag"

# ---- 7. tag deletion -----------------------------------------------------
git push -q origin :refs/tags/v0.1.0 || fail "deleting a tag should succeed"
if git ls-remote --tags origin refs/tags/v0.1.0 | grep -q .; then
	fail "tag v0.1.0 should have been deleted"
fi
say "ok: delete tag"

say "PASS: pre-push hook behaves correctly across the release sequence"
