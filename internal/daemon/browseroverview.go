package daemon

import (
	"fmt"
	"html/template"
	"path/filepath"

	"github.com/probertson/diff-by-numbers/internal/present"
)

// overviewData is the Overview as the page draws it: what the TUI's Overview
// shows, read rather than acted on, so it leaves out the keys the TUI offers
// and the line naming the page itself.
type overviewData struct {
	Label string
	// Round is which round this is, named in the header after the first.
	Round      int
	Goal       template.HTML
	Background template.HTML
	// Questions are the Round's own Agent Questions, about the work as a whole,
	// which sit with the Brief (ADR-0017); Carried are the answered questions a
	// replacement kept, which belong to no Step of this Round.
	Questions []questionData
	Carried   []carriedData
	// Since and Accounted are what the agent did with the previous round's
	// Comments and Agent Questions, under the headings the TUI sorts them by.
	Since     []outcomeData
	Accounted []outcomeData
	// PreviousRound is the round Withdrawn names: what it had and this one
	// removed outright.
	PreviousRound int
	Withdrawn     []withdrawnData
	Repositories  []RepositoryWire
	// A Round in Chapters lists its Chapters in place of its Steps.
	Chapters []listedData
	Steps    []listedData
}

// carriedData is an answered question a replacement carried over.
type carriedData struct {
	ID     int
	Text   template.HTML
	Answer string
}

// outcomeData is one heading of what became of the previous round, with its
// items: Comments for Since, Agent Questions for Accounted.
type outcomeData struct {
	Status    string
	Heading   string
	Mark      string
	Comments  []dispositionData
	Questions []accountedData
}

// dispositionData is one Comment of the previous round and what the agent did
// with it. ReRaised names the Comment of this round that pushes back on it.
type dispositionData struct {
	ID       int
	Location string
	Note     string
	Response string
	ReRaised string
}

// accountedData is one Agent Question of the previous round: what was asked
// and where, the Answer it got, and what the agent did about it.
type accountedData struct {
	ID         int
	Where      string
	AskedAgain string
	Text       template.HTML
	Answer     string
	Response   string
}

// withdrawnData is a run of lines the previous round had and this one removed.
type withdrawnData struct {
	Heading string
	Lines   []string
}

// listedData is one row of the Overview's list of Steps or Chapters.
type listedData struct {
	Number    int
	Name      string
	Range     string
	Seen      bool
	Unchanged string
	Asks      string
}

func newOverviewData(label string, view ViewWire) *overviewData {
	data := &overviewData{
		Label:         label,
		Round:         view.Round,
		Goal:          renderProse(view.Brief.Goal),
		PreviousRound: view.PreviousRound,
		Repositories:  view.Repositories,
	}
	// Only round 1 must give a Background. A later round that gives none shows
	// none, rather than an empty heading or an earlier round's stage-setting.
	if view.Brief.Background != "" {
		data.Background = renderProse(view.Brief.Background)
	}
	for _, question := range view.RoundQuestions {
		data.Questions = append(data.Questions, questionData{Text: renderProse(question.Text), Answer: question.Answer})
	}
	for _, question := range view.Questions {
		if question.CarriedOver {
			data.Carried = append(data.Carried, carriedData{ID: question.ID, Text: renderProse(question.Text), Answer: question.Answer})
		}
	}
	data.Since = sinceTheLastRound(view)
	data.Accounted = questionsFromTheLastRound(view)
	data.Withdrawn = withdrawnSince(view)

	unchanged := fmt.Sprintf("unchanged since round %d", view.PreviousRound)
	if len(view.Chapters) > 0 {
		for _, chapter := range view.Chapters {
			row := listedData{
				Number: chapter.Number, Name: chapter.Name, Seen: chapter.Seen,
				Range: present.StepRange(chapter.FirstStep, chapter.LastStep), Asks: asksYou(chapter.QuestionCount),
			}
			if chapter.UnchangedSincePrevious {
				row.Unchanged = unchanged
			}
			data.Chapters = append(data.Chapters, row)
		}
		return data
	}
	asked := map[int]int{}
	for _, question := range view.Questions {
		asked[question.Step]++
	}
	for i, name := range view.StepNames {
		row := listedData{Number: i + 1, Name: name, Asks: asksYou(asked[i+1])}
		row.Seen = i < len(view.Seen) && view.Seen[i]
		if i < len(view.UnchangedSincePrevious) && view.UnchangedSincePrevious[i] {
			row.Unchanged = unchanged
		}
		data.Steps = append(data.Steps, row)
	}
	return data
}

