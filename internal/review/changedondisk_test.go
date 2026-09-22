package review_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// snapshottingDeriver is a fixedDeriver that also snapshots, always answering
// with the same tree id — enough for a test to check that what the core asks
// about is the round's own snapshot.
type snapshottingDeriver struct {
	fixedDeriver
	tree string
}

func (d snapshottingDeriver) Snapshot(string) (string, error) { return d.tree, nil }

func (d snapshottingDeriver) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return fakeMapping{}, nil
}

// diskResolver is a stubResolver that can also say whether a file on disk still
// matches a snapshot. edited holds each file a test has changed since a given
// snapshot, so a question about any other snapshot reads as unchanged and a core
// that asks about the wrong one is caught.
type diskResolver struct {
	stubResolver
	edited map[editedFile]bool
}

type editedFile struct{ snapshot, repository, file string }

func (d *diskResolver) ChangedOnDisk(repository, snapshot, file string) bool {
	return d.edited[editedFile{snapshot, repository, file}]
}

func (d *diskResolver) edit(snapshot, repository, file string) {
	if d.edited == nil {
		d.edited = map[editedFile]bool{}
	}
	d.edited[editedFile{snapshot, repository, file}] = true
}

func TestAStepWhoseFileChangedOnDiskStillShowsTheCodeItWasPostedWith(t *testing.T) {
	resolver := &diskResolver{}
	session := review.NewSession(resolver, snapshottingDeriver{fixedDeriver{lines: changed("src/fetch.ts", 20, 22)}, "tree-1"})
	mustPost(t, session, validRound())
	mustAdvance(t, session)
	if session.View().Step.Excerpts[0].ChangedOnDisk {
		t.Fatal("a file untouched since the post must not be flagged")
	}

	resolver.edit("tree-1", "/repos/argus-portal", "src/fetch.ts")
	step := session.View().Step

	excerpt := step.Excerpts[0]
	if !excerpt.ChangedOnDisk {
		t.Error("expected the edited file to be flagged as changed on disk")
	}
	if excerpt.Problem != "" || len(excerpt.Lines) != 23 {
		t.Errorf("expected the Step to go on showing all 23 lines it was posted with, got %d (problem %q)", len(excerpt.Lines), excerpt.Problem)
	}
}

func TestOnlyTheFileThatChangedOnDiskIsFlagged(t *testing.T) {
	walkthrough := validRound()
	walkthrough.Steps = []review.Step{
		{Name: "A", Explanation: "a", Excerpts: []review.Excerpt{{Repository: "/repos/argus-portal", File: "a.ts", Side: review.NewSide, FirstLine: 1, LastLine: 10}}},
		{Name: "B", Explanation: "b", Excerpts: []review.Excerpt{{Repository: "/repos/argus-portal", File: "b.ts", Side: review.NewSide, FirstLine: 1, LastLine: 10}}},
	}
	resolver := &diskResolver{}
	lines := append(changed("a.ts", 1, 3), changed("b.ts", 1, 3)...)
	session := review.NewSession(resolver, snapshottingDeriver{fixedDeriver{lines: lines}, "tree-1"})
	mustPost(t, session, walkthrough)

	resolver.edit("tree-1", "/repos/argus-portal", "a.ts")

	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if !session.View().Step.Excerpts[0].ChangedOnDisk {
		t.Error("expected a.ts to be flagged")
	}
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	if session.View().Step.Excerpts[0].ChangedOnDisk {
		t.Error("expected b.ts, untouched, not to be flagged")
	}
}

const postedVersionNote = "(file changed since this round was posted; line numbers are from the posted version)"

func TestAnchoringCodeThatChangedOnDiskQuotesThePostedVersionAndSaysSo(t *testing.T) {
	resolver := &diskResolver{}
	session := review.NewSession(resolver, snapshottingDeriver{fixedDeriver{lines: changed("src/fetch.ts", 20, 22)}, "tree-1"})
	mustPost(t, session, validRound())
	mustAdvance(t, session)
	resolver.edit("tree-1", "/repos/argus-portal", "src/fetch.ts")

	anchor, err := session.Anchor(span(0, 20, 22))

	if err != nil {
		t.Fatalf("expected code that changed on disk to be anchorable, got %v", err)
	}
	rendered := anchor.Render()
	header, _, _ := strings.Cut(rendered, "\n")
	if !strings.Contains(header, postedVersionNote) {
		t.Errorf("expected the Re: header to say the numbers are from the posted version:\n%s", rendered)
	}
	if !strings.Contains(rendered, "src/fetch.ts line 20") {
		t.Errorf("expected the posted lines quoted:\n%s", rendered)
	}
}

func TestAnAnchorOnUnchangedCodeCarriesNoNote(t *testing.T) {
	resolver := &diskResolver{}
	session := review.NewSession(resolver, snapshottingDeriver{fixedDeriver{lines: changed("src/fetch.ts", 20, 22)}, "tree-1"})
	mustPost(t, session, validRound())
	mustAdvance(t, session)

	anchor, err := session.Anchor(span(0, 20, 22))

	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(anchor.Render(), "changed since") {
		t.Errorf("an unchanged file needs no note:\n%s", anchor.Render())
	}
}

func TestAnExpandedAcknowledgementThatChangedOnDiskShowsAndAnchorsThePostedVersion(t *testing.T) {
	resolver := &diskResolver{}
	walkthrough := validRound()
	walkthrough.Steps = []review.Step{acknowledgingStep("gen.ts")}
	session := review.NewSession(resolver, snapshottingDeriver{fixedDeriver{lines: changed("gen.ts", 1, 3)}, "tree-1"})
	mustPost(t, session, walkthrough)
	mustAdvance(t, session)
	resolver.edit("tree-1", "/repos/argus-portal", "gen.ts")
	ack := 0

	views, err := session.ExpandAcknowledgement(1, ack)
	if err != nil {
		t.Fatal(err)
	}
	anchor, anchorErr := session.Anchor(review.AnchorTarget{Acknowledgement: &ack, Start: newRow(1), End: newRow(2)})

	if len(views) != 1 || !views[0].ChangedOnDisk || len(views[0].Lines) != 3 {
		t.Errorf("expected the expansion to show its 3 posted lines, flagged, got %+v", views)
	}
	if anchorErr != nil {
		t.Fatalf("expected acknowledged code that changed on disk to be anchorable, got %v", anchorErr)
	}
	if header, _, _ := strings.Cut(anchor.Render(), "\n"); !strings.Contains(header, postedVersionNote) {
		t.Errorf("expected the Re: header to carry the note, got %q", header)
	}
}

func TestADeletionShownFromTheMergeBaseIsNeverFlagged(t *testing.T) {
	// An old-side Excerpt is read from the merge-base, which no edit on disk
	// moves, so there is nothing to warn about even if the file is edited.
	walkthrough := validRound()
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.OldSide, FirstLine: 5, LastLine: 7}
	deleted := []review.ChangedLine{
		{File: "src/fetch.ts", Side: review.OldSide, Line: 5},
		{File: "src/fetch.ts", Side: review.OldSide, Line: 6},
		{File: "src/fetch.ts", Side: review.OldSide, Line: 7},
	}
	resolver := &diskResolver{}
	session := review.NewSession(resolver, snapshottingDeriver{fixedDeriver{lines: deleted}, "tree-1"})
	mustPost(t, session, walkthrough)
	mustAdvance(t, session)

	resolver.edit("tree-1", "/repos/argus-portal", "src/fetch.ts")

	if session.View().Step.Excerpts[0].ChangedOnDisk {
		t.Error("a before-side read from the merge-base must not be flagged")
	}
}
