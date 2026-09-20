#!/bin/sh
# release_test.sh — the test seam for scripts/release.sh.
#
# It drives the real release.sh against a throwaway repository with a real
# `origin`: a bare repo on disk, so pushes and tags are genuine git operations
# that go nowhere near GitHub. Nothing is stubbed but the tests the script would
# run (SKIP_TESTS=1) and the confirmation prompt (fed on stdin).
#
# That lets us assert the release's external behaviour — the plugin version it
# writes, the commit it makes, which commit the tag lands on, and the state it
# leaves the tree in — without cutting a release.
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
new_repo() {
	SAND=$(mktemp -d)
	sandboxes="${sandboxes}${SAND}
"
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
	RELEASE_OUT=$(cd "$WORK" && printf 'y\n' | SKIP_TESTS=1 sh "$RELEASE" "$1" 2>&1)
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

# A bump keyword has to resolve against the tag just cut and carry the same
# behaviour, since that is how a release is normally cut.
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

# ---------------------------------------------------------------------------

if [ "$fails" -ne 0 ]; then
	printf '\n%d test(s) failed\n' "$fails"
	exit 1
fi
printf '\nall release.sh tests passed\n'
