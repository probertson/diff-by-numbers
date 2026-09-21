package tui

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A file edited after the round was posted is still shown as posted; the
// Reviewer is told they are no longer looking at what is on disk.

const postedWarning = "has changed on disk since this round was posted — you're seeing the posted version"

func stepWithEditedFile() *daemon.StepWire {
	return &daemon.StepWire{
		Number: 1, Name: "Retry", Explanation: "wrap the transport",
		Excerpts: []daemon.ExcerptWire{
			{Repository: "/r", File: "fetch.ts", Side: "new", FirstLine: 1, LastLine: 1, ChangedOnDisk: true,
				Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: "posted line 1", Changed: true}}},
			{Repository: "/r", File: "fetch.ts", Side: "new", FirstLine: 9, LastLine: 9, ChangedOnDisk: true,
				Lines: []daemon.LineWire{{Number: 9, Side: "new", Text: "posted line 9", Changed: true}}},
			{Repository: "/r", File: "other.ts", Side: "new", FirstLine: 1, LastLine: 1,
				Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: "other line", Changed: true}}},
		},
	}
}

func TestAStepWarnsOnceForEachFileThatChangedOnDiskAndStillShowsItsCode(t *testing.T) {
	step := stepWithEditedFile()

	out := renderStep(step, newStepCursor(step, nil), nil, nil, 200, 40, false, false)

	if n := strings.Count(out, "fetch.ts "+postedWarning); n != 1 {
		t.Errorf("expected one warning for fetch.ts, got %d:\n%s", n, out)
	}
	if strings.Contains(out, "other.ts "+postedWarning) {
		t.Errorf("other.ts is unchanged and must not be warned about:\n%s", out)
	}
	if !strings.Contains(out, "posted line 9") {
		t.Errorf("expected the posted code still shown:\n%s", out)
	}
}

func TestAnExpansionWarnsAboutAFileThatChangedOnDisk(t *testing.T) {
	step := &daemon.StepWire{
		Number: 1, Name: "Mechanical", Explanation: "regenerated",
		Acknowledgements: []daemon.AcknowledgementWire{{Reason: "generated", Entries: []daemon.AcknowledgedFileWire{{Repository: "/r", File: "gen.ts", ChangedLines: 1, Change: "modified"}}}},
	}
	expanded := map[int][]daemon.ExcerptWire{0: {{
		Repository: "/r", File: "gen.ts", Side: "new", FirstLine: 1, LastLine: 1, ChangedOnDisk: true,
		Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: "generated line", Changed: true}},
	}}}
	cur := newStepCursor(step, expanded)

	rows := paneRows(step, cur, nil, nil, 200, 4, false, false)

	var warned bool
	for _, row := range rows {
		if strings.Contains(row.text, "gen.ts "+postedWarning) {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected the expansion to warn about gen.ts, got %+v", rows)
	}
}
