// Package buildinfo carries the version this binary was built as. It lives in
// an internal package rather than in package main so every part of dbn — the
// daemon's MCP handshake, the shim, `dbn version`, the update check — reports
// the one stamp instead of its own hardcoded guess.
package buildinfo

import (
	"strconv"
	"strings"
)

// version is the build stamp. It is a var, not a const, so a release build can
// override it with:
//
//	-ldflags "-X github.com/probertson/diff-by-numbers/internal/buildinfo.version=X.Y.Z"
//
// The default says development build: only the release pipeline stamps a real
// version, so anything else is someone's own `go build`.
var version = "0.1.0-dev"

// Version is what this binary was built as.
func Version() string { return version }

// IsRelease reports whether this build came off the release pipeline — a bare
// X.Y.Z stamp. Anything else is a development build, which never checks for
// updates and never replaces itself: there is no published release it is
// meaningfully behind, and its binary was not downloaded in the first place.
func IsRelease() bool { return isRelease(version) }

func isRelease(s string) bool {
	_, ok := parse(s)
	return ok
}

// Newer reports whether candidate is a strictly newer release than current.
// Either side being unparseable makes the answer no: dbn never offers a move it
// cannot order, and — since this is a strict comparison — never a downgrade.
func Newer(candidate, current string) bool {
	c, ok := parse(candidate)
	if !ok {
		return false
	}
	base, ok := parse(current)
	if !ok {
		return false
	}
	return c.after(base)
}

// Normalize strips the leading "v" a git tag carries, so a release named v0.2.0
// is spoken about as 0.2.0 — the form the binary stamps and reports.
func Normalize(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }

// release is a plain X.Y.Z version. Pre-releases and build metadata are
// deliberately not understood: dbn publishes none, so a parser that ordered them
// would be inventing an ordering it never has to make. The dev stamp
// (0.1.0-dev) failing to parse is exactly what marks it as not a release.
type release struct{ major, minor, patch int }

func (r release) after(other release) bool {
	switch {
	case r.major != other.major:
		return r.major > other.major
	case r.minor != other.minor:
		return r.minor > other.minor
	default:
		return r.patch > other.patch
	}
}

func parse(s string) (release, bool) {
	parts := strings.Split(Normalize(s), ".")
	if len(parts) != 3 {
		return release{}, false
	}
	var out [3]int
	for i, part := range parts {
		// strconv.Atoi accepts a sign and Go's own "0x" is not a worry here, but
		// "+1" or "-0" would parse; a version component is plain digits.
		if part == "" || strings.ContainsFunc(part, func(r rune) bool { return r < '0' || r > '9' }) {
			return release{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return release{}, false
		}
		out[i] = n
	}
	return release{major: out[0], minor: out[1], patch: out[2]}, true
}
