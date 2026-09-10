#!/bin/sh
# install_test.sh — the test seam for the curl-able installer.
#
# It drives the real install.sh with its two external touch-points injected: a
# stub `curl` and a stub `uname` placed ahead of the real ones on PATH, and
# release fixtures on disk. That lets us assert the installer's external
# behaviour — asset selection, URL construction, checksum verification, install
# location, and PATH guidance — with no network and no real GitHub Release.
#
# Pure POSIX sh; no bats. Run directly (`sh install_test.sh`) or via
# `go test ./internal/installer`, which execs this file.
set -u

here=$(CDPATH= cd "$(dirname "$0")" && pwd)
INSTALL="$here/install.sh"

fails=0
pass() { printf 'ok   - %s\n' "$1"; }
fail() {
	printf 'FAIL - %s\n' "$1"
	[ $# -ge 2 ] && printf '       %s\n' "$2"
	fails=$((fails + 1))
}

sha_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

# new_sandbox — a fresh temp world per test. Sets SAND, FIXTURES, DEST, STUBBIN,
# CURL_LOG, HOMEDIR, REAL_PATH; installs the stub curl/uname; clears any leftover
# per-test env so one case never leaks into the next.
new_sandbox() {
	unset STUB_S STUB_M DBN_VERSION DBN_INSTALL_DIR EXTRA_PATH 2>/dev/null || true

	SAND=$(mktemp -d)
	FIXTURES="$SAND/fixtures"
	DEST="$SAND/dest"
	STUBBIN="$SAND/bin"
	CURL_LOG="$SAND/curl.log"
	HOMEDIR="$SAND/home"
	mkdir -p "$FIXTURES" "$STUBBIN" "$HOMEDIR"
	: >"$CURL_LOG"

	# Stub curl: understands `-fsSL URL -o OUT` (and `... URL` to stdout), logs
	# every requested URL, serves $FIXTURES/<basename>, and 404s (exit 22) when
	# the fixture is absent — the real curl -f behaviour the script relies on.
	cat >"$STUBBIN/curl" <<'EOF'
#!/bin/sh
out=""; url=""
while [ $# -gt 0 ]; do
	case "$1" in
		-o) out="$2"; shift 2 ;;
		-*) shift ;;
		*) url="$1"; shift ;;
	esac
done
printf '%s\n' "$url" >> "$CURL_LOG"
src="$FIXTURES/$(basename "$url")"
[ -f "$src" ] || exit 22
if [ -n "$out" ]; then cp "$src" "$out"; else cat "$src"; fi
EOF

	# Stub uname: canned OS/arch from STUB_S/STUB_M.
	cat >"$STUBBIN/uname" <<'EOF'
#!/bin/sh
case "$1" in
	-s) printf '%s\n' "${STUB_S:-Linux}" ;;
	-m) printf '%s\n' "${STUB_M:-x86_64}" ;;
	*) printf '%s\n' "unknown" ;;
esac
EOF
	chmod +x "$STUBBIN/curl" "$STUBBIN/uname"

	export FIXTURES CURL_LOG
	REAL_PATH="$PATH"
}

# add_release ASSET — drop a fixture archive named ASSET in $FIXTURES holding a
# recognisable dbn binary, and append its real checksum to checksums.txt.
add_release() {
	asset="$1"
	work="$SAND/work"
	rm -rf "$work"
	mkdir -p "$work"
	printf '#!/bin/sh\necho dbn fixture\n' >"$work/dbn"
	chmod +x "$work/dbn"
	(cd "$work" && tar -czf "$FIXTURES/$asset" dbn)
	printf '%s  %s\n' "$(sha_of "$FIXTURES/$asset")" "$asset" >>"$FIXTURES/checksums.txt"
}

# do_install — run install.sh in the sandbox, capturing OUT (stdout+stderr) and
# CODE. Honours the STUB_*/DBN_*/EXTRA_PATH the caller exported.
do_install() {
	path="$STUBBIN:$REAL_PATH"
	[ -n "${EXTRA_PATH:-}" ] && path="$EXTRA_PATH:$path"
	OUT=$(env PATH="$path" HOME="$HOMEDIR" sh "$INSTALL" 2>&1)
	CODE=$?
}

