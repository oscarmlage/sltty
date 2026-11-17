#!/usr/bin/env bash
# next-version.sh [REV]: the tag the next release of REV (HEAD) takes. The
# last vX.Y.Z tag goes up a patch, a minor with "[minor]" or a major with
# "[major]" in a commit message since then; v0.1.0 when there is none.
# Prints nothing when REV is released already or its message asks
# "[skip release]".
set -euo pipefail

rev=${1:-HEAD}
if git tag --points-at "$rev" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  exit 0 # released already (a re-run)
fi
if git log -1 --format=%B "$rev" | grep -qF '[skip release]'; then
  exit 0
fi
last=$(git tag --list 'v*' --merged "$rev" --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true)
if [[ -z $last ]]; then
  echo v0.1.0
  exit 0
fi
IFS=. read -r major minor patch <<<"${last#v}"
log=$(git log --format=%B "$last..$rev")
if grep -qF '[major]' <<<"$log"; then
  echo "v$((major + 1)).0.0"
elif grep -qF '[minor]' <<<"$log"; then
  echo "v$major.$((minor + 1)).0"
else
  echo "v$major.$minor.$((patch + 1))"
fi
