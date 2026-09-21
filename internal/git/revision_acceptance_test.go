package git_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// newReviewSession drives the real git adapter, so these exercise the snapshot
// and mapping against repositories on disk rather than a stand-in.
func newReviewSession() *review.Session {
	return review.NewSession(workingtree.NewResolver(), git.NewDeriver())
}

func stepOver(root, file string, first, last int) review.Step {
	return review.Step{
		Name: "Step", Explanation: "e",
		Excerpts: []review.Excerpt{
			{Repository: root, File: file, Side: review.NewSide, FirstLine: first, LastLine: last},
		},
	}
}

func roundOver(root string, steps []review.Step, dispositions []review.Disposition) review.Walkthrough {
	return review.Walkthrough{
		Brief:        review.Brief{Ask: "x", Approach: "y", Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "s"}},
		ChangeSet:    review.ChangeSet{Repositories: []review.Repository{{Root: root, Base: "main"}}},
		Steps:        steps,
		Dispositions: dispositions,
	}
}

// finishRound posts a Walkthrough and finishes it, leaving the Session ready for
// a Revision Round.
func finishRound(t *testing.T, session *review.Session, w review.Walkthrough) {
	t.Helper()
	if err := session.Post(w); err != nil {
		t.Fatalf("posting the round: %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("finishing the round: %v", err)
	}
}

// The measured case from the ticket: in a braced language most lines are blank
// or a lone `}`, none of which is unique, so content matching could never scope
// them out — 42% of one real Change Set. Position handles them exactly.
func TestUntouchedBoilerplateIsPreMarkedInARevisionRound(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "function a() {\n}\n\nfunction b() {\n}\n")

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 5)}, nil))

	// Round 2 edits only line 1. Every other line — two `}`, one blank — is
	// untouched, and none of them has unique content.
	write(t, root, "app.ts", "function renamed() {\n}\n\nfunction b() {\n}\n")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 1)}, nil))

	if err != nil {
		t.Fatalf("only line 1 moved, so covering it should be enough: %v", err)
	}
	// The Change Set spans both sides — the base's three lines were replaced —
	// so assert the property rather than a hand-counted total: exactly one atom,
	// the edited line 1, is left to read.
	view := session.View()
	if demanded := view.Coverage.Total - view.Coverage.Seen; demanded != 1 {
		t.Errorf("expected only the edited line demanded, got %d of %d demanded",
			demanded, view.Coverage.Total)
	}
}

// The safety property, against a real repository: a genuinely new `}` must not
// be pre-marked off the back of an existing one.
func TestANewlyAddedBraceIsStillDemanded(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "function a() {\n}\n")

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 2)}, nil))

	// A whole new function, whose closing brace is byte-identical to the old one.
	write(t, root, "app.ts", "function a() {\n}\nfunction b() {\n}\n")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 2)}, nil))

	if err == nil {
		t.Fatal("the new function's lines were never reviewed and must be demanded")
	}
	if !strings.Contains(err.Error(), "uncovered_changes") {
		t.Errorf("expected a coverage rejection, got %v", err)
	}
}

// #66, absorbed here: old-side lines never went through content matching at all,
// because a removed line has no working-tree text to read. They are positions in
// the merge-base, which has not moved, so they map by identity.
func TestAnUntouchedDeletionIsPreMarkedInARevisionRound(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\n") // two and three deleted from the base

	// The deletion is at the end of the file, so no new-side line rides it along:
	// round 1 has to show it with an old-side Excerpt of its own.
	shown := []review.Step{stepOver(root, "app.ts", 1, 1)}
	shown[0].Excerpts = append(shown[0].Excerpts,
		review.Excerpt{Repository: root, File: "app.ts", Side: review.OldSide, FirstLine: 2, LastLine: 3})

	session := newReviewSession()
	finishRound(t, session, roundOver(root, shown, nil))
	before := session.View().Coverage.Total

	// Round 2 changes nothing at all: the same deletion still stands, and the
	// agent should not have to re-walk it.
	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 1)}, nil))

	if err != nil {
		t.Fatalf("nothing moved, so the round should be accepted: %v", err)
	}
	view := session.View()
	if view.Coverage.Total != before {
		t.Errorf("the Change Set should be unchanged, got %d then %d", before, view.Coverage.Total)
	}
	if view.Coverage.Seen != view.Coverage.Total {
		t.Errorf("nothing moved, so everything should be pre-marked: %d of %d", view.Coverage.Seen, view.Coverage.Total)
	}
}

