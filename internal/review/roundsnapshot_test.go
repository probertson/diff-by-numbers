package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// treeResolver reads each file as though from a git tree: the lines it returns
// name the Round Snapshot they came from, and each snapshot holds a file of the
// length a test gives it. "" stands for the working tree, which is what a
// repository without a snapshot is read from.
type treeResolver struct {
	lengths map[string]int
}

func (r treeResolver) Resolve(e review.Excerpt, round review.Round) ([]review.Line, error) {
	tree := round.Snapshots[e.Repository]
	if e.LastLine > r.lengths[tree] {
		return nil, fmt.Errorf("%s has %d lines in %q", e.File, r.lengths[tree], tree)
	}
	var lines []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: fmt.Sprintf("%s: line %d", tree, n)})
	}
	return lines, nil
}

// sequenceDeriver snapshots each repository to the next tree in its list, so a
// test can tell one round's snapshot from the next.
type sequenceDeriver struct {
	fixedDeriver
	trees []string
	taken int
}

func (d *sequenceDeriver) Snapshot(string) (string, error) {
	tree := d.trees[d.taken]
	d.taken++
	return tree, nil
}

func (d *sequenceDeriver) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return fakeMapping{}, nil
}

func TestAStepShowsItsCodeFromTheRoundSnapshot(t *testing.T) {
	resolver := treeResolver{lengths: map[string]int{"tree-1": 100, "": 100}}
	session := review.NewSession(resolver, &sequenceDeriver{fixedDeriver: fixedDeriver{lines: changed("src/fetch.ts", 20, 22)}, trees: []string{"tree-1"}})
	mustPost(t, session, validWalkthrough())

	mustAdvance(t, session)

	if got := session.View().Step.Excerpts[0].Lines[0].Text; got != "tree-1: line 12" {
		t.Errorf("expected the posted snapshot's line, got %q", got)
	}
}

func TestAPostIsCheckedAgainstTheSnapshotItWillBeShownFrom(t *testing.T) {
	// The working tree still has every line the Excerpt names, but by the time
	// the snapshot was taken the file had shrunk to 30: the Walkthrough would be
	// shown from a snapshot that cannot satisfy it, so it must be refused.
	resolver := treeResolver{lengths: map[string]int{"tree-1": 30, "": 100}}
	session := review.NewSession(resolver, &sequenceDeriver{fixedDeriver: fixedDeriver{lines: changed("src/fetch.ts", 20, 22)}, trees: []string{"tree-1"}})

	err := session.Post(validWalkthrough())

	assertRejected(t, err, review.RejectedUnresolvableExcerpt)
}

func TestARejectedPostLeavesTheViewReadingTheRoundOnScreen(t *testing.T) {
	resolver := treeResolver{lengths: map[string]int{"tree-1": 100, "tree-2": 100}}
	deriver := &sequenceDeriver{fixedDeriver: fixedDeriver{lines: changed("src/fetch.ts", 20, 22)}, trees: []string{"tree-1", "tree-2"}}
	session := review.NewSession(resolver, deriver)
	mustPost(t, session, validWalkthrough())
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	// Refused only once its snapshot has been taken: the disposition names a
	// Comment the first round never raised.
	misdisposed := validWalkthrough()
	misdisposed.Dispositions = []review.Disposition{{CommentID: 7, Status: review.DispositionAddressed}}

	err := session.Post(misdisposed)

	assertRejected(t, err, review.RejectedMalformedDisposition)
	if deriver.taken != 2 {
		t.Fatalf("expected the refused post to have taken its own snapshot, took %d in all", deriver.taken)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if got := session.View().Step.Excerpts[0].Lines[0].Text; !strings.HasPrefix(got, "tree-1:") {
		t.Errorf("a rejected post must not move the view off the round on screen, got %q", got)
	}
}
