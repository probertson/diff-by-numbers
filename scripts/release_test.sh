#!/bin/sh
# release_test.sh — the test seam for scripts/release.sh.
#
# It drives the real release.sh against a throwaway repository with a real
# `origin`: a bare repo on disk, so pushes and tags are genuine git operations
# that go nowhere near GitHub. Nothing is stubbed but the tests the script would
# run (SKIP_TESTS=1), the confirmation prompt (fed on stdin) and the editor the
# release notes are written in (a script that writes canned notes).
#
# That lets us assert the release's external behaviour — the plugin version it
# writes, the commit it makes, which commit the tag lands on, the notes the tag
# carries, and the state it leaves the tree in — without cutting a release.
#
# Pure POSIX sh; no bats. Run directly (`sh scripts/release_test.sh`) or via
# `go test ./internal/releasescript`, which execs this file.
set -u

here=$(CDPATH= cd "$(dirname "$0")" && pwd)
RELEASE="$here/release.sh"

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() {
	printf 'FAIL - %s\n' "$1"
	[ $# -ge 2 ] && printf '       %s\n' "$2"
	fails=$((fails + 1))
}

# check_equal NAME WANT GOT
check_equal() {
	if [ "$2" = "$3" ]; then
		pass "$1"
	else
		fail "$1" "want [$2], got [$3]"
	fi
}

# Every sandbox this run created, removed on the way out however we leave.
# Kept newline-separated rather than space-separated so a TMPDIR containing a
# space cannot split one path into two for rm -rf.
sandboxes=""
cleanup() {
	[ -n "$sandboxes" ] || return 0
	echo "$sandboxes" | while IFS= read -r sandbox; do
		[ -n "$sandbox" ] && rm -rf "$sandbox"
	done
	return 0
}
trap cleanup EXIT INT TERM

# new_repo — a throwaway clone with a bare origin, seeded with the two files a
# release touches. Sets WORK and ORIGIN.
#
# Also writes the stand-in editor: it keeps a copy of the draft it was handed in
# $SAND/draft, then replaces the draft with whatever is in $SAND/notes.
new_repo() {
	SAND=$(mktemp -d)
	sandboxes="${sandboxes}${SAND}
"
	cat >"$SAND/editor.sh" <<-EDITOR
		cp "\$1" "$SAND/draft"
		cat "$SAND/notes" >"\$1"
	EDITOR
	printf '## Fixes\n\n- A fix worth telling people about\n' >"$SAND/notes"
	ORIGIN="$SAND/origin.git"
	WORK="$SAND/work"

	git init -q --bare "$ORIGIN"
	git init -q "$WORK"
	# Local config only: never read or write the developer's real git identity.
	git -C "$WORK" config user.name "Release Test"
	git -C "$WORK" config user.email "release-test@example.invalid"
	git -C "$WORK" config commit.gpgsign false
	git -C "$WORK" config tag.gpgsign false
	git -C "$WORK" remote add origin "$ORIGIN"

	mkdir -p "$WORK/.claude-plugin"
	cat >"$WORK/.claude-plugin/plugin.json" <<-'JSON'
		{
		  "name": "dbn",
		  "description": "Teach your agent to use diff-by-numbers.",
		  "version": "0.0.1",
		  "author": {
		    "name": "Paul Robertson"
		  }
		}
	JSON
	cat >"$WORK/.claude-plugin/marketplace.json" <<-'JSON'
		{
		  "name": "diff-by-numbers",
		  "plugins": [{ "name": "dbn", "source": "./" }]
		}
	JSON
	echo "a project" >"$WORK/README.md"

	git -C "$WORK" add -A
	git -C "$WORK" commit -qm "Initial commit"
	git -C "$WORK" branch -M main
	git -C "$WORK" push -q -u origin main
}

# cut_release VERSION — run the real release script, answering the prompt with "y".
# Captures combined output in RELEASE_OUT and the exit status in RELEASE_STATUS.
cut_release() {
	RELEASE_OUT=$(cd "$WORK" && printf 'y\n' |
		SKIP_TESTS=1 GIT_EDITOR="sh $SAND/editor.sh" sh "$RELEASE" "$1" 2>&1)
	RELEASE_STATUS=$?
}

# version_in FILE — the "version" field, without depending on jq being installed.
#
# Deliberately NOT release.sh's own sed expression: a test that verifies a write
# using the writer's own extraction would pass on both sides of the same bug.
# This reaches the value by a different route — isolate the line, drop
# everything up to the colon, then keep what is between the quotes.
version_in() {
	grep '"version"' "$1" | head -n 1 | cut -d: -f2 | tr -d ' ",'
}

# drafted_commits — the commit lines in the draft the editor was handed, up to
# the scissors line.
drafted_commits() {
	awk '/>8/ { exit } /^- / { print }' "$SAND/draft"
}

# ---------------------------------------------------------------------------

new_repo
cut_release v1.2.3

if [ "$RELEASE_STATUS" -ne 0 ]; then
	fail "the release succeeds" "exit $RELEASE_STATUS: $RELEASE_OUT"
else
	pass "the release succeeds"
fi

check_equal "the plugin version is written without the leading v" \
	"1.2.3" "$(version_in "$WORK/.claude-plugin/plugin.json")"

check_equal "the release commit is the one the tag points at" \
	"$(git -C "$WORK" rev-parse HEAD)" "$(git -C "$WORK" rev-list -n 1 v1.2.3)"

check_equal "the release commit says what it is" \
	"Release v1.2.3" "$(git -C "$WORK" log -1 --pretty=%s)"

check_equal "the working tree is clean afterwards" \
	"" "$(git -C "$WORK" status --porcelain)"

check_equal "main is pushed, so the tagged commit is on origin" \
	"$(git -C "$WORK" rev-parse HEAD)" "$(git -C "$WORK" rev-parse origin/main)"

check_equal "the tag is pushed" \
	"$(git -C "$WORK" rev-parse HEAD)" "$(git -C "$ORIGIN" rev-list -n 1 v1.2.3 2>/dev/null)"

# The docs say plugin.json silently wins over marketplace.json, so the release
# must set the version in exactly one place and leave the other alone.
check_equal "marketplace.json is left byte-for-byte alone" \
	"$(git -C "$WORK" show "$(git -C "$WORK" rev-list --max-parents=0 HEAD):.claude-plugin/marketplace.json")" \
	"$(cat "$WORK/.claude-plugin/marketplace.json")"

check_equal "the tag's subject says what it is" \
	"Release v1.2.3" "$(git -C "$WORK" tag -l --format='%(contents:subject)' v1.2.3)"

# The body is what GoReleaser publishes, so it has to be the notes exactly: the
# Markdown heading kept, the scissors line and instructions gone.
check_equal "the tag's body is the release notes as written" \
	"$(cat "$SAND/notes")" "$(git -C "$WORK" tag -l --format='%(contents:body)' v1.2.3)"

check_equal "the first release's draft lists every commit" \
	"- Initial commit" "$(drafted_commits)"

# A bump keyword has to resolve against the tag just cut and carry the same
# behaviour, since that is how a release is normally cut.
echo "a feature" >"$WORK/feature.txt"
git -C "$WORK" add feature.txt
git -C "$WORK" commit -qm "Add a feature"
git -C "$WORK" push -q origin main
cut_release PATCH

if [ "$RELEASE_STATUS" -ne 0 ]; then
	fail "a PATCH bump succeeds" "exit $RELEASE_STATUS: $RELEASE_OUT"
else
	pass "a PATCH bump succeeds"
fi

check_equal "a PATCH bump resolves against the tag just cut" \
	"1.2.4" "$(version_in "$WORK/.claude-plugin/plugin.json")"

check_equal "the bumped release is tagged at its own commit" \
	"$(git -C "$WORK" rev-parse HEAD)" "$(git -C "$WORK" rev-list -n 1 v1.2.4)"

# Only what changed since the last release. The "Release v1.2.3" commit is left
# out without any filtering, because it is the commit the previous tag is on.
check_equal "the draft lists only the commits since the previous release" \
	"- Add a feature" "$(drafted_commits)"

# Empty notes are the way out of a release, so nothing may have happened yet.
new_repo
printf '\n  \n' >"$SAND/notes"
cut_release v3.0.0

if [ "$RELEASE_STATUS" -eq 0 ]; then
	fail "a release with empty notes is aborted" "it exited 0: $RELEASE_OUT"
else
	pass "a release with empty notes is aborted"
fi

check_equal "an aborted release makes no commit" \
	"Initial commit" "$(git -C "$WORK" log -1 --pretty=%s)"

check_equal "an aborted release leaves the tree clean" \
	"" "$(git -C "$WORK" status --porcelain)"

check_equal "an aborted release makes no tag" \
	"" "$(git -C "$WORK" tag -l v3.0.0)"

# Re-cutting a version whose plugin.json is already correct must not fail on an
# empty commit: the script has to notice there is nothing to write.
new_repo
# Not `sed -i`, which is not POSIX: rewrite through a temp file instead.
sed 's/"version": "0.0.1"/"version": "2.0.0"/' "$WORK/.claude-plugin/plugin.json" >"$SAND/manifest"
cat "$SAND/manifest" >"$WORK/.claude-plugin/plugin.json"
git -C "$WORK" commit -qam "Set the plugin version by hand"
git -C "$WORK" push -q origin main
cut_release v2.0.0

if [ "$RELEASE_STATUS" -ne 0 ]; then
	fail "a release whose plugin version is already right still succeeds" \
		"exit $RELEASE_STATUS: $RELEASE_OUT"
else
	pass "a release whose plugin version is already right still succeeds"
fi

check_equal "and it is still tagged" \
	"$(git -C "$WORK" rev-parse HEAD)" "$(git -C "$WORK" rev-list -n 1 v2.0.0)"

# The skill's "Requires dbn vX.Y.Z or later" line is kept by hand, so the
# release warns when it looks forgotten: the MCP schema changed since the last
# release and the line did not, or the line names a release that is not out.

# with_skill REQUIRES — seed the skill's minimum-version line and the MCP schema
# file, and commit them.
with_skill() {
	mkdir -p "$WORK/skills/dbn-review" "$WORK/internal/daemon"
	printf '# dbn-review\n\n**Requires dbn %s or later.**\n' "$1" >"$WORK/skills/dbn-review/SKILL.md"
	[ -f "$WORK/internal/daemon/wire.go" ] || echo "package daemon" >"$WORK/internal/daemon/wire.go"
	git -C "$WORK" add -A
	git -C "$WORK" commit -qm "Require dbn $1"
	git -C "$WORK" push -q origin main
}

# change_schema — edit the MCP schema file and commit it.
change_schema() {
	echo "// a new field" >>"$WORK/internal/daemon/wire.go"
	git -C "$WORK" commit -qam "Change the MCP schema"
	git -C "$WORK" push -q origin main
}

forgotten="the MCP schema (internal/daemon/wire.go) changed since"
unreleased="names a dbn that is not out yet"

new_repo
with_skill v1.0.0
cut_release v1.0.0
change_schema
cut_release PATCH

case "$RELEASE_OUT" in
*"$forgotten"*) pass "a schema change with the minimum version left alone is warned about" ;;
*) fail "a schema change with the minimum version left alone is warned about" "$RELEASE_OUT" ;;
esac
check_equal "the warning does not stop the release" "0" "$RELEASE_STATUS"

