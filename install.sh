#!/bin/sh
# install.sh — download the right prebuilt dbn binary for this machine and put it
# on PATH. No Go toolchain, no build step. Served raw from main:
#
#   curl -fsSL https://raw.githubusercontent.com/probertson/diff-by-numbers/main/install.sh | sh
#
# Honours:
#   DBN_VERSION      install a specific release tag (e.g. v0.2.0) instead of the latest
#   DBN_INSTALL_DIR  where to install (default: $HOME/.local/bin)
#
# Ships macOS and Linux, on amd64 and arm64. On native Windows, use WSL2 (it is
# served by the Linux build).
set -eu

OWNER="probertson"
REPO="diff-by-numbers"
BINARY="dbn"

info() { printf '%s\n' "$*"; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

# fetch URL OUTFILE — download, following redirects, failing on HTTP errors.
fetch() {
	curl -fsSL "$1" -o "$2"
}

# sha256_of FILE — the file's SHA-256, on either the Linux or macOS tool.
sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

main() {
	for tool in curl tar awk mktemp; do
		command -v "$tool" >/dev/null 2>&1 || die "required tool not found: $tool"
	done
	command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 ||
		die "required tool not found: need sha256sum or shasum for checksum verification"

	os_raw=$(uname -s)
	case "$os_raw" in
	Darwin) os="darwin" ;;
	Linux) os="linux" ;;
	*) die "unsupported operating system: ${os_raw} — dbn ships macOS and Linux binaries; on Windows use WSL2" ;;
	esac

	arch_raw=$(uname -m)
	case "$arch_raw" in
	x86_64 | amd64) arch="amd64" ;;
	arm64 | aarch64) arch="arm64" ;;
	*) die "unsupported architecture: ${arch_raw} — dbn ships amd64 and arm64 binaries" ;;
	esac

	asset="${BINARY}_${os}_${arch}.tar.gz"

	version="${DBN_VERSION:-latest}"
	if [ "$version" = "latest" ]; then
		base="https://github.com/${OWNER}/${REPO}/releases/latest/download"
	else
		base="https://github.com/${OWNER}/${REPO}/releases/download/${version}"
	fi

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	info "downloading ${asset} (${version})..."
	fetch "${base}/${asset}" "${tmp}/${asset}" || die "download failed: ${base}/${asset}"
	fetch "${base}/checksums.txt" "${tmp}/checksums.txt" || die "could not download checksums for verification"

	want=$(awk -v f="$asset" '$2 == f {print $1}' "${tmp}/checksums.txt")
	[ -n "$want" ] || die "no checksum published for ${asset} — refusing to install"
	got=$(sha256_of "${tmp}/${asset}")
	[ -n "$got" ] || die "could not compute checksum for ${asset}"
	[ "$want" = "$got" ] || die "checksum mismatch for ${asset} (expected ${want}, got ${got}) — refusing to install"

	tar -xzf "${tmp}/${asset}" -C "$tmp" "$BINARY" || die "could not extract ${BINARY} from ${asset}"

	dest="${DBN_INSTALL_DIR:-$HOME/.local/bin}"
	mkdir -p "$dest" || die "could not create install directory: ${dest}"

	# Stage inside $dest, then rename into place. mktemp's dir and $dest can be on
	# different filesystems, so a direct mv would be a non-atomic copy that could
	# corrupt an existing binary if interrupted; a same-filesystem rename cannot.
	staged="${dest}/.${BINARY}.$$"
	cp "${tmp}/${BINARY}" "$staged" || {
		rm -f "$staged"
		die "could not stage ${BINARY} in ${dest}"
	}
	chmod +x "$staged"
	mv "$staged" "${dest}/${BINARY}" || {
		rm -f "$staged"
		die "could not install to ${dest}"
	}

	info "installed ${BINARY} to ${dest}/${BINARY}"

	case ":${PATH}:" in
	*":${dest}:"*) : ;; # already on PATH
	*)
		info ""
		info "${dest} is not on your PATH. Add this line to your shell profile:"
		info "    export PATH=\"${dest}:\$PATH\""
		;;
	esac

	info ""
	info "run '${BINARY} version' to confirm."
}

main "$@"
