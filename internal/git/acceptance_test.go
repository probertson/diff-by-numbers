package git_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// TestABranchOfMechanicalChangesCanCompleteAWalkthrough is the headline scenario
// of #11: an ordinary branch that regenerates a lockfile, deletes a long file and
// touches a binary asset. Without Acknowledgements the Coverage Ledger would make
// it impossible to finish — the binary has no lines to excerpt at all. This drives
// real git derivation into a real review Session and finishes it.
func TestABranchOfMechanicalChangesCanCompleteAWalkthrough(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q", "-b", "main")
	write(t, root, "app.ts", "one\ntwo\nthree\n")
	write(t, root, "package-lock.json", "v1-a\nv1-b\nv1-c\n")
	write(t, root, "longfile.ts", strings.Repeat("boilerplate\n", 60))
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xff})
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "initial")

	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")                         // the real code change
	write(t, root, "package-lock.json", "v2-a\nv2-b\nv2-c\nv2-d\n")             // regenerated lockfile
	run(t, root, "rm", "-q", "longfile.ts")                                     // a long file deleted
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xfe, 0x10}) // binary touched

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatalf("derivation failed: %v", err)
	}
	// Sanity: the binary really has no lines, so nothing but an Acknowledgement
	// could ever account for it.
	if _, ok := opaqueFor(derivation.Opaque, "logo.png"); !ok {
		t.Fatalf("expected logo.png to derive as an Opaque Change, got %+v", derivation.Opaque)
	}

	walkthrough := review.Walkthrough{
		Brief: review.Brief{
			Ask:        "Bump the dependency and refresh the asset",
			Approach:   "One real code line; everything else is mechanical",
			Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "session abc"},
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Range: "main"}}},
		Steps: []review.Step{
			{
				Name:        "The one real change",
				Explanation: "app.ts gains a line",
				Excerpts: []review.Excerpt{
					{Repository: root, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 4},
				},
			},
			{
				Name:        "Everything mechanical",
				Explanation: "regenerated lockfile, removed dead file, swapped the asset",
				Acknowledgements: []review.Acknowledgement{
					{
						Repository: root,
						Files:      []string{"package-lock.json", "longfile.ts", "logo.png"},
						Reason:     "regenerated / deleted / re-exported by the build; nothing to read",
					},
				},
			},
		},
	}

	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected the mechanical branch to be accepted, got %v", err)
	}

	if err := session.GoTo(2); err != nil {
		t.Fatalf("expected to reach the acknowledgement Step, got %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("expected the Walkthrough to finish, got %v", err)
	}

	results, err := session.Results()
	if err != nil {
		t.Fatal(err)
	}
	if !results.Finished {
		t.Error("expected the review to be finished")
	}
}
