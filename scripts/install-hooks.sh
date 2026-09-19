#!/bin/sh
#
# Install spore's versioned git hooks into the current clone by pointing
# core.hooksPath at scripts/hooks. Run once per clone.
#
# Unlike .git/hooks (never versioned, never shared), scripts/hooks is committed,
# so every clone gets the same pre-push policy by running this script.

set -e

root=$(git rev-parse --show-toplevel)
cd "$root"

git config core.hooksPath scripts/hooks

# Make hooks executable where the platform records the bit (no-op on Windows).
chmod +x scripts/hooks/pre-push scripts/hooks/pre_push_test.sh 2>/dev/null || true

echo "installed git hooks: core.hooksPath -> scripts/hooks"
echo "pre-push now refuses to publish a tag before its commit is on the remote's main"
echo "see scripts/README.md for details"
