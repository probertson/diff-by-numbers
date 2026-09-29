package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// Every CJK rune below draws two terminal cells, so a row that counted runes
// would come out twice as wide as it was allowed.
const (
	cjkWithSpaces = "// 这是 一个 很长的 注释 用来 测试 宽字符 在 终端 中的 换行 行为 是否 正确"
	cjkUnbroken   = "这是一个没有任何空格的很长的中文注释用来测试宽字符的硬换行"
	// Each "é" is an e followed by a combining acute accent: two runes, one cell.
	combining = "café résumé fiancé café résumé fiancé café"
)

func TestWrappedCodeFitsItsWidthInCellsNotRunes(t *testing.T) {
	cases := map[string]string{
		"wide runes with spaces":    cjkWithSpaces,
		"wide runes with no spaces": cjkUnbroken,
		"combining marks":           combining,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			const width = 12

			chunks := wrapCode(text, width, width, 100)

			for i, chunk := range chunks {
				if got := lipgloss.Width(chunk); got > width {
					t.Errorf("row %d %q is %d cells, over the %d it was given", i, chunk, got, width)
				}
			}
			// Wrapping drops only the spaces at a break, never content.
			if got, want := strings.ReplaceAll(strings.Join(chunks, ""), " ", ""), strings.ReplaceAll(text, " ", ""); got != want {
				t.Errorf("wrapping lost content: got %q, want %q", got, want)
			}
		})
	}
}

func TestACombiningMarkWrapsWithTheLetterItSitsOn(t *testing.T) {
	// Room for "cafe" and nothing more by cells: the accent must stay with the
	// e it belongs to rather than start the next row.
	chunks := wrapCode("caféau", 4, 4, 10)

	if chunks[0] != "café" {
		t.Errorf("expected the first row to end with the accented e, got %q", chunks[0])
	}
}

func TestARuneWiderThanTheRowStillWrapsOntoItsOwnRow(t *testing.T) {
	// Nothing fits a one-cell row, so each rune overflows by a cell rather than
	// the wrap never making progress.
	chunks := wrapCode("这是", 1, 1, 10)

	if len(chunks) != 2 || chunks[0] != "这" || chunks[1] != "是" {
		t.Errorf("expected one rune per row, got %q", chunks)
	}
}

func TestTruncatingWideRunesClipsToCells(t *testing.T) {
	for _, width := range []int{1, 2, 5, 10} {
		got := truncateTo(cjkUnbroken, width)

		if w := lipgloss.Width(got); w > width {
			t.Errorf("truncated to %d: %q is %d cells", width, got, w)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("truncated to %d: expected an ellipsis to mark the cut, got %q", width, got)
		}
	}
}

func TestTheStepPaneKeepsWideRunesInsideTheFrame(t *testing.T) {
	step := &daemon.StepWire{
		Name: "Localise the greeting", Explanation: "adds the Chinese strings",
		Excerpts: []daemon.ExcerptWire{{File: "greeting.go", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
			{Number: 1, Text: cjkWithSpaces, Side: "new", Changed: true},
			{Number: 2, Text: cjkUnbroken, Side: "new", Changed: true},
		}}},
	}
	const width = 40
	for _, wrapAll := range []bool{false, true} {
		cur := newStepCursor(step, nil)

		out := renderStep(step, cur, map[string]bool{}, nil, width, 40, false, wrapAll)

		if got := widestLine(out); got > width {
			t.Errorf("wrapAll=%v: the widest row is %d cells, over the %d-cell frame:\n%s", wrapAll, got, width, out)
		}
	}
}
