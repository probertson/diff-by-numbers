package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// Every atom of a renamed file is derived under its destination path, so an
// agent writing about the rename under the path the file came from — the natural
// thing — accounts for nothing. Normalisation reads the source as an alias.

const (
	renameSource      = "app.ts"
	renameDestination = "src/app.ts"
)

// roundRenaming is a Round whose single Step is about a rename, with
// the Excerpts and Acknowledgements the caller wants to try.
func walkthroughRenaming(excerpts []review.Excerpt, acknowledgements []review.Acknowledgement) review.Round {
	w := validRound()
	w.Steps = []review.Step{{
		Name:             "Rename the fetch layer",
		Explanation:      "app.ts becomes src/app.ts with no other change",
		Excerpts:         excerpts,
		Acknowledgements: acknowledgements,
	}}
	return w
}

func acknowledging(files ...string) []review.Acknowledgement {
	return []review.Acknowledgement{{
		Repository: testRepository,
		Files:      files,
		Reason:     "a rename with no content change",
	}}
}

// renamedFileDeriver reports a pure rename: one Opaque Change under the
// destination path, and the rename that produced it.
func renamedFileDeriver() fixedDeriver {
	return fixedDeriver{
		opaque:  []review.OpaqueChange{{File: renameDestination, Kind: review.OpaqueRename, Detail: "renamed from " + renameSource}},
		renames: map[string]string{renameSource: renameDestination},
	}
}

func TestAnAcknowledgementNamingOnlyTheOldPathCoversTheRename(t *testing.T) {
	session := review.NewSession(stubResolver{}, renamedFileDeriver())

	err := session.Post(walkthroughRenaming(nil, acknowledging(renameSource)))

	if err != nil {
		t.Fatalf("expected the old path to account for the move, got %v", err)
	}
}

func TestAnAcknowledgementNamingBothPathsCoversTheRenameOnce(t *testing.T) {
	// An agent covering its bases lists the rename's two ends. They are one file,
	// and the manifest must not show it twice.
	session := review.NewSession(stubResolver{}, renamedFileDeriver())

	err := session.Post(walkthroughRenaming(nil, acknowledging(renameSource, renameDestination)))

	if err != nil {
		t.Fatalf("expected both paths together to be accepted, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if entries := session.View().Step.Acknowledgements[0].Entries; len(entries) != 1 {
		t.Errorf("expected the two paths to fold into one entry, got %v", entries)
	}
}

func TestAnOldSideExcerptNamingTheSourceCoversTheRenamedFilesLines(t *testing.T) {
	// A file renamed and edited has hunks, so it is not an Opaque Change: its
	// before-side lines are real Changed Lines, derived under the destination.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:   changedFile(renameDestination, review.OldSide, 1, 3),
		renames: map[string]string{renameSource: renameDestination},
	})

	err := session.Post(walkthroughRenaming([]review.Excerpt{
		{Repository: testRepository, File: renameSource, Side: review.OldSide, FirstLine: 1, LastLine: 3},
	}, nil))

	if err != nil {
		t.Fatalf("expected the old path to cover the moved file's old-side lines, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.File != renameDestination {
		t.Errorf("expected the stored Excerpt to name %q, got %q", renameDestination, got.File)
	}
}

func TestAnOldPathReusedByANewFileGetsNoAlias(t *testing.T) {
	// The branch renamed app.ts to src/app.ts and wrote a different app.ts in its
	// place. The freed-up name now means that new file, so acknowledging it
	// accounts for the new file and says nothing about the rename.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:   changedFile(renameSource, review.NewSide, 1, 4),
		opaque:  []review.OpaqueChange{{File: renameDestination, Kind: review.OpaqueRename, Detail: "renamed from " + renameSource}},
		renames: map[string]string{renameSource: renameDestination},
	})

	err := session.Post(walkthroughRenaming(nil, acknowledging(renameSource)))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, renameDestination)
}

func TestANewSideExcerptNamingTheSourceIsNotAliased(t *testing.T) {
	// The source path is a name in the merge-base, not in the working tree, so
	// only the old side can mean it. Aliasing a new-side range would point the
	// Reviewer at a file that is not there — or, worse, at whatever now is.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:   changedFile(renameDestination, review.NewSide, 1, 4),
		renames: map[string]string{renameSource: renameDestination},
	})

	err := session.Post(walkthroughRenaming([]review.Excerpt{
		{Repository: testRepository, File: renameSource, Side: review.NewSide, FirstLine: 1, LastLine: 4},
	}, nil))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, renameDestination)
}
