package daemon

import (
	"embed"
	"fmt"
	"html/template"
	"slices"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/present"
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
	// DiagramsURL is the diagrams frame, which draws the page's Mermaid
	// diagrams (browserdiagram.go).
	DiagramsURL string
	Style       template.CSS
	Script      template.JS
	Stop        stopData
}

// stopData is what the page draws for where the Reviewer is: a Step, or a
// one-line notice for anywhere else. Key names the stop, so the script can tell
// a move to another stop (scroll to the top) from a redraw of the same one.
type stopData struct {
	Key    string
	Notice string
	Step   *stepData
	Intro  *introData
	// Following is the TUI's cursor being on something this stop draws, which
	// it brightens and dims the rest around (#187).
	Following bool
}

// pageContext is what a page is drawn with beside the view: where its
// Documents' images are found, the screen the TUI last reported showing, and
// where the TUI last said its cursor is (nil before it has said).
type pageContext struct {
	images documentImages
	screen Screen
	cursor *CursorWire
}

// excerptContext is what one Excerpt is drawn with: where its Document's
// images are found, its Step and its index among the Step's own Excerpts,
// which name the places on it (placeOfBlock, placeOfLine), and the place the
// TUI's cursor is at, which its rows there are marked as.
type excerptContext struct {
	images      documentImagesAt
	step, index int
	cursor      string
}

func (ctx pageContext) excerpt(step, index int, excerpt ExcerptWire) excerptContext {
	x := excerptContext{images: ctx.images.at(excerpt.Repository, excerpt.File), step: step, index: index}
	if ctx.cursor != nil {
		x.cursor = ctx.cursor.place()
	}
	return x
}

// blockPlace is the place of one of the Excerpt's blocks.
func (x excerptContext) blockPlace(block int) string { return placeOfBlock(x.step, x.index, block) }

// rowPlaces are the places a row of the Excerpt is at: its block, if it is in
// one, and its own line. A signpost is at none.
func (x excerptContext) rowPlaces(line LineWire, excerpt ExcerptWire) []string {
	if line.Signpost {
		return nil
	}
	var places []string
	if line.Block > 0 {
		places = append(places, x.blockPlace(line.Block))
	}
	return append(places, placeOfLine(x.step, x.index, sideOf(line, excerpt), line.Number))
}

// onCursor reports whether the TUI's cursor is at any of places.
func (x excerptContext) onCursor(places ...string) bool {
	return x.cursor != "" && slices.Contains(places, x.cursor)
}

// introData is a Chapter's intro, as the TUI draws it: the Chapter's number and
// name, where its Steps sit among the Round's, the agent's intro, and the
// Steps it holds.
type introData struct {
	Heading string
	Range   string
	Intro   template.HTML
	Steps   []introStepData
}

type introStepData struct {
	Number int
	Name   string
}

type stepData struct {
	Number int
	Count  int
	// Seen and Total are the changed lines the Reviewer has seen of all the
	// review shows, as the TUI's header counts them.
	Seen, Total int
	// Chapter names the Step's Chapter by number and name, "" in a Round
	// without Chapters.
	Chapter     string
	Name        string
	Explanation template.HTML
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
	Text   template.HTML
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
	Index int
	Label string
	// Path is the heading path a Document Excerpt opens under, which its file
	// header names after the file, as the TUI's does.
	Path          []string
	Problem       string
	ChangedOnDisk bool
	Pieces        []pieceData
	// following is the TUI's cursor being on a row drawn of the Excerpt.
	following bool
}

type pieceData struct {
	Rows     []rowData
	HTML     template.HTML
	Comments []commentData
}

