#!/bin/sh
# release.sh — cut a dbn release: validate, tag, and push a version tag, which
# triggers the GitHub release workflow (GoReleaser) that builds and publishes the
# binaries the installer downloads.
#
# Usage: scripts/release.sh vX.Y.Z
#   SKIP_TESTS=1 scripts/release.sh vX.Y.Z   # skip the local test gate
set -eu

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

version="${1:-}"
[ -n "$version" ] || die "usage: scripts/release.sh vX.Y.Z"
echo "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
	die "version must look like vX.Y.Z (got: ${version})"

# Work from the repo root, wherever the script was invoked from.
root=$(git rev-parse --show-toplevel) || die "not in a git repository"
cd "$root"

branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = "main" ] || die "releases are cut from main, but you are on '${branch}'"
[ -z "$(git status --porcelain)" ] || die "working tree is not clean — commit or stash first"

git rev-parse "$version" >/dev/null 2>&1 && die "tag ${version} already exists locally"
git ls-remote --exit-code --tags origin "refs/tags/${version}" >/dev/null 2>&1 &&
	die "tag ${version} already exists on origin"

# The workflow builds from the tagged commit on GitHub, so main must be on
# origin. Refuse if origin/main has commits we lack (needs a pull); push main
# ourselves if it is merely ahead, so this stays a one-command release.
git fetch -q origin main
[ -z "$(git rev-list HEAD..origin/main)" ] ||
	die "origin/main has commits you don't have locally — pull/rebase first"
push_main=no
[ -z "$(git rev-list origin/main..HEAD)" ] || push_main=yes

if [ "${SKIP_TESTS:-}" != "1" ]; then
	echo "running tests..."
	go test ./... >/dev/null || die "tests failed — not tagging a broken build (SKIP_TESTS=1 to override)"
fi

commit=$(git rev-parse --short HEAD)
if [ "$push_main" = yes ]; then
	printf 'About to push main, then tag %s at %s and push the tag, triggering the release build.\n' "$version" "$commit"
else
	printf 'About to tag %s at %s (main) and push it, triggering the release build.\n' "$version" "$commit"
fi
printf 'Continue? [y/N] '
read -r reply
case "$reply" in
y | Y) ;;
*) die "aborted" ;;
esac

if [ "$push_main" = yes ]; then
	echo "pushing main..."
	git push origin main
fi

git tag -a "$version" -m "Release ${version}"
git push origin "$version"

printf '\npushed %s — watch the release build at:\n' "$version"
printf '  https://github.com/probertson/diff-by-numbers/actions\n'
