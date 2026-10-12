#!/usr/bin/env bash
# PR 3595 / F6: the PR edits .github/config/wordlist.txt, which main deleted (#3845, spellcheck removed).
#
# Expected: every file the PR modifies still exists on main, so the PR merges cleanly.
# Actual on 0ce47b76d: wordlist.txt is gone on main -> modify/delete conflict.
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout MAIN_REF=upstream/main bash .review/pr-3595/F6-wordlist-removed-on-main.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
MAIN_REF="${MAIN_REF:-upstream/main}"
PR_HEAD="${PR_HEAD:-0ce47b76d7d7cc49756d463315e11894028b9db0}"
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }
git -C "$REPO_ROOT" rev-parse --verify --quiet "$MAIN_REF" >/dev/null || setup_fail "ref $MAIN_REF not found"
git -C "$REPO_ROOT" cat-file -e "$PR_HEAD^{commit}" 2>/dev/null || setup_fail "PR head $PR_HEAD not found"

f=.github/config/wordlist.txt
# Only relevant when the checkout under test carries the PR's change to the file.
if ! git -C "$REPO_ROOT" diff --quiet "$(git -C "$REPO_ROOT" merge-base "$PR_HEAD" "$MAIN_REF")" -- "$f" 2>/dev/null \
   && ! git -C "$REPO_ROOT" cat-file -e "$MAIN_REF:$f" 2>/dev/null; then
  echo "REPRODUCED: $f is modified here but deleted on $MAIN_REF:"
  git -C "$REPO_ROOT" log -1 --format='  %h %s' "$MAIN_REF" -- "$f"
  exit 1
fi
echo "OK: no edits to files deleted on $MAIN_REF"
