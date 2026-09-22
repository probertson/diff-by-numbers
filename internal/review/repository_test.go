package review_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// With one repository under review, repeating its absolute root on every Excerpt
// and Acknowledgement is noise the agent cannot get wrong in an interesting way.
// It may leave the field out; dbn fills in the only repository there is.

const otherRepository = "/repos/argus-worker"

// onlyIn derives Changed Lines for one repository and nothing for any other, so
// a multi-repository test can say which tree the changes are actually in.
type onlyIn struct {
	root  string
	lines []review.ChangedLine
}

func (d onlyIn) Derive(repo review.Repository) (review.Derivation, error) {
	if repo.Root != d.root {
		return review.Derivation{}, nil
	}
	return review.Derivation{Lines: inRepository(d.lines, repo.Root)}, nil
}

// unrooted is an Excerpt on the shared test file that names no repository.
func unrooted(side review.Side, first, last int) review.Excerpt {
	return review.Excerpt{File: testFile, Side: side, FirstLine: first, LastLine: last}
}

func TestASingleRepositoryPostNeedNotNameIt(t *testing.T) {
	walkthrough := walkthroughShowing([]review.Excerpt{unrooted(review.NewSide, 1, 5)})
	walkthrough.Steps[0].Acknowledgements = []review.Acknowledgement{{
		Files:  []string{"go.sum"},
		Reason: "regenerated lockfile",
	}}
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines: append(changedOn(review.NewSide, 1, 5), changedFile("go.sum", review.NewSide, 1, 2)...),
	})

	err := session.Post(walkthrough)

	if err != nil {
		t.Fatalf("expected the post to be accepted without a repository, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.Repository != testRepository {
		t.Errorf("expected the Excerpt to resolve to %q, got %q", testRepository, got.Repository)
	}
}

func TestAMultiRepositoryPostMustNameTheRepository(t *testing.T) {
	// Two repositories, and nothing but the agent knows which one a path is in:
	// the same relative path can exist in both, so guessing would quietly point
	// the Reviewer at the wrong tree.
	walkthrough := walkthroughShowing([]review.Excerpt{unrooted(review.NewSide, 1, 5)})
	walkthrough.ChangeSet.Repositories = append(walkthrough.ChangeSet.Repositories,
		review.Repository{Root: otherRepository, Base: "merge-base"})
	session := review.NewSession(stubResolver{}, fixedDeriver{lines: changedOn(review.NewSide, 1, 5)})

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedMalformedStep)
	assertDetailContains(t, err, "repository is required when the Change Set has more than one repository")
}

func TestAnExplicitRepositoryIsStillHonoured(t *testing.T) {
	walkthrough := walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5)})
	walkthrough.ChangeSet.Repositories = append(walkthrough.ChangeSet.Repositories,
		review.Repository{Root: otherRepository, Base: "merge-base"})
	session := review.NewSession(stubResolver{}, onlyIn{root: testRepository, lines: changedOn(review.NewSide, 1, 5)})

	err := session.Post(walkthrough)

	if err != nil {
		t.Fatalf("expected the named repository to be accepted, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.Repository != testRepository {
		t.Errorf("expected the Excerpt to keep %q, got %q", testRepository, got.Repository)
	}
}

func TestTheRejectionNamesWhichExcerptIsMissingIt(t *testing.T) {
	walkthrough := walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 5)},
		[]review.Excerpt{at(review.NewSide, 6, 8), unrooted(review.NewSide, 9, 10)},
	)
	walkthrough.ChangeSet.Repositories = append(walkthrough.ChangeSet.Repositories,
		review.Repository{Root: otherRepository, Base: "merge-base"})
	session := review.NewSession(stubResolver{}, fixedDeriver{lines: changedOn(review.NewSide, 1, 10)})

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedMalformedStep)
	if detail := err.Error(); !strings.Contains(detail, "Excerpt 2 of Step 2") {
		t.Errorf("expected the rejection to name Excerpt 2 of Step 2, got %q", detail)
	}
}

func TestAFileAcknowledgedOnceWithTheRootAndOnceWithoutIsStillADoubleClaim(t *testing.T) {
	// "A file is acknowledged once" is checked before the omitted repository is
	// filled in, so the same file named two ways would otherwise read as two.
	walkthrough := walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5)})
	walkthrough.Steps = append(walkthrough.Steps, review.Step{
		Name:             "The lockfile, again",
		Explanation:      "the same claim, written the other way",
		Acknowledgements: []review.Acknowledgement{{Files: []string{"go.sum"}, Reason: "regenerated lockfile"}},
	})
	walkthrough.Steps[0].Acknowledgements = []review.Acknowledgement{{
		Repository: testRepository,
		Files:      []string{"go.sum"},
		Reason:     "regenerated lockfile",
	}}
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines: append(changedOn(review.NewSide, 1, 5), changedFile("go.sum", review.NewSide, 1, 2)...),
	})

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedMalformedStep)
	assertDetailContains(t, err, "a file is acknowledged once")
}

func TestAnAnchorShowsTheResolvedRepository(t *testing.T) {
	// The repository is filled in before the Round is stored, so everything
	// composed from it downstream carries a real root. An Anchor is the case that
	// matters most: it is built to survive being pasted somewhere with no context
	// at all, and a blank root there would name nothing.
	session := review.NewSession(stubResolver{}, fixedDeriver{lines: changedOn(review.NewSide, 1, 5)})
	if err := session.Post(walkthroughShowing([]review.Excerpt{unrooted(review.NewSide, 1, 5)})); err != nil {
		t.Fatalf("expected the post to be accepted, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	comment, err := session.RaiseComment(review.AnchorTarget{
		Start: review.AnchorEndpoint{Side: review.NewSide, Line: 2},
		End:   review.AnchorEndpoint{Side: review.NewSide, Line: 3},
	}, "why this way?")

	if err != nil {
		t.Fatalf("expected the Comment to be raised, got %v", err)
	}
	if comment.Anchor.Repository != testRepository {
		t.Errorf("expected the Anchor to name %q, got %q", testRepository, comment.Anchor.Repository)
	}
}