// An Opaque Change has no lines to map, so content matching never pre-marked one
// at all (ADR-0007's limit): every round re-Acknowledged every binary. It counts
// as read when the file is byte-identical between rounds.
func TestAnUntouchedBinaryNeedsNoSecondAcknowledgement(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xff})

	acknowledged := stepOver(root, "app.ts", 1, 4)
	acknowledged.Acknowledgements = []review.Acknowledgement{
		{Repository: root, Files: []string{"logo.png"}, Reason: "re-exported asset"},
	}
	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{acknowledged}, nil))

	// Round 2 touches only the code. The binary is untouched, so the agent should
	// not have to Acknowledge it again.
	write(t, root, "app.ts", "one\ntwo\nthree\nFOUR\n")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 4, 4)}, nil))

	if err != nil {
		t.Fatalf("an untouched binary should not need re-Acknowledging: %v", err)
	}
}

func TestARetouchedBinaryIsDemandedAgain(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xff})

	acknowledged := stepOver(root, "app.ts", 1, 4)
	acknowledged.Acknowledgements = []review.Acknowledgement{
		{Repository: root, Files: []string{"logo.png"}, Reason: "re-exported asset"},
	}
	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{acknowledged}, nil))

	// The agent re-exported the asset in response to feedback: it is new again.
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xAA, 0xBB})

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 4)}, nil))

	if err == nil {
		t.Fatal("a binary that changed since the last round must be Acknowledged again")
	}
	if !strings.Contains(err.Error(), "logo.png") {
		t.Errorf("the rejection should name the re-touched binary, got %v", err)
	}
}

// The snapshot, not HEAD, is the reference: uncommitted round-1 work that is
// later overwritten still has to map, or a Reviewer would be re-shown code they
// read because the agent happened to commit in between.
func TestUncommittedRoundOneWorkStillMapsAfterBeingCommitted(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n") // uncommitted in round 1

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 4)}, nil))

	// Between rounds the agent commits exactly what was reviewed, changing HEAD
	// but not the working tree.
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "commit the reviewed work")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 1)}, nil))

	if err != nil {
		t.Fatalf("committing already-reviewed work should move nothing: %v", err)
	}
	view := session.View()
	if view.Coverage.Seen != view.Coverage.Total {
		t.Errorf("nothing moved, so everything should be pre-marked: %d of %d",
			view.Coverage.Seen, view.Coverage.Total)
	}
}

// A rejected post changes nothing — including the stored snapshot. Otherwise a
// failed round would silently become the baseline the next round is scoped
// against, and lines the Reviewer never saw would be pre-marked as read.
func TestARejectedRevisionRoundLeavesTheStoredSnapshotAlone(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 4)}, nil))

	// A round that adds a line and fails to cover it.
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\nfive\n")
	// A well-formed round that simply fails to show the new line, so the refusal
	// is about coverage rather than a malformed post.
	rejected := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 1)}, nil))
	if rejected == nil || !strings.Contains(rejected.Error(), "uncovered_changes") {
		t.Fatalf("the uncovered new line should have been refused for coverage, got %v", rejected)
	}

	// The agent now covers it. If the rejected post had been taken as the
	// baseline, line 5 would count as already read and this would report it seen
	// without the Reviewer ever having been shown it.
	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 5, 5)}, nil))

	if err != nil {
		t.Fatalf("covering the new line should be accepted: %v", err)
	}
	view := session.View()
	if demanded := view.Coverage.Total - view.Coverage.Seen; demanded != 1 {
		t.Errorf("line 5 was never shown, so exactly one atom should be demanded, got %d", demanded)
	}
}

