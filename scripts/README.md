# scripts

Repository tooling that is not part of the Go build.

## Git hooks (`scripts/hooks`)

Install once per clone:

```bash
scripts/install-hooks.sh
```

That points `core.hooksPath` at `scripts/hooks`, so the committed hooks apply to
this clone instead of the per-clone, never-versioned `.git/hooks`.

### `pre-push` — no tag before main

Refuses a tag push whose tagged commit is not already on the remote's `main`.
The v0.4.0 release pushed its tag before `main` had been pushed, so the tag
reached the remote pointing at a commit remote `main` did not contain. The hook
turns the correct ordering into a hard rule:

```bash
git push <remote> main      # first, and let it land
git push <remote> <tag>     # then the tag
```

Details:

- A push that updates `main` *and* the tag in the same command is allowed (main
  is on the remote as part of that push).
- A tag whose commit is already reachable from remote `main` is allowed.
- Tag deletion is always allowed.
- Annotated tags are peeled to their commit before the check.
- If the remote cannot be queried for `main` (network), the hook warns to stderr
  and allows the push.
- Deliberate override: `git push --no-verify <remote> <tag>`.

### Verifying the hook

```bash
scripts/hooks/pre_push_test.sh
```

Runs the full simulated release sequence (push main, push tag, tag-first refusal,
main+tag together, annotated tag, deletion) against a throwaway bare remote and
fails if the hook's verdicts are wrong.
