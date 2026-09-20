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
# origin. Refuse if origin/main has commits we lack (needs a pull). We always
# push main ourselves below, because the release now makes a commit of its own.
git fetch -q origin main
[ -z "$(git rev-list HEAD..origin/main)" ] ||
	die "origin/main has commits you don't have locally — pull/rebase first"

if [ "${SKIP_TESTS:-}" != "1" ]; then
	echo "running tests..."
	go test ./... >/dev/null || die "tests failed — not tagging a broken build (SKIP_TESTS=1 to override)"
fi

# The Claude Code plugin carries the dbn-review skill, and Claude Code only
# offers an update when plugin.json's version changes — so a release that leaves
# it alone ships a skill nobody receives. It is set here, in plugin.json only:
# the docs are explicit that plugin.json silently wins over marketplace.json,
# so setting both would just be two places to disagree.
plugin_manifest=".claude-plugin/plugin.json"
[ -f "$plugin_manifest" ] || die "${plugin_manifest} not found — is this the dbn repository?"
plugin_version=${version#v}

printf 'About to set the plugin version to %s, commit it as "Release %s", push main,\n' "$plugin_version" "$version"
printf 'then tag %s at that commit and push the tag, triggering the release build.\n' "$version"
printf 'Continue? [y/N] '
read -r reply
case "$reply" in
y | Y) ;;
*) die "aborted" ;;
esac

# Rewrite the first "version" key in the manifest. plugin.json is a flat,
# hand-maintained object with exactly one, so first-match is the right match;
# were it ever to gain a nested "version", this would need a real JSON tool.
# The result is read back and checked, because a silently unchanged manifest is
# the whole bug this guards against.
#
# The temp file goes outside the repository: one left behind inside it would be
# untracked, and the *next* release would then die on its clean-tree check.
tmp_manifest=$(mktemp) || die "could not create a temporary file"
trap 'rm -f "$tmp_manifest"' EXIT INT TERM

sed 's/\("version"[[:space:]]*:[[:space:]]*\)"[^"]*"/\1"'"$plugin_version"'"/' \
	"$plugin_manifest" >"$tmp_manifest" || die "could not rewrite ${plugin_manifest}"
cat "$tmp_manifest" >"$plugin_manifest" || die "could not write ${plugin_manifest}"

written=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugin_manifest" | head -n 1)
[ "$written" = "$plugin_version" ] ||
	die "could not set the plugin version in ${plugin_manifest} (it reads '${written}')"

# Nothing to commit when the manifest already carried this version, which
# happens when an earlier attempt got this far and then failed. Re-running must
# still tag and push rather than dying on an empty commit.
if git diff --quiet -- "$plugin_manifest"; then
	echo "plugin version is already ${plugin_version}; nothing to commit"
else
	echo "setting the plugin version to ${plugin_version}..."
	git add "$plugin_manifest"
	git commit -qm "Release ${version}"
fi

echo "pushing main..."
git push origin main

git tag -a "$version" -m "Release ${version}"
git push origin "$version"

printf '\npushed %s — watch the release build at:\n' "$version"
printf '  https://github.com/probertson/diff-by-numbers/actions\n'