# --- asset selection per platform -------------------------------------------
check_asset() { # desc uname_s uname_m expected_asset
	new_sandbox
	export STUB_S="$2" STUB_M="$3" DBN_INSTALL_DIR="$DEST"
	add_release "$4"
	do_install
	if [ "$CODE" -ne 0 ]; then
		fail "$1" "exit $CODE: $OUT"
		return
	fi
	if [ ! -x "$DEST/dbn" ]; then
		fail "$1" "binary not installed"
		return
	fi
	if ! grep -q "/$4\$" "$CURL_LOG"; then
		fail "$1" "did not request $4; log: $(cat "$CURL_LOG")"
		return
	fi
	pass "$1"
}

check_asset "darwin/arm64 selects dbn_darwin_arm64" Darwin arm64 dbn_darwin_arm64.tar.gz
check_asset "darwin/amd64 (Intel) selects dbn_darwin_amd64" Darwin x86_64 dbn_darwin_amd64.tar.gz
check_asset "linux/amd64 selects dbn_linux_amd64" Linux x86_64 dbn_linux_amd64.tar.gz
check_asset "linux/arm64 (aarch64) selects dbn_linux_arm64" Linux aarch64 dbn_linux_arm64.tar.gz

# --- latest vs pinned URL ----------------------------------------------------
new_sandbox
export STUB_S=Darwin STUB_M=arm64 DBN_INSTALL_DIR="$DEST"
add_release dbn_darwin_arm64.tar.gz
do_install
if grep -q 'releases/latest/download/dbn_darwin_arm64.tar.gz' "$CURL_LOG"; then
	pass "latest resolves via releases/latest/download"
else
	fail "latest resolves via releases/latest/download" "$(cat "$CURL_LOG")"
fi

new_sandbox
export STUB_S=Darwin STUB_M=arm64 DBN_INSTALL_DIR="$DEST" DBN_VERSION=v1.2.3
add_release dbn_darwin_arm64.tar.gz
do_install
if grep -q 'releases/download/v1.2.3/dbn_darwin_arm64.tar.gz' "$CURL_LOG"; then
	pass "DBN_VERSION pins releases/download/<tag>"
else
	fail "DBN_VERSION pins releases/download/<tag>" "$(cat "$CURL_LOG")"
fi

# --- checksum verification ---------------------------------------------------
new_sandbox
export STUB_S=Linux STUB_M=x86_64 DBN_INSTALL_DIR="$DEST"
add_release dbn_linux_amd64.tar.gz
printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" \
	dbn_linux_amd64.tar.gz >"$FIXTURES/checksums.txt"
do_install
if [ "$CODE" -ne 0 ] && [ ! -e "$DEST/dbn" ]; then
	pass "checksum mismatch aborts and installs nothing"
else
	fail "checksum mismatch aborts and installs nothing" \
		"code=$CODE installed=$([ -e "$DEST/dbn" ] && echo yes || echo no)"
fi

new_sandbox
export STUB_S=Linux STUB_M=x86_64 DBN_INSTALL_DIR="$DEST"
add_release dbn_linux_amd64.tar.gz
: >"$FIXTURES/checksums.txt" # no entry for the asset
do_install
if [ "$CODE" -ne 0 ] && [ ! -e "$DEST/dbn" ]; then
	pass "missing checksum entry aborts"
else
	fail "missing checksum entry aborts" "code=$CODE"
fi

# --- download failure --------------------------------------------------------
new_sandbox
export STUB_S=Linux STUB_M=x86_64 DBN_INSTALL_DIR="$DEST"
# no release added: the asset URL 404s
do_install
if [ "$CODE" -ne 0 ] && [ ! -e "$DEST/dbn" ]; then
	pass "download failure aborts and installs nothing"
else
	fail "download failure aborts and installs nothing" "code=$CODE"
fi

