package daemon

import (
	"embed"
	"fmt"
	"html/template"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// browserAssets are the page's templates, style and script, embedded so dbn
// stays one binary with no build step.
//
//go:embed browser
var browserAssets embed.FS

var pageTemplates = template.Must(template.New("").ParseFS(browserAssets, "browser/*.html"))

// pageStyle and pageScript are inlined into the page, so it is one request.
var (
	pageStyle  = template.CSS(mustAsset("browser/page.css"))
	pageScript = template.JS(mustAsset("browser/page.js"))
)

func mustAsset(name string) string {
	content, err := browserAssets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(content)
}

// pageData is the whole page: its title, where its script reads from, and
// the stop it opens on.
type pageData struct {
	Title     string
	StopURL   string
	EventsURL string
	CursorURL string
	Style     template.CSS
	Script    template.JS
	Stop      stopData
}

// stopData is what the page draws for where the Reviewer is: a Step, or a
// one-line notice for anywhere else. Key names the stop, so the script can tell
// a move to another stop (scroll to the top) from a redraw of the same one.
type stopData struct {
	Key    string
	Notice string
	Step   *stepData
	// Following is the TUI's cursor being on something this stop draws, which
	// it brightens and dims the rest around (#187).
	Following bool
}

type stepData struct {
	Number      int
	Count       int
	Name        string
	Explanation string
	Oversize    string
	Questions   []questionData
	Excerpts    []excerptData
	// Acknowledgements are the Step's Acknowledgements, as the manifests the
	// TUI shows before any is expanded.
	Acknowledgements []acknowledgementData
	// Comments are the Step's Comments that sit on nothing the page draws.
	Comments []commentData
}

type questionData struct {
	Text   string
	Answer string
}

type acknowledgementData struct {
	Reason   string
	Entries  []AcknowledgedFileWire
	Comments []commentData
}

type commentData struct {
	ID       int
	Location string
	Note     string
}

// excerptData is one Excerpt, drawn as a run of pieces: source rows, or a
// stretch of a Document rendered as HTML. A Comment's note follows the piece
// holding the last row it covers.
type excerptData struct {
	// Index is the Excerpt's place among the Step's own, by which the TUI's
	// cursor names it.
	Index         int
	Label         string
	Problem       string
	ChangedOnDisk bool
	Pieces        []pieceData
}

type pieceData struct {
	Rows     []rowData
	HTML     template.HTML
	Comments []commentData
}

// rowData is one row in the source view's style: a sign, a line number, and
// the text, with what the edit changed coloured. Side, Number and Block are
// how the TUI's cursor names the row: by its line, or by the block it is in.
type rowData struct {
	Number    int
	Side      string
	Block     int
	Sign      string
	Class     string
	Parts     []partData
	Commented bool
	Cursor    bool
}

type partData struct {
	Text    string
	Changed bool
}

// newPageData lays out a review's page from the view the TUI is sent.
// cursor is where the TUI last said its cursor is, or nil.
func newPageData(label string, view ViewWire, cursor *CursorWire) pageData {
	base := PagePath(view.ReviewID)
	title := "dbn"
	if label != "" {
		title = label + " · dbn"
	}
	return pageData{
		Title:     title,
		StopURL:   base + "/stop",
		EventsURL: base + "/events",
		CursorURL: base + "/cursor",
		Style:     pageStyle,
		Script:    pageScript,
		Stop:      newStopData(view, cursor),
	}
}

// newStopData is the stop the Reviewer is at. Anywhere but a Step is, for
// now, a notice that the page is following (#190 draws the rest). The TUI's
// cursor is drawn only on the Step it was reported on.
func newStopData(view ViewWire, cursor *CursorWire) stopData {
	switch {
	case view.Step != nil:
		if cursor != nil && cursor.Step != view.Position {
			cursor = nil
		}
		return stopData{Key: fmt.Sprintf("step-%d", view.Position), Step: newStepData(view, cursor),
			Following: cursor != nil && cursor.Excerpt >= 0}
	case view.ChapterIntro > 0:
		return stopData{Key: fmt.Sprintf("intro-%d", view.ChapterIntro),
			Notice: fmt.Sprintf("On Chapter %d's intro. This page shows each Step as you reach it.", view.ChapterIntro)}
	}
	return stopData{Key: "overview", Notice: "On the Overview. This page shows each Step as you reach it."}
}

func newStepData(view ViewWire, cursor *CursorWire) *stepData {
	step := view.Step
	data := &stepData{
		Number:      step.Number,
		Count:       view.StepCount,
		Name:        step.Name,
		Explanation: step.Explanation,
		Oversize:    step.OversizeJustification,
	}
	for _, question := range step.Questions {
		data.Questions = append(data.Questions, questionData{Text: question.Text, Answer: question.Answer})
	}

	var own []CommentWire
	byAck := map[int][]commentData{}
	for _, comment := range view.Comments {
		if comment.Step != step.Number || comment.Stepless() != "" {
			continue
		}
		if comment.Acknowledgement != nil {
			byAck[*comment.Acknowledgement] = append(byAck[*comment.Acknowledgement], toCommentData(comment))
			continue
		}
		own = append(own, comment)
	}

	placed := map[int]bool{}
	for ei, excerpt := range step.Excerpts {
		data.Excerpts = append(data.Excerpts, newExcerptData(ei, excerpt, own, placed, cursor.in(ei)))
	}
	for _, comment := range own {
		if !placed[comment.ID] {
			data.Comments = append(data.Comments, toCommentData(comment))
		}
	}
	for i, ack := range step.Acknowledgements {
		data.Acknowledgements = append(data.Acknowledgements, acknowledgementData{
			Reason: ack.Reason, Entries: ack.Entries, Comments: byAck[i],
		})
	}
	return data
}

func toCommentData(comment CommentWire) commentData {
	return commentData{ID: comment.ID, Location: comment.Location, Note: comment.Note}
}

// newExcerptData lays an Excerpt out as pieces and puts each Comment's note
// after the piece holding the last row it covers, marking placed the Comments
// it found a row for. cursor is the TUI's cursor if it is in this Excerpt.
func newExcerptData(index int, excerpt ExcerptWire, comments []CommentWire, placed map[int]bool, cursor *CursorWire) excerptData {
	data := excerptData{
		Index:         index,
		Label:         fmt.Sprintf("%s:%d–%d", excerpt.File, excerpt.FirstLine, excerpt.LastLine),
		Problem:       excerpt.Problem,
		ChangedOnDisk: excerpt.ChangedOnDisk,
	}
	if excerpt.Problem != "" {
		return data
	}
	pieces, pieceOfRow := layoutExcerpt(excerpt, comments, cursor)
	last := map[int]int{}
	for i, line := range excerpt.Lines {
		for _, comment := range comments {
			if covers(comment, excerpt, line) {
				last[comment.ID] = pieceOfRow[i]
			}
		}
	}
	for _, comment := range comments {
		if piece, ok := last[comment.ID]; ok && piece < len(pieces) {
			pieces[piece].Comments = append(pieces[piece].Comments, toCommentData(comment))
			placed[comment.ID] = true
		}
	}
	data.Pieces = pieces
	return data
}

func covers(comment CommentWire, excerpt ExcerptWire, line LineWire) bool {
	return !line.Signpost && comment.Covers(excerpt.Repository, excerpt.File, sideOf(line, excerpt), line.Number)
}

// sideOf is a row's own side, or the Excerpt's for a row that names none.
func sideOf(line LineWire, excerpt ExcerptWire) string {
	if line.Side != "" {
		return line.Side
	}
	return excerpt.Side
}

// layoutExcerpt cuts an Excerpt's rows into pieces, returning the piece each
// row went into. Code is all source rows. In a Document, each stretch of new
// and unchanged blocks is rendered; an edited or removed block, and any row in
// no block, is drawn as source until the page draws edits as the TUI does
// (#188).
//
// The rows cursor is on, when it is in this Excerpt, are marked as its.
func layoutExcerpt(excerpt ExcerptWire, comments []CommentWire, cursor *CursorWire) ([]pieceData, []int) {
	pieceOfRow := make([]int, len(excerpt.Lines))
	commentedBlocks := map[int]bool{}
	for _, line := range excerpt.Lines {
		for _, comment := range comments {
			if line.Block > 0 && covers(comment, excerpt, line) {
				commentedBlocks[line.Block] = true
			}
		}
	}

	var pieces []pieceData
	var stretch []documentRow
	var rows []rowData
	var pending []int // the rows of the piece being gathered
	flush := func() {
		switch {
		case len(stretch) > 0 && excerpt.Plain:
			pieces = append(pieces, pieceData{HTML: renderPlain(stretch)})
		case len(stretch) > 0:
			pieces = append(pieces, pieceData{HTML: renderDocument(stretch)})
		case len(rows) > 0:
			pieces = append(pieces, pieceData{Rows: rows})
		default:
			return
		}
		for _, i := range pending {
			pieceOfRow[i] = len(pieces) - 1
		}
		stretch, rows, pending = nil, nil, nil
	}
	for i, line := range excerpt.Lines {
		switch {
		case rendered(line, excerpt):
			if len(rows) > 0 {
				flush()
			}
			stretch = append(stretch, documentRow{text: line.Text, block: line.Block, commented: commentedBlocks[line.Block],
				cursor: cursor.on(line, excerpt)})
		case len(excerpt.Blocks) > 0 && blankBetweenBlocks(line, excerpt):
			// A blank line between blocks belongs to the stretch it sits in,
			// where it keeps two paragraphs apart; on its own it draws nothing.
			switch {
			case len(stretch) > 0 && !excerpt.Plain:
				stretch = append(stretch, documentRow{text: line.Text})
			case len(rows) > 0:
				rows = append(rows, sourceRow(line, excerpt, comments))
			}
		default:
			if len(stretch) > 0 {
				flush()
			}
			row := sourceRow(line, excerpt, comments)
			row.Cursor = cursor.on(line, excerpt)
			rows = append(rows, row)
		}
		pending = append(pending, i)
	}
	flush()
	return pieces, pieceOfRow
}

// rendered reports whether a row of a Document Excerpt is drawn rendered: a
// row on its block's own side, in a new or unchanged block of the Document's
// text (frontmatter stays source).
func rendered(line LineWire, excerpt ExcerptWire) bool {
	if line.Block < 1 || line.Block > len(excerpt.Blocks) || line.Signpost {
		return false
	}
	block := excerpt.Blocks[line.Block-1]
	switch {
	case block.Kind == string(review.BlockFrontmatter):
		return false
	case block.Treatment != string(review.TreatmentNew) && block.Treatment != string(review.TreatmentContext):
		return false
	}
	return sideOf(line, excerpt) == block.Side
}

// blankBetweenBlocks reports whether a row is a blank line in no block, on the
// side the Document is read on (a new file's blank lines are added ones).
func blankBetweenBlocks(line LineWire, excerpt ExcerptWire) bool {
	return line.Block == 0 && !line.Signpost && strings.TrimSpace(line.Text) == "" &&
		!(line.Changed && removedSide(sideOf(line, excerpt)))
}

// sourceRow draws a row as the TUI's source view does: a sign and colour for a
// changed row, with only the text the edit changed coloured where dbn says
// which, and never struck through.
func sourceRow(line LineWire, excerpt ExcerptWire, comments []CommentWire) rowData {
	row := rowData{Number: line.Number, Side: sideOf(line, excerpt), Block: line.Block, Sign: " ", Class: "context"}
	for _, comment := range comments {
		if covers(comment, excerpt, line) {
			row.Commented = true
		}
	}
	switch {
	case line.Signpost:
		row.Class, row.Number = "signpost", 0
	case line.Changed && removedSide(sideOf(line, excerpt)):
		row.Sign, row.Class = "-", "removed"
	case line.Changed:
		row.Sign, row.Class = "+", "added"
	case line.Widened:
		row.Class = "widened"
	}
	row.Parts = parts(line.Text, line.Changed && !line.Signpost, line.Emphasis)
	return row
}

func removedSide(side string) bool { return side == "old" || side == "previous" }

// parts cuts a row's text by its emphasis (rune ranges). A changed row with no
// emphasis is changed throughout.
func parts(text string, changed bool, emphasis [][2]int) []partData {
	if !changed {
		return []partData{{Text: text}}
	}
	if len(emphasis) == 0 {
		return []partData{{Text: text, Changed: true}}
	}
	runes := []rune(text)
	var out []partData
	at := 0
	for _, r := range emphasis {
		start, end := min(max(r[0], at), len(runes)), min(r[1], len(runes))
		if start > at {
			out = append(out, partData{Text: string(runes[at:start])})
		}
		if end > start {
			out = append(out, partData{Text: string(runes[start:end]), Changed: true})
		}
		at = max(at, end)
	}
	if at < len(runes) {
		out = append(out, partData{Text: string(runes[at:])})
	}
	return out
}