new_repo
with_skill v1.0.0
cut_release v1.0.0
change_schema
with_skill v1.1.0
cut_release v1.1.0

case "$RELEASE_OUT" in
*"$forgotten"* | *"$unreleased"*) fail "a schema change that raised the minimum version is not warned about" "$RELEASE_OUT" ;;
*) pass "a schema change that raised the minimum version is not warned about" ;;
esac

new_repo
with_skill v1.0.0
cut_release v1.0.0
echo "docs" >>"$WORK/README.md"
git -C "$WORK" commit -qam "Just docs"
git -C "$WORK" push -q origin main
cut_release PATCH

case "$RELEASE_OUT" in
*"$forgotten"*) fail "a release that leaves the schema alone is not warned about" "$RELEASE_OUT" ;;
*) pass "a release that leaves the schema alone is not warned about" ;;
esac

new_repo
with_skill v1.0.0
cut_release v1.0.0
with_skill v2.0.0
cut_release PATCH

case "$RELEASE_OUT" in
*"v2.0.0"*"$unreleased"*) pass "a minimum version above the release is warned about" ;;
*) fail "a minimum version above the release is warned about" "$RELEASE_OUT" ;;
esac

# ---------------------------------------------------------------------------

if [ "$fails" -ne 0 ]; then
	printf '\n%d test(s) failed\n' "$fails"
	exit 1
fi
printf '\nall release.sh tests passed\n'