new_sandbox
export STUB_S=Linux STUB_M=x86_64 DBN_INSTALL_DIR="$DEST"
add_release dbn_linux_amd64.tar.gz
rm -f "$FIXTURES/checksums.txt" # asset present, but checksums.txt 404s
do_install
if [ "$CODE" -ne 0 ] && [ ! -e "$DEST/dbn" ]; then
	pass "checksums download failure aborts and installs nothing"
else
	fail "checksums download failure aborts and installs nothing" "code=$CODE"
fi

# --- unsupported platform ----------------------------------------------------
new_sandbox
export STUB_S=Windows_NT STUB_M=x86_64 DBN_INSTALL_DIR="$DEST"
add_release dbn_darwin_arm64.tar.gz
do_install
if [ "$CODE" -ne 0 ] && [ ! -s "$CURL_LOG" ] && printf '%s' "$OUT" | grep -qi 'unsupported'; then
	pass "unsupported OS fails fast before downloading"
else
	fail "unsupported OS fails fast before downloading" \
		"code=$CODE log=$(cat "$CURL_LOG") out=$OUT"
fi

new_sandbox
export STUB_S=Linux STUB_M=ppc64 DBN_INSTALL_DIR="$DEST"
do_install
if [ "$CODE" -ne 0 ] && printf '%s' "$OUT" | grep -qi 'unsupported'; then
	pass "unsupported architecture fails"
else
	fail "unsupported architecture fails" "code=$CODE out=$OUT"
fi

# --- install location --------------------------------------------------------
new_sandbox
export STUB_S=Darwin STUB_M=arm64 DBN_INSTALL_DIR="$SAND/custom/bin"
add_release dbn_darwin_arm64.tar.gz
do_install
if [ -x "$SAND/custom/bin/dbn" ]; then
	pass "DBN_INSTALL_DIR override honoured (dir created)"
else
	fail "DBN_INSTALL_DIR override honoured (dir created)" "code=$CODE out=$OUT"
fi

new_sandbox
export STUB_S=Darwin STUB_M=arm64
add_release dbn_darwin_arm64.tar.gz
do_install # DBN_INSTALL_DIR unset -> default
if [ -x "$HOMEDIR/.local/bin/dbn" ]; then
	pass "default install dir is ~/.local/bin (created)"
else
	fail "default install dir is ~/.local/bin (created)" "code=$CODE out=$OUT"
fi

new_sandbox
export STUB_S=Darwin STUB_M=arm64 DBN_INSTALL_DIR="$DEST"
mkdir -p "$DEST"
printf 'stale binary\n' >"$DEST/dbn" # a previously-installed dbn
add_release dbn_darwin_arm64.tar.gz
do_install
if [ "$CODE" -eq 0 ] && grep -q 'dbn fixture' "$DEST/dbn"; then
	pass "installing over an existing binary replaces it"
else
	fail "installing over an existing binary replaces it" "code=$CODE contents=$(cat "$DEST/dbn" 2>&1)"
fi

# --- PATH guidance -----------------------------------------------------------
new_sandbox
export STUB_S=Darwin STUB_M=arm64 DBN_INSTALL_DIR="$DEST"
add_release dbn_darwin_arm64.tar.gz
do_install # DEST not on PATH
if printf '%s' "$OUT" | grep -q "export PATH=" && printf '%s' "$OUT" | grep -q "$DEST"; then
	pass "prints PATH guidance when install dir is off PATH"
else
	fail "prints PATH guidance when install dir is off PATH" "$OUT"
fi

new_sandbox
export STUB_S=Darwin STUB_M=arm64 DBN_INSTALL_DIR="$DEST" EXTRA_PATH="$DEST"
add_release dbn_darwin_arm64.tar.gz
do_install # DEST on PATH
if printf '%s' "$OUT" | grep -q "export PATH="; then
	fail "stays quiet about PATH when dir already on PATH" "$OUT"
else
	pass "stays quiet about PATH when dir already on PATH"
fi

# --- summary -----------------------------------------------------------------
printf '\n'
if [ "$fails" -eq 0 ]; then
	printf 'all install.sh checks passed\n'
	exit 0
else
	printf '%d install.sh check(s) failed\n' "$fails"
	exit 1
fi