// rowData is one row in the source view's style: a sign, a line number, and
// the text, with what the edit changed coloured. Places are the places the
// TUI's cursor finds it at: its line, and the block it is in.
type rowData struct {
	Number    int
	Side      string
	Block     int
	Places    string
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

// newPageData lays out a review's page from the view the TUI is sent and what
// it last reported.
func newPageData(label string, view ViewWire, ctx pageContext) pageData {
	base := PagePath(view.ReviewID)
	title := "dbn"
	if label != "" {
		title = label + " · dbn"
	}
	return pageData{
		Title:       title,
		StopURL:     base + "/stop",
		EventsURL:   base + "/events",
		DiagramsURL: diagramsPath,
		Style:       pageStyle,
		Script:      pageScript,
		Stop:        newStopData(view, ctx),
	}
}

// newStopData is the stop the Reviewer is at: a Step or a Chapter's intro, or
// a notice of where they are for anywhere else. A review handed off is held
// at its last Step, but the Reviewer has left it, so the page says it was
// handed off, which dbn knows without the TUI saying.
func newStopData(view ViewWire, ctx pageContext) stopData {
	switch {
	case ctx.screen == ScreenInbox:
		return stopData{Key: "inbox", Notice: "At the Inbox. This page shows this review again when you open it."}
	case view.Concluded:
		return stopData{Key: "handed-off", Notice: "Handed off. The review is complete."}
	case view.Finished:
		return stopData{Key: "handed-off", Notice: "Handed off to your agent. This page follows its Revision Round when it arrives."}
	case ctx.screen == ScreenConclusion:
		return stopData{Key: "conclusion", Notice: "At the end of the review. This page shows each Step as you reach it."}
	}
	if view.Step != nil {
		step := newStepData(view, ctx)
		following := slices.ContainsFunc(step.Excerpts, func(e excerptData) bool { return e.following })
		return stopData{Key: fmt.Sprintf("step-%d", view.Position), Step: step, Following: following}
	}
	if chapter, ok := chapterOf(view, view.ChapterIntro); ok {
		return stopData{Key: fmt.Sprintf("intro-%d", view.ChapterIntro), Intro: newIntroData(view, chapter)}
	}
	return stopData{Key: "overview", Notice: "On the Overview. This page shows each Step as you reach it."}
}

func newIntroData(view ViewWire, chapter ChapterWire) *introData {
	data := &introData{
		Heading: chapterHeading(chapter),
		Range:   fmt.Sprintf("%s of %d", present.StepRange(chapter.FirstStep, chapter.LastStep), view.StepCount),
		Intro:   renderProse(chapter.Intro),
	}
	for step := chapter.FirstStep; step <= chapter.LastStep && step <= len(view.StepNames); step++ {
		data.Steps = append(data.Steps, introStepData{Number: step, Name: view.StepNames[step-1]})
	}
	return data
}

func newStepData(view ViewWire, ctx pageContext) *stepData {
	step := view.Step
	data := &stepData{
		Number:      step.Number,
		Count:       view.StepCount,
		Seen:        view.Coverage.Seen,
		Total:       view.Coverage.Total,
		Name:        step.Name,
		Explanation: renderProse(step.Explanation),
		Oversize:    step.OversizeJustification,
	}
	if chapter, ok := chapterOf(view, step.Chapter); ok {
		data.Chapter = chapterHeading(chapter)
	}
	for _, question := range step.Questions {
		data.Questions = append(data.Questions, questionData{Text: renderProse(question.Text), Answer: question.Answer})
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
		data.Excerpts = append(data.Excerpts, newExcerptData(excerpt, own, placed, ctx.excerpt(view.Position, ei, excerpt)))
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

// chapterOf is the Round's Chapter numbered n, from 1, and whether there is
// one.
func chapterOf(view ViewWire, n int) (ChapterWire, bool) {
	if n < 1 || n > len(view.Chapters) {
		return ChapterWire{}, false
	}
	return view.Chapters[n-1], true
}

// chapterHeading names a Chapter by number and name, as the TUI does.
func chapterHeading(chapter ChapterWire) string {
	return present.ChapterHeading(chapter.Number, chapter.Name)
}

func toCommentData(comment CommentWire) commentData {
	return commentData{ID: comment.ID, Location: comment.Location, Note: comment.Note}
}

// newExcerptData lays an Excerpt out as pieces and puts each Comment's note
// after the piece holding the last row it covers, marking placed the Comments
// it found a row for.
func newExcerptData(excerpt ExcerptWire, comments []CommentWire, placed map[int]bool, x excerptContext) excerptData {
	data := excerptData{
		Index:         x.index,
		Label:         excerptLabel(excerpt),
		Path:          openingPath(excerpt),
		Problem:       excerpt.Problem,
		ChangedOnDisk: excerpt.ChangedOnDisk,
	}
	if excerpt.Problem != "" {
		return data
	}
	pieces, pieceOfRow, following, inFlow := layoutExcerpt(excerpt, comments, x)
	last := map[int]int{}
	for i, line := range excerpt.Lines {
		for _, comment := range comments {
			if covers(comment, excerpt, line) {
				last[comment.ID] = pieceOfRow[i]
			}
		}
	}
	for _, comment := range comments {
		if inFlow[comment.ID] {
			placed[comment.ID] = true
			continue
		}
		if piece, ok := last[comment.ID]; ok && piece < len(pieces) {
			pieces[piece].Comments = append(pieces[piece].Comments, toCommentData(comment))
			placed[comment.ID] = true
		}
	}
	data.Pieces, data.following = pieces, following
	return data
}

// excerptLabel heads an Excerpt with its file and the lines it shows on each
// side, its own first: the old lines an edit replaced or removed are drawn
// among the new ones, so a range alone would leave unsaid which file's lines
// it counts, and miss the others. An Excerpt that cannot be shown names the
// range it asked for.
func excerptLabel(excerpt ExcerptWire) string {
	type span struct{ first, last int }
	spans := map[string]*span{}
	for _, line := range excerpt.Lines {
		if line.Signpost || line.Number < 1 {
			continue
		}
		side := sideOf(line, excerpt)
		if s, ok := spans[side]; ok {
			s.first, s.last = min(s.first, line.Number), max(s.last, line.Number)
		} else {
			spans[side] = &span{line.Number, line.Number}
		}
	}
	if len(spans) == 0 {
		spans[excerpt.Side] = &span{excerpt.FirstLine, excerpt.LastLine}
	}
	label := excerpt.File
	for _, side := range []string{excerpt.Side, otherSideOf(excerpt.Side)} {
		if s, ok := spans[side]; ok {
			label += " · " + side + " " + present.LineRange(s.first, s.last)
		}
	}
	return label
}

// otherSideOf is the side an Excerpt is not on.
func otherSideOf(side string) string {
	if side == "old" {
		return "new"
	}
	return "old"
}

// openingPath is the heading path of an Excerpt's first row, as dbn decides it:
// none for code.
func openingPath(excerpt ExcerptWire) []string {
	for _, line := range excerpt.Lines {
		if !line.Signpost {
			return line.Path
		}
	}
	return nil
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
// row went into, and whether it drew a row the TUI's cursor is on. Code is all source rows. In a Document, each stretch of new
// and unchanged blocks is rendered, with each edited or removed block in it
// drawn as track changes; a row in no block is drawn as source.
//
// The rows at the place the TUI's cursor is at are marked as its. A Comment
// whose last row is in a block drawn in a stretch has its note drawn beneath
// that block, in the Document's flow, as the TUI draws it; inFlow names those
// Comments, whose notes need no other place.
func layoutExcerpt(excerpt ExcerptWire, comments []CommentWire, x excerptContext) (pieces []pieceData, pieceOfRow []int, following bool, inFlow map[int]bool) {
	if drawsDiagramFile(excerpt) {
		pieces, pieceOfRow, following = layoutDiagramFile(excerpt, comments, x)
		return pieces, pieceOfRow, following, nil
	}
	inFlow = map[int]bool{}
	lastRow := map[int]int{}
	for i, line := range excerpt.Lines {
		for _, comment := range comments {
			if covers(comment, excerpt, line) {
				lastRow[comment.ID] = i
			}
		}
	}
	// notesAt are the notes of the Comments whose last row is row i, now
	// drawn in the flow.
	notesAt := func(i int) []commentData {
		var notes []commentData
		for _, comment := range comments {
			if last, ok := lastRow[comment.ID]; ok && last == i {
				notes = append(notes, toCommentData(comment))
				inFlow[comment.ID] = true
			}
		}
		return notes
	}
	pieceOfRow = make([]int, len(excerpt.Lines))
	begun := map[int]bool{} // the edited or removed blocks drawn so far
	// drawn notes whether a row drawn is the cursor's.
	drawn := func(cursor bool) bool {
		following = following || cursor
		return cursor
	}
	commentedBlocks := map[int]bool{}
	for _, line := range excerpt.Lines {
		for _, comment := range comments {
			if line.Block > 0 && covers(comment, excerpt, line) {
				commentedBlocks[line.Block] = true
			}
		}
	}

	var stretch []documentRow
	var rows []rowData
	var pending []int // the rows of the piece being gathered
	flush := func() {
		switch {
		case len(stretch) > 0 && excerpt.Plain:
			pieces = append(pieces, pieceData{HTML: renderPlain(stretch)})
		case len(stretch) > 0:
			pieces = append(pieces, pieceData{HTML: renderDocument(stretch, x.images)})
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
			stretch = append(stretch, documentRow{
				text: line.Text, block: line.Block, commented: commentedBlocks[line.Block],
				place: x.blockPlace(line.Block), cursor: drawn(x.onCursor(x.blockPlace(line.Block))),
				treatment: excerpt.Blocks[line.Block-1].Treatment, widened: line.Widened,
				diagram: excerpt.Blocks[line.Block-1].Diagram, cutInto: !shownWhole(line.Block, excerpt), notes: notesAt(i),
				frontmatter: excerpt.Blocks[line.Block-1].Kind == string(review.BlockFrontmatter),
			})
		case edited(line, excerpt):
			// An edited or removed block is drawn as track changes in the
			// stretch it sits in, from its own rows; the lines an edit
			// replaced draw nothing, as their struck words are in those rows.
			// It is drawn where it begins, on either side, as the TUI lays it
			// out: an edit's replaced lines can begin above blocks the change
			// removed, and its own rows come after them.
			if begun[line.Block] {
				break
			}
			begun[line.Block] = true
			if len(rows) > 0 {
				flush()
			}
			stretch = append(stretch, trackedBlock(excerpt, i, x, commentedBlocks[line.Block], notesAt, drawn)...)
		case len(excerpt.Blocks) > 0 && blankBetweenBlocks(line, excerpt):
			// A blank line between blocks belongs to the stretch it sits in,
			// where it keeps two paragraphs apart; on its own it draws nothing.
			switch {
			case len(stretch) > 0 && !excerpt.Plain:
				stretch = append(stretch, documentRow{text: line.Text})
			case len(rows) > 0:
				row := sourceRow(line, excerpt, comments, x)
				drawn(row.Cursor)
				rows = append(rows, row)
			}
		default:
			if len(stretch) > 0 {
				flush()
			}
			row := sourceRow(line, excerpt, comments, x)
			drawn(row.Cursor)
			rows = append(rows, row)
		}
		pending = append(pending, i)
	}
	flush()
	return pieces, pieceOfRow, following, inFlow
}

// trackedBlock is the rows an edited or removed block is drawn from, the
// block whose first row is row first: its rows on its own side, in order,
// the last carrying the notes of every Comment whose last row is any of the
// block's, either side.
func trackedBlock(excerpt ExcerptWire, first int, x excerptContext, commented bool, notesAt func(int) []commentData, drawn func(bool) bool) []documentRow {
	number := excerpt.Lines[first].Block
	block := excerpt.Blocks[number-1]
	place := x.blockPlace(number)
	var own []documentRow
	var rows []int // every row of the block, either side
	for i := first; i < len(excerpt.Lines); i++ {
		line := excerpt.Lines[i]
		if line.Block != number || !edited(line, excerpt) {
			continue
		}
		rows = append(rows, i)
		if sideOf(line, excerpt) != block.Side {
			continue
		}
		row := trackedRow(line, block, commented)
		row.place, row.cursor = place, drawn(x.onCursor(place))
		row.diagram, row.cutInto = block.Diagram, !shownWhole(number, excerpt)
		own = append(own, row)
	}
	// A block drawn from no rows has nowhere for notes, which then follow
	// the piece, as notes on any other row in no stretch do.
	if len(own) == 0 {
		return nil
	}
	for _, i := range rows {
		own[len(own)-1].notes = append(own[len(own)-1].notes, notesAt(i)...)
	}
	return own
}

// drawsDiagramFile reports whether an Excerpt of a diagram file is drawn as
// its diagram: whenever dbn could read the file, however much of it the
// Excerpt shows, since dbn holds the diagram's whole source to draw from.
func drawsDiagramFile(excerpt ExcerptWire) bool {
	return excerpt.DiagramFile != nil && excerpt.DiagramFile.Read
}

// layoutDiagramFile lays out an Excerpt showing a diagram file, or any part of
// one, as a
// fenced diagram is drawn: a new or unchanged one as a diagram container, and
// an edited or removed one as renderDiagramChange draws it, the file's rows
// the edit beneath. What holds the diagram is at the places of every row, as a
// fenced diagram is at its block's: a Comment on any row is on the diagram,
// and the TUI's cursor on any row is on it.
func layoutDiagramFile(excerpt ExcerptWire, comments []CommentWire, x excerptContext) ([]pieceData, []int, bool) {
	file := excerpt.DiagramFile
	var places []string
	var edit []documentRow
	commented := false
	for _, line := range excerpt.Lines {
		places = append(places, x.rowPlaces(line, excerpt)...)
		edit = append(edit, diagramFileEditRow(line, excerpt))
		for _, comment := range comments {
			commented = commented || covers(comment, excerpt, line)
		}
	}
	cursor := x.onCursor(places...)
	place := strings.Join(places, " ")

	var held template.HTML
	switch file.Treatment {
	case string(review.TreatmentEdited), string(review.TreatmentRemoved):
		held = renderDiagramChange(changedDiagram{
			sources: file.Sources, treatment: file.Treatment, place: place,
			commented: commented, cursor: cursor, edit: edit,
		})
	default:
		var classes []string
		if commented {
			classes = append(classes, "commented")
		}
		if cursor {
			classes = append(classes, "cursor")
		}
		attributes := ` data-place="` + template.HTMLEscapeString(place) + `"`
		if file.Treatment != string(review.TreatmentContext) {
			attributes += ` data-treatment="` + template.HTMLEscapeString(file.Treatment) + `"`
		}
		var out strings.Builder
		writeDiagram(&out, strings.Join(classes, " "), func() { out.WriteString(attributes) }, file.Sources.After)
		held = template.HTML(out.String())
	}
	return []pieceData{{HTML: held}}, make([]int, len(excerpt.Lines)), cursor
}

// edited reports whether a row of a Document Excerpt is in a block an edit
// changed or removed, which the page draws as track changes.
func edited(line LineWire, excerpt ExcerptWire) bool {
	if line.Block < 1 || line.Block > len(excerpt.Blocks) || line.Signpost {
		return false
	}
	treatment := excerpt.Blocks[line.Block-1].Treatment
	return treatment == string(review.TreatmentEdited) || treatment == string(review.TreatmentRemoved)
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
	case block.Kind == string(review.BlockFrontmatter) && excerpt.Plain:
		return false
	case block.Treatment != string(review.TreatmentNew) && block.Treatment != string(review.TreatmentContext):
		return false
	}
	return sideOf(line, excerpt) == block.Side
}

// shownWhole reports whether an Excerpt shows every line of one of its blocks
// (numbered from 1), on the block's own side.
func shownWhole(block int, excerpt ExcerptWire) bool {
	b := excerpt.Blocks[block-1]
	shown := 0
	for _, line := range excerpt.Lines {
		if line.Block == block && !line.Signpost && sideOf(line, excerpt) == b.Side {
			shown++
		}
	}
	return shown == b.LastLine-b.FirstLine+1
}

// blankBetweenBlocks reports whether a row is a blank line in no block, on the
// side the Document is read on (a new file's blank lines are added ones).
func blankBetweenBlocks(line LineWire, excerpt ExcerptWire) bool {
	return line.Block == 0 && !line.Signpost && strings.TrimSpace(line.Text) == "" &&
		!(line.Changed && present.RemovedSide(sideOf(line, excerpt)))
}

// sourceRow draws a row as the TUI's source view does: a sign and colour for a
// changed row, with only the text the edit changed coloured where dbn says
// which, and never struck through.
func sourceRow(line LineWire, excerpt ExcerptWire, comments []CommentWire, x excerptContext) rowData {
	places := x.rowPlaces(line, excerpt)
	row := rowData{Number: line.Number, Side: sideOf(line, excerpt), Block: line.Block, Sign: " ", Class: "context",
		Places: strings.Join(places, " "), Cursor: x.onCursor(places...)}
	for _, comment := range comments {
		if covers(comment, excerpt, line) {
			row.Commented = true
		}
	}
	switch {
	case line.Signpost:
		row.Class, row.Number = "signpost", 0
	case line.Changed && present.RemovedSide(sideOf(line, excerpt)):
		row.Sign, row.Class = "-", "removed"
	case line.Changed:
		row.Sign, row.Class = "+", "added"
	case line.Widened:
		row.Class = "widened"
	}
	row.Parts = parts(line.Text, line.Changed && !line.Signpost, line.Emphasis)
	return row
}

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