// asksYou says a Step or a Chapter asks the Reviewer questions: how many,
// never what, since a Step's question needs the Steps before it.
func asksYou(asked int) string {
	switch asked {
	case 0:
		return ""
	case 1:
		return "asks you a question"
	}
	return fmt.Sprintf("asks you %d questions", asked)
}

// sinceTheLastRound sorts the previous round's Comments under what the agent
// did with each. A heading counts those the Reviewer has pushed back on, so
// the count they act on is the one still standing (#80).
func sinceTheLastRound(view ViewWire) []outcomeData {
	reRaised := map[int]int{}
	for _, comment := range view.Comments {
		if comment.ReRaised() {
			reRaised[comment.ReRaisedFrom] = comment.ID
		}
	}
	var out []outcomeData
	for _, outcome := range present.CommentOutcomes {
		group := outcomeData{Status: outcome.Status, Mark: outcome.Mark}
		pushedBack := 0
		for _, disposition := range view.Dispositions {
			if present.CommentStatus(disposition.Status) != outcome.Status {
				continue
			}
			item := dispositionData{
				ID: disposition.CommentID, Location: disposition.Location,
				Note: disposition.Note, Response: disposition.Response,
			}
			if id, ok := reRaised[disposition.CommentID]; ok {
				item.ReRaised = fmt.Sprintf("re-raised as #%d", id)
				pushedBack++
			}
			group.Comments = append(group.Comments, item)
		}
		if len(group.Comments) == 0 {
			continue
		}
		group.Heading = fmt.Sprintf("%s (%d)", outcome.Heading, len(group.Comments))
		if pushedBack > 0 {
			group.Heading = fmt.Sprintf("%s (%d, %d re-raised)", outcome.Heading, len(group.Comments), pushedBack)
		}
		out = append(out, group)
	}
	return out
}

// questionsFromTheLastRound sorts the previous round's Agent Questions under
// the status the agent gave each.
func questionsFromTheLastRound(view ViewWire) []outcomeData {
	var out []outcomeData
	for _, outcome := range present.QuestionOutcomes {
		group := outcomeData{Status: outcome.Status, Mark: outcome.Mark}
		for _, accounted := range view.AccountedQuestions {
			if accounted.Status != outcome.Status {
				continue
			}
			question := accounted.Question
			item := accountedData{
				ID: question.ID, Where: present.AskedWhere(question.Step),
				Text: renderProse(question.Text), Answer: question.Answer, Response: accounted.Response,
			}
			// Where it was asked again, and not what: the new question is read
			// where it sits, with the Steps before it for context.
			if asked := accounted.AskedAgainAs; asked != nil {
				switch {
				case asked.CarriedOver:
					item.AskedAgain = "asked again, and answered before the Round was replaced"
				case asked.Step == 0:
					item.AskedAgain = "asked again with the Brief"
				default:
					item.AskedAgain = fmt.Sprintf("asked again on Step %d", asked.Step)
				}
			}
			group.Questions = append(group.Questions, item)
		}
		if len(group.Questions) == 0 {
			continue
		}
		group.Heading = fmt.Sprintf("%s (%d)", outcome.Heading, len(group.Questions))
		out = append(out, group)
	}
	return out
}

// withdrawnSince heads each run of withdrawn lines with its file and where it
// was: below which line of this round, and where it stood in the previous
// one, which is all there is to place it by when this round no longer has the
// file at all.
func withdrawnSince(view ViewWire) []withdrawnData {
	var out []withdrawnData
	for _, w := range view.Withdrawn {
		file := w.File
		if len(view.Repositories) > 1 {
			file = "[" + filepath.Base(w.Repository) + "] " + file
		}
		where := "at the top"
		if w.After > 0 {
			where = fmt.Sprintf("below line %d", w.After)
		}
		span := "line " + present.LineRange(w.PreviousFirst, w.PreviousFirst+len(w.Lines)-1)
		if len(w.Lines) > 1 {
			span = "lines " + present.LineRange(w.PreviousFirst, w.PreviousFirst+len(w.Lines)-1)
		}
		out = append(out, withdrawnData{
			Heading: fmt.Sprintf("%s — %s (round %d %s)", file, where, view.PreviousRound, span),
			Lines:   w.Lines,
		})
	}
	return out
}
