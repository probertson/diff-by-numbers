// The ports a Session reaches the git adapter through to scope a Revision
// Round, and to show the Reviewer what changed since the last one. The core
// holds no git knowledge of its own: it asks for a snapshot, asks how two
// snapshots differ, and decides for itself what that means — for coverage, and
// for how the round is shaded (#44). See ADR-0014.
package review

// Position is where a line stood in one round's snapshot of the working tree.
type Position struct {
	File string
	Line int
}

// RoundMapping answers what moved between two rounds.
type RoundMapping interface {
	// Lookup returns where a line of the later round stood in the earlier one.
	// ok is false when the line was touched in between, so nothing about it has
	// been reviewed.
	Lookup(file string, line int) (Position, bool)
	// Touched reports whether a file differs at all between the two rounds. A
	// file that does not is identical in path, content and mode, which is what
	// lets an Opaque Change — a binary, a mode change, a rename, none of which
	// has lines to map — be recognised as already reviewed.
	Touched(file string) bool
	// PathIn returns what this file was called in the earlier round, which
	// differs only when it was renamed in between. Old-side lines need this on
	// its own: they are positions in the merge-base, so their numbers do not
	// move, but the ledger files them under the file's *current* path.
	PathIn(file string) string
	// Edits lists what changed in a file between the two rounds, in file order:
	// what a Revision Round shows the Reviewer when it shades a Step by what
	// changed since the last round, rather than since the merge-base (#44).
	Edits(file string) []RoundEdit
	// Files names every file that differs between the two rounds, by its path
	// in the later one — or in the earlier, for a file the later round no
	// longer has, which nothing else would ever name.
	Files() []string
}

// RoundEdit is one edit between two rounds: the lines of the earlier round it
// replaced, and the lines of the later one that took their place. Either count
// may be zero. A pure withdrawal has no new lines, and then NewFirst is the line
// above the gap it left, as git reports it.
type RoundEdit struct {
	OldFirst, OldCount int
	NewFirst, NewCount int
}

// Snapshotter is the optional capability that lets a Revision Round be scoped to
// what the Authoring Agent actually moved.
//
// Scoping used to work by line content: a Changed Line was pre-marked as already
// reviewed when its text was unique in both rounds (ADR-0007). In any braced
// language that disqualifies a large share of the Change Set — blank lines and
// closing braces are never unique — so rounds stayed nearly as large as the
// first however little moved.
//
// Instead the adapter records each accepted round as a tree object and maps the
// next round's lines back through a diff of the two. The core holds no git
// knowledge: it asks for a snapshot, asks for a mapping, and decides for itself
// what being unmapped means for coverage.
//
// It is optional so a Session can be driven by a Deriver that cannot snapshot —
// tests with a fake ledger, chiefly. Without it nothing is pre-marked, which is
// the same conservative answer a failed snapshot gives.
type Snapshotter interface {
	// Snapshot records a repository's working tree and returns an opaque id.
	Snapshot(root string) (string, error)
	// MapBetween maps positions in the `to` snapshot back to the `from` one.
	MapBetween(root, from, to string) (RoundMapping, error)
}
