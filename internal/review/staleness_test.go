package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// hashingResolver is a stubResolver that also fingerprints files, from a map a
// test can mutate — so a file "changing on disk" between accept and render is a
// one-line edit. A missing entry hashes as an error, standing in for a file that
// cannot be read.
type hashingResolver struct {
	stubResolver
	hashes map[string]string
}

func (h *hashingResolver) Hash(repository, file string) (string, error) {
	if hash, ok := h.hashes[repository+"|"+file]; ok {
		return hash, nil
	}
	return "", fmt.Errorf("no such file %s", file)
}

func TestAStepWhoseFileChangedRefusesToRender(t *testing.T) {
	resolver := &hashingResolver{hashes: map[string]string{"/repos/argus-portal|src/fetch.ts": "v1"}}
	session := review.NewSession(resolver, fixedDeriver{lines: changed("src/fetch.ts", 20, 22)})
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	if session.View().Step.Stale {
		t.Fatal("expected a Step whose file is unchanged to render")
	}

	resolver.hashes["/repos/argus-portal|src/fetch.ts"] = "v2" // the file changes on disk

	step := session.View().Step
	if !step.Stale {
		t.Fatal("expected the Step to be stale once its file changed")
	}
	if len(step.StaleFiles) != 1 || step.StaleFiles[0] != "src/fetch.ts" {
		t.Errorf("expected src/fetch.ts named as stale, got %v", step.StaleFiles)
	}
	if len(step.Excerpts) != 0 {
		t.Errorf("a stale Step shows no code, got %d excerpt(s)", len(step.Excerpts))
	}
}

func TestAStepUntouchedByTheChangeStillRenders(t *testing.T) {
	walkthrough := validWalkthrough()
	walkthrough.Steps = []review.Step{
		{Name: "A", Explanation: "a", Excerpts: []review.Excerpt{{Repository: "/repos/argus-portal", File: "a.ts", Side: review.NewSide, FirstLine: 1, LastLine: 10}}},
		{Name: "B", Explanation: "b", Excerpts: []review.Excerpt{{Repository: "/repos/argus-portal", File: "b.ts", Side: review.NewSide, FirstLine: 1, LastLine: 10}}},
	}
	resolver := &hashingResolver{hashes: map[string]string{
		"/repos/argus-portal|a.ts": "a1",
		"/repos/argus-portal|b.ts": "b1",
	}}
	lines := append(changed("a.ts", 1, 3), changed("b.ts", 1, 3)...)
	session := review.NewSession(resolver, fixedDeriver{lines: lines})
	mustPost(t, session, walkthrough)

	resolver.hashes["/repos/argus-portal|a.ts"] = "a2" // only a.ts changes

	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if !session.View().Step.Stale {
		t.Error("expected Step 1 (a.ts) to be stale")
	}
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	if session.View().Step.Stale {
		t.Error("expected Step 2 (b.ts, untouched) to still render — the blast radius is per file")
	}
}

func TestAStepWhoseFileVanishedIsStale(t *testing.T) {
	resolver := &hashingResolver{hashes: map[string]string{"/repos/argus-portal|src/fetch.ts": "v1"}}
	session := review.NewSession(resolver, fixedDeriver{lines: changed("src/fetch.ts", 20, 22)})
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	delete(resolver.hashes, "/repos/argus-portal|src/fetch.ts") // the file becomes unreadable

	if !session.View().Step.Stale {
		t.Error("expected a Step whose file can no longer be read to be stale")
	}
}

func TestAnchoringAStaleStepIsRefused(t *testing.T) {
	resolver := &hashingResolver{hashes: map[string]string{"/repos/argus-portal|src/fetch.ts": "v1"}}
	session := review.NewSession(resolver, fixedDeriver{lines: changed("src/fetch.ts", 20, 22)})
	mustPost(t, session, validWalkthrough())
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	resolver.hashes["/repos/argus-portal|src/fetch.ts"] = "v2"

	_, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 20, LastLine: 22})

	assertRejected(t, err, review.RejectedStaleContent)
}
