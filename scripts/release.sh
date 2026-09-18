#!/bin/sh
# release.sh — cut a dbn release: validate, tag, and push a version tag, which
# triggers the GitHub release workflow (GoReleaser) that builds and publishes the
# binaries the installer downloads.
#
# Usage: scripts/release.sh vX.Y.Z
#        scripts/release.sh MAJOR|MINOR|PATCH   # bump the latest vX.Y.Z tag
#   SKIP_TESTS=1 scripts/release.sh vX.Y.Z   # skip the local test gate
set -eu

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage="usage: scripts/release.sh vX.Y.Z | MAJOR | MINOR | PATCH"
arg="${1:-}"
[ -n "$arg" ] || die "$usage"

# Work from the repo root, wherever the script was invoked from.
root=$(git rev-parse --show-toplevel) || die "not in a git repository"
cd "$root"

# A bump keyword resolves against the latest release tag. Fetch tags first, so a
# release cut from another clone is not bumped from twice.
case "$arg" in
MAJOR | MINOR | PATCH | major | minor | patch)
	git fetch -q --tags origin || die "could not fetch tags from origin"
	latest=$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)
	[ -n "$latest" ] || latest=v0.0.0
	IFS=. read -r major minor patch <<-EOF
		${latest#v}
	EOF
	case "$arg" in
	MAJOR | major) version="v$((major + 1)).0.0" ;;
	MINOR | minor) version="v${major}.$((minor + 1)).0" ;;
	*) version="v${major}.${minor}.$((patch + 1))" ;;
	esac
	printf 'latest release is %s; %s bump makes it %s\n' "$latest" "$arg" "$version"
	;;
*)
	version="$arg"
	echo "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
		die "version must look like vX.Y.Z, or be MAJOR, MINOR or PATCH (got: ${version})"
	;;
esac

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