// Old-side lines are positions in the merge-base. When the branch is rebased
// under the review the base itself moves, so they have to be mapped through a
// diff of the two bases rather than assumed to have stayed put.
func TestARebaseBetweenRoundsMapsOldSideLinesThroughTheMovedBase(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\n") // two and three deleted from the base

	shown := stepOver(root, "app.ts", 1, 1)
	shown.Excerpts = append(shown.Excerpts,
		review.Excerpt{Repository: root, File: "app.ts", Side: review.OldSide, FirstLine: 2, LastLine: 3})

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{shown}, nil))

	// main gains a commit and the feature branch is rebased onto it, moving the
	// merge-base without changing what the branch itself did.
	run(t, root, "checkout", "-q", "main")
	write(t, root, "unrelated.ts", "elsewhere\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "unrelated work on main")
	run(t, root, "checkout", "-q", "feature")
	run(t, root, "rebase", "-q", "main")
	write(t, root, "app.ts", "one\n")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 1)}, nil))

	if err != nil {
		t.Fatalf("a rebase moved the base but the agent moved nothing: %v", err)
	}
	view := session.View()
	if view.Coverage.Seen != view.Coverage.Total {
		t.Errorf("nothing the agent did moved, so everything should be pre-marked: %d of %d",
			view.Coverage.Seen, view.Coverage.Total)
	}
}

// A deletion the agent made *since* the last round was never shown, so it must
// be demanded even though every other old-side line is pre-marked.
func TestADeletionMadeSinceTheLastRoundIsDemanded(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nEXTRA\n")

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 4)}, nil))

	// In response to feedback the agent deletes an original line.
	write(t, root, "app.ts", "one\nthree\nEXTRA\n")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 1, 1)}, nil))

	if err == nil {
		t.Fatal("a newly made deletion was never shown and must be demanded")
	}
	if !strings.Contains(err.Error(), "uncovered_changes") {
		t.Errorf("expected a coverage rejection, got %v", err)
	}
}

// A mode change is an Opaque Change with no lines, like a binary.
func TestAnUntouchedModeChangeNeedsNoSecondAcknowledgement(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	write(t, root, "script.sh", "echo hi\n")
	run(t, root, "add", "script.sh")
	run(t, root, "commit", "-qm", "add a script")
	run(t, root, "update-index", "--chmod=+x", "script.sh")

	acknowledged := stepOver(root, "app.ts", 1, 4)
	acknowledged.Acknowledgements = []review.Acknowledgement{
		{Repository: root, Files: []string{"script.sh"}, Reason: "made executable"},
	}
	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{acknowledged}, nil))

	write(t, root, "app.ts", "one\ntwo\nthree\nFOUR\n")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "app.ts", 4, 4)}, nil))

	if err != nil {
		t.Fatalf("an untouched mode change should not need re-Acknowledging: %v", err)
	}
}

// Renaming a file between rounds must not make the Reviewer re-read it. Both
// sides matter: the new side follows git's rename detection, and the old side
// keeps its line numbers but has to follow the file to its new name, because
// the ledger files old-side lines under the path the file has now.
func TestAFileRenamedBetweenRoundsIsNotReDemanded(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q", "-b", "main")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\nfive\nsix\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "initial")

	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\nthree\nfour\nfive\nsix\n") // "two" deleted

	// A pure deletion has no new-side line to ride along on, so round 1 shows the
	// removed line with an old-side Excerpt of its own. That is the line whose
	// path has to follow the rename below.
	shown := stepOver(root, "app.ts", 1, 5)
	shown.Excerpts = append(shown.Excerpts,
		review.Excerpt{Repository: root, File: "app.ts", Side: review.OldSide, FirstLine: 2, LastLine: 2})

	session := newReviewSession()
	finishRound(t, session, roundOver(root, []review.Step{shown}, nil))

	// The agent renames the file in response to feedback, changing nothing in it.
	// Content is similar enough that git reports one rename rather than a delete
	// and an add, which is the case this exercises.
	move(t, root, "app.ts", "renamed.ts")

	err := session.Post(roundOver(root, []review.Step{stepOver(root, "renamed.ts", 1, 5)}, nil))

	if err != nil {
		t.Fatalf("a rename moved no code, so nothing should be re-demanded: %v", err)
	}
	view := session.View()
	if view.Coverage.Seen != view.Coverage.Total {
		t.Errorf("a pure rename moves nothing, so everything should be pre-marked: %d of %d",
			view.Coverage.Seen, view.Coverage.Total)
	}
}
