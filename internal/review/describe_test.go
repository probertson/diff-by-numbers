package review_test

import (
	"reflect"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// describe_changes gives the Authoring Agent dbn's own account of a Change Set,
// computed the way a post over it would be validated, so the agent plans its
// Excerpts from the ledger rather than from a git diff of its own.

// literalDeriver returns one Derivation exactly as given, for any repository.
type literalDeriver struct{ derivation review.Derivation }

func (d literalDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	out := d.derivation
	out.Lines = inRepository(d.derivation.Lines, repo.Root)
	out.Correspondences = nil
	for _, c := range d.derivation.Correspondences {
		c.Repository = repo.Root
		out.Correspondences = append(out.Correspondences, c)
	}
	out.Opaque = nil
	for _, o := range d.derivation.Opaque {
		o.Repository = repo.Root
		out.Opaque = append(out.Opaque, o)
	}
	return out, nil
}

func changeSet() review.ChangeSet {
	return review.ChangeSet{Repositories: []review.Repository{{Root: revRepo, Base: "main"}}}
}

func lines(file string, side review.Side, numbers ...int) []review.ChangedLine {
	var out []review.ChangedLine
	for _, n := range numbers {
		out = append(out, review.ChangedLine{File: file, Side: side, Line: n})
	}
	return out
}

func fileNamed(t *testing.T, d review.ChangeDescription, path string) review.FileDescription {
	t.Helper()
	for _, repository := range d.Repositories {
		for _, file := range repository.Files {
			if file.Path == path {
				return file
			}
		}
	}
	t.Fatalf("no file %q described in %+v", path, d)
	return review.FileDescription{}
}

func TestDescribingAChangeSetGivesEachFilesStatusAndRanges(t *testing.T) {
	var all []review.ChangedLine
	all = append(all, lines("app.ts", review.NewSide, 2, 3, 4, 9)...)
	all = append(all, lines("app.ts", review.OldSide, 2, 3)...)
	all = append(all, lines("fresh.ts", review.NewSide, 1, 2)...)
	all = append(all, lines("gone.ts", review.OldSide, 1, 2, 3)...)
	all = append(all, lines("rewritten.ts", review.NewSide, 5)...)
	session := review.NewSession(stubResolver{}, literalDeriver{review.Derivation{
		Lines: all,
		Correspondences: []review.Correspondence{
			{File: "app.ts", OldFirst: 2, OldLast: 3, NewFirst: 2, NewLast: 4},
			{File: "app.ts", NewFirst: 9, NewLast: 9},
		},
		Opaque: []review.OpaqueChange{
			{File: "logo.png", Kind: review.OpaqueBinary},
			{File: "run.sh", Kind: review.OpaqueMode},
			{File: "lib/b.ts", Kind: review.OpaqueRename},
		},
		Renames: map[string]string{"old.ts": "rewritten.ts", "lib/a.ts": "lib/b.ts"},
		Added:   []string{"fresh.ts"},
		Deleted: []string{"gone.ts"},
		Base:    "abc123",
	}})

	d, err := session.DescribeChanges(changeSet())

	if err != nil {
		t.Fatal(err)
	}
	if len(d.Repositories) != 1 || d.Repositories[0].Root != revRepo || d.Repositories[0].MergeBase != "abc123" {
		t.Fatalf("expected the one repository with its merge-base, got %+v", d.Repositories)
	}
	app := fileNamed(t, d, "app.ts")
	if app.Status != review.FileModified ||
		!reflect.DeepEqual(app.NewRanges, []review.LineRange{{First: 2, Last: 4}, {First: 9, Last: 9}}) ||
		!reflect.DeepEqual(app.OldRanges, []review.LineRange{{First: 2, Last: 3}}) {
		t.Errorf("unexpected app.ts: %+v", app)
	}
	if !reflect.DeepEqual(app.Modifications, []review.Modification{{Old: review.LineRange{First: 2, Last: 3}, New: review.LineRange{First: 2, Last: 4}}}) {
		t.Errorf("expected only the edit with both sides as a modification, got %+v", app.Modifications)
	}
	for path, want := range map[string]review.FileStatus{
		"fresh.ts": review.FileAdded,
		"gone.ts":  review.FileDeleted,
		"logo.png": review.FileBinary,
		"run.sh":   review.FileMode,
	} {
		if got := fileNamed(t, d, path).Status; got != want {
			t.Errorf("%s: expected %q, got %q", path, want, got)
		}
	}
	for path, from := range map[string]string{"rewritten.ts": "old.ts", "lib/b.ts": "lib/a.ts"} {
		if got := fileNamed(t, d, path); got.Status != review.FileRenamed || got.From != from {
			t.Errorf("%s: expected renamed from %s, got %+v", path, from, got)
		}
	}
	if d.RevisionRound {
		t.Error("with no review under way, the next post is a first round")
	}
}

func TestBeforeARevisionRoundTheDescriptionPreMarksWhatThePostWill(t *testing.T) {
	// Line 2 moved since the first round; 1, 3, the deleted old-side line 5 and
	// the binary did not.
	deriver := &roundDeriver{lines: append(changedApp(1, 3), review.ChangedLine{File: "app.ts", Side: review.OldSide, Line: 5}),
		touched: map[string]bool{"app.ts:2": true}}
	logo := review.OpaqueChange{File: "logo.png", Kind: review.OpaqueBinary, Detail: "binary file"}
	session := review.NewSession(&textResolver{text: map[string]string{}}, opaqueRounds{deriver, logo})
	first := appRound([]review.Step{appStep(1, 3), deletionStep(5), acknowledgingRevStep("logo.png")}, nil)
	mustPost(t, session, first)
	handOffWithAComment(t, session)

	d, err := session.DescribeRevision(session.ReviewID(), appRound(nil, nil).ChangeSet)

	if err != nil {
		t.Fatal(err)
	}
	if !d.RevisionRound || d.StillToCover != 1 {
		t.Errorf("expected a Revision Round with 1 line still to cover, got round %v, %d", d.RevisionRound, d.StillToCover)
	}
	app := fileNamed(t, d, "app.ts")
	if !reflect.DeepEqual(app.PreMarkedNew, []review.LineRange{{First: 1, Last: 1}, {First: 3, Last: 3}}) {
		t.Errorf("expected new-side lines 1 and 3 pre-marked, got %+v", app.PreMarkedNew)
	}
	if !reflect.DeepEqual(app.PreMarkedOld, []review.LineRange{{First: 5, Last: 5}}) {
		t.Errorf("expected old-side line 5 pre-marked, got %+v", app.PreMarkedOld)
	}
	if !fileNamed(t, d, "logo.png").PreMarked {
		t.Error("expected the untouched binary to read as already shown")
	}
	// And the post agrees: covering only what is left is enough.
	if err := session.Revise(session.ReviewID(), revising(appRound([]review.Step{appStep(2, 2)}, nil))); err != nil {
		t.Errorf("expected a post covering only the line still to cover to be accepted, got %v", err)
	}
}

// opaqueRounds is a roundDeriver that also reports one Opaque Change.
type opaqueRounds struct {
	*roundDeriver
	opaque review.OpaqueChange
}

func (d opaqueRounds) Derive(repo review.Repository) (review.Derivation, error) {
	derivation, err := d.roundDeriver.Derive(repo)
	opaque := d.opaque
	opaque.Repository = repo.Root
	derivation.Opaque = []review.OpaqueChange{opaque}
	return derivation, err
}

func deletionStep(line int) review.Step {
	return review.Step{Name: "Deletion", Explanation: "e",
		Excerpts: []review.Excerpt{{Repository: revRepo, File: "app.ts", Side: review.OldSide, FirstLine: line, LastLine: line}}}
}

func acknowledgingRevStep(files ...string) review.Step {
	return review.Step{Name: "Mechanical", Explanation: "e",
		Acknowledgements: []review.Acknowledgement{{Repository: revRepo, Files: files, Reason: "generated"}}}
}

func TestDescribingChangesLeavesTheReviewAsItWas(t *testing.T) {
	session, _ := underReview(t)
	raise(t, session, 2, 2, "a point")
	before := session.View()

	if _, err := session.DescribeChanges(appRound(nil, nil).ChangeSet); err != nil {
		t.Fatal(err)
	}

	after := session.View()
	if after.Posting != before.Posting || after.ReviewID != before.ReviewID || after.Position != before.Position ||
		len(after.Comments) != 1 || after.Coverage != before.Coverage {
		t.Errorf("describing changes must not change the review: before %+v, after %+v", before, after)
	}
}

func TestDescribingWithAMalformedBaseReportsTheProblem(t *testing.T) {
	session := review.NewSession(stubResolver{}, emptyDeriver{})
	set := review.ChangeSet{Repositories: []review.Repository{{Root: revRepo, Base: "HEAD~1..HEAD"}}}

	_, err := session.DescribeChanges(set)

	assertRejected(t, err, review.RejectedMalformedBase)
}

func TestDescribingWhenGitCannotDeriveReportsTheProblem(t *testing.T) {
	session := review.NewSession(stubResolver{}, failingDeriver{})

	_, err := session.DescribeChanges(changeSet())

	assertRejected(t, err, review.RejectedDerivationFailed)
}

func TestAnEmptyFileGitReportsNewOrGoneIsStillDescribed(t *testing.T) {
	// An empty file has no Changed Lines, so no atom would ever name it; git
	// still says it came or went, and the agent should hear so.
	session := review.NewSession(stubResolver{}, literalDeriver{review.Derivation{
		Added:   []string{"empty.txt"},
		Deleted: []string{"was-empty.txt"},
	}})

	d, err := session.DescribeChanges(changeSet())

	if err != nil {
		t.Fatal(err)
	}
	if got := fileNamed(t, d, "empty.txt"); got.Status != review.FileAdded || len(got.NewRanges) != 0 {
		t.Errorf("expected empty.txt added with no ranges, got %+v", got)
	}
	if got := fileNamed(t, d, "was-empty.txt"); got.Status != review.FileDeleted {
		t.Errorf("expected was-empty.txt deleted, got %+v", got)
	}
}

func TestARenamedBinaryReadsAsBinaryAndSaysWhereItCameFrom(t *testing.T) {
	session := review.NewSession(stubResolver{}, literalDeriver{review.Derivation{
		Opaque:  []review.OpaqueChange{{File: "img/logo.png", Kind: review.OpaqueBinary}},
		Renames: map[string]string{"logo.png": "img/logo.png"},
	}})

	d, err := session.DescribeChanges(changeSet())

	if err != nil {
		t.Fatal(err)
	}
	if got := fileNamed(t, d, "img/logo.png"); got.Status != review.FileBinary || got.From != "logo.png" {
		t.Errorf("expected a binary renamed from logo.png, got %+v", got)
	}
}
