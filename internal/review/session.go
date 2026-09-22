package review

import (
	"crypto/rand"
	"encoding/hex"
)

// Results is what the Authoring Agent receives when it asks how a review went.
type Results struct {
	// Posted reports whether a Round exists at all. Without it, an agent
	// whose post was rejected would be told the Reviewer "has not handed off yet"
	// and wait on a Round that was never created.
	Posted bool
	// Finished reports whether the Reviewer has handed off the Round. The
	// agent does not wait for this; it asks and is told.
	Finished bool
	Comments []Comment
	// Brief and StepReports let the agent re-ground itself when it comes to work
	// the Comments, since its own context may have moved on or been
	// compacted since it posted (ADR-0008).
	Brief       Brief
	StepReports []StepReport
}

// Session holds the one Review currently under way. Several concurrent
// Reviews are deliberately out of scope until the Inbox exists.
type Session struct {
	current  *Round
	ledger   ledger
	resolver Resolver
	deriver  Deriver
	// position is where the Reviewer is: 0 for the Brief, 1..len(Steps) for a
	// Step. It lives here rather than in any UI so that a reattaching surface
	// finds the review where it was left.
	position int
	// seen records which Steps the Reviewer has visited. A Step becomes seen the
	// moment it is in view, whether reached by advancing, going back, or jumping.
	seen          map[int]bool
	comments      []Comment
	nextCommentID int
	finished      bool
	// latest is the accepted round on screen: its Round, which its code is read
	// from, and the atoms it showed. It is what the next Revision Round is scoped
	// against (ADR-0014).
	latest roundState
	// answering is the earlier round the Round on screen is a revision
	// of, or nil for a first round. A replacement answers to it too.
	answering *earlierRound
	// replaced records that the Round on screen replaced another in place,
	// so a surface can tell the Reviewer why it changed under them.
	replaced bool
	// roundNumber counts the review's rounds: 1 for the first, one more for each
	// Revision Round. A Replacement is the same round.
	roundNumber int
	// previous is the round the one on screen is compared with, or nil for a
	// first round; showAll is the Reviewer choosing to see every change under
	// review instead.
	previous *previousRound
	showAll  bool
	// editChanges remembers what changed inside each edit already matched
	// (#81). The round's content is fixed, so it holds for as long as the round
	// is on screen.
	editChanges map[string]changesWithin
	// dispositions accounts for the previous round's Comments in a Revision
	// Round, for display before any code.
	dispositions []ResolvedDisposition
	// id is the review's identity, minted when a new review is first posted and
	// preserved across its Revision Rounds and Replacements, so the Authoring
	// Agent can refer back to the review to replace or conclude it.
	id string
	// label is the optional human-readable name carried on the Round.
	label string
	// concluded records an explicit conclusion. A review also reads as concluded
	// by inference — see isConcluded — when a round finishes with nothing raised.
	concluded bool
	// mint generates a new review id. Injected so tests can assert on a known id.
	mint func() string
	// postings counts the Rounds accepted, so a surface can tell when a
	// different one — a new review, a Revision Round or a Replacement — has taken
	// the screen.
	postings int
}

// SessionOption configures a Session at construction.
type SessionOption func(*Session)

// WithIDMinter overrides how review ids are generated, so a test can assert on a
// known id rather than a random one.
func WithIDMinter(mint func() string) SessionOption {
	return func(s *Session) { s.mint = mint }
}

// NewSession returns a Session with no Round posted. The resolver turns
// Excerpts into lines when a view is drawn; the deriver reports what git says
// actually changed. The core itself neither reads files nor runs git.
func NewSession(resolver Resolver, deriver Deriver, opts ...SessionOption) *Session {
	s := &Session{resolver: resolver, deriver: deriver, mint: defaultMint}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// defaultMint returns a short random hex id, unique enough to tell concurrent
// reviews apart without any coordination.
func defaultMint() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ReviewID returns the id of the review under review, or "" if none is posted.
func (s *Session) ReviewID() string { return s.id }

// PostKind is what an accepted post was to its review.
type PostKind string

const (
	PostedNewReview     PostKind = "new_review"
	PostedRevisionRound PostKind = "revision_round"
	PostedReplacement   PostKind = "replacement"
)

// LastPost reports what the Round on screen was when it was accepted. A
// replacement of a Revision Round reads as a replacement: what the agent just
// did is replace the Round, not start a round.
func (s *Session) LastPost() PostKind {
	switch {
	case s.replaced:
		return PostedReplacement
	case s.answering != nil:
		return PostedRevisionRound
	}
	return PostedNewReview
}

// Label returns the optional human-readable name the Authoring Agent attached to
// the review, or "" if none was given.
func (s *Session) Label() string { return s.label }

// Post submits a Round for review. Posting after the previous Round
// was handed off with Comments is a Revision Round: it re-derives the full Change
// Set, pre-marks what is unchanged, and must account for those Comments. Posting
// once the review is concluded — explicitly, or by a hand-off that raised
// nothing — starts a new review.
func (s *Session) Post(w Round) error {
	if s.current != nil && !s.finished && !s.isConcluded() {
		return reject(RejectedWalkthroughActive,
			"a Round is already under review (id %s); to update it, post again with replaces: %q; otherwise wait for the Reviewer to hand it off",
			s.id, s.id)
	}
	return s.accept(w, s.nextAnswering(), false)
}

// nextAnswering is the earlier round the next accepted post would answer: the
// round just handed off, or — while one is under review — whatever that one
// answers, which is what its Replacement answers too.
//
// A concluded review answers nothing, so the next post starts a new review. That
// covers a round handed off with nothing raised as well as an explicit conclude:
// fetch_results already calls that review complete, and posting new work as a
// Revision Round of it pre-marked the new work against the finished review, so it
// could escape coverage.
func (s *Session) nextAnswering() *earlierRound {
	switch {
	case s.current == nil || s.isConcluded():
		return nil
	case s.finished:
		return &earlierRound{state: s.latest, comments: s.comments}
	}
	return s.answering
}

// Replace puts a Round in place of the one under review, for when the
// Reviewer asks for a change mid-review or the agent sees its plan was wrong.
// It is the same review: the id stays, the Reviewer's Comments carry over, and a
// Revision Round's replacement answers to the same earlier round the replaced
// one did. What changes is what the Reviewer walks, so they start it afresh.
func (s *Session) Replace(id string, w Round) error {
	if s.current == nil || id != s.id {
		return reject(RejectedUnknownReview,
			"no review with id %q is under review, so there is nothing to replace", id)
	}
	// Concluded outright, or by a hand-off that raised nothing: either way the
	// review is over, and new work is a new review.
	if s.isConcluded() {
		return reject(RejectedUnknownReview,
			"review %q is concluded; post without replaces to start a new review", id)
	}
	if s.finished {
		return reject(RejectedRoundHandedOff,
			"this round is handed off; post a Revision Round instead")
	}
	return s.accept(w, s.nextAnswering(), true)
}

// earlierRound is what a Revision Round answers to: the round before it, as it
// was scoped, and the Comments that round raised.
type earlierRound struct {
	state    roundState
	comments []Comment
}

// accept validates a Round and, if it passes, makes it the one under
// review. earlier is the round it answers to, or nil for a first round;
// replacing says it takes the place of the Round under review, which
// makes it the same review and the same round.
func (s *Session) accept(w Round, earlier *earlierRound, replacing bool) error {
	// The Goal belongs to the Review, not the Round: a Revision Round or a
	// Replacement that leaves it out keeps the one the Review already has.
	if w.Brief.Goal == "" && (earlier != nil || replacing) {
		w.Brief.Goal = s.current.Brief.Goal
	}
	if rejection := validate(w); rejection != nil {
		return rejection
	}

	// Stage 1 stops at the first failure. Derivation needs a well-formed
	// Round, and every stage-2 check needs the ledger, so anything they
	// might say without these would be guesswork. The round's snapshot is
	// always taken: it is what the round will be shown from, and it is kept only
	// once the post is accepted.
	ledger, round, rejection := s.scopedLedger(w.ChangeSet, earlier, true)
	if rejection != nil {
		return rejection
	}

	// Normalisation rewrites the Excerpts into the ranges dbn will use, before
	// anything judges them, so the checks below see exactly what will be stored
	// and shown.
	w.Steps = normalize(w.Steps, w.ChangeSet, ledger)

	// Stage 2: every check runs, and every one that fails is reported. They are
	// independent of each other, so stopping at the first only hides what the
	// agent would have to come back for.
	var problems []Problem
	add := func(rejection *Rejection) {
		if rejection != nil {
			problems = append(problems, rejection.Problems...)
		}
	}

	var dispositions []ResolvedDisposition
	if earlier != nil {
		resolved, rejection := resolveDispositions(w.Dispositions, earlier.comments)
		add(rejection)
		dispositions = resolved
	} else if len(w.Dispositions) > 0 {
		add(reject(RejectedMalformedDisposition,
			"this is round 1; there are no Comments to dispose of"))
	}

	add(validateNewSideResolves(w.Steps, s.resolver, round))

	add(ledger.validateBudget(w.Steps))
	add(ledger.validateAcknowledgements(w.Steps))
	add(ledger.validateCoverage(w.Steps))

	if rejection := rejectAll(problems); rejection != nil {
		return rejection
	}

	// Every validation has passed and this Round is now the one under
	// review. Only now does its round replace the one on screen: a rejected post
	// leaves the Reviewer reading exactly what they were.
	s.current = &w
	s.ledger = ledger
	s.position = 0
	s.seen = map[int]bool{}
	s.finished = false
	s.dispositions = dispositions
	s.answering = earlier
	s.latest = captureRound(ledger, round)
	s.replaced = replacing
	s.postings++
	switch {
	case replacing:
	case earlier != nil:
		s.roundNumber++
	default:
		s.roundNumber = 1
	}
	s.previous = s.comparedWith(earlier, round, w.ChangeSet)
	s.settle(s.previous)
	s.showAll = false
	s.editChanges = map[string]changesWithin{}

	// A replacement keeps the Reviewer's Comments: each quotes its own code, so
	// it stands without the Step it was raised on, which the replacement no
	// longer has. Any other posting starts a round with none.
	if replacing {
		for i := range s.comments {
			s.comments[i].Step = 0
			s.comments[i].CarriedOver = true
		}
	} else {
		s.comments = nil
		s.nextCommentID = 0
	}

	// A new review mints an id and takes the Round's label as given; a
	// later posting keeps the id and only updates the label if one is supplied,
	// so an agent that omits it does not blank it. Either way, posting means
	// the review is active again.
	if earlier == nil && !replacing {
		s.id = s.mint()
		s.label = w.Label
	} else if w.Label != "" {
		s.label = w.Label
	}
	s.concluded = false
	return nil
}

// scopedLedger derives a Change Set and marks on it what the earlier round has
// already shown. It is the one path by which a post and describe_changes see a
// Change Set, which is what keeps them from disagreeing.
//
// The snapshot is taken before anything else reads the working tree. It is what
// a posted round is shown from, so it is also what the post is checked against:
// every Excerpt an accepted Round names is one its own snapshot can show.
// (The Change Set is still derived from the working tree a moment later; see
// #104.) It is also what pre-marking maps through. A caller with neither use for
// it — a description of a first round — passes snapshot false and writes no
// tree.
//
// What a Revision Round has already shown is marked on the ledger itself, so
// normalisation and every check ask their questions of the round's scope, not of
// the raw Change Set. It is always the earlier accepted round that decides this —
// never a Round being replaced, which the Reviewer may not have read.
func (s *Session) scopedLedger(set ChangeSet, earlier *earlierRound, snapshot bool) (ledger, RoundSource, *Rejection) {
	var round RoundSource
	if snapshotter, ok := s.deriver.(Snapshotter); ok && snapshot {
		round.Snapshots = snapshotAll(snapshotter, set)
	}
	l, err := buildLedger(set, s.deriver)
	if err != nil {
		return ledger{}, RoundSource{}, reject(RejectedDerivationFailed,
			"could not derive the changes under review: %v", err)
	}
	round.Bases = l.bases
	if earlier != nil {
		l = l.withPreMarking(s.preMarkUnchanged(l, set, round, earlier.state))
	}
	return l, round, nil
}

// moveTo places the Reviewer at a position and records a Step as seen. It is the
// single path every navigation goes through, so seen-tracking cannot be skipped.
func (s *Session) moveTo(position int) {
	s.position = position
	if position > 0 {
		if s.seen == nil {
			s.seen = map[int]bool{}
		}
		s.seen[position] = true
	}
}

// validateNewSideResolves rejects a new-side Excerpt whose range the round's
// snapshot cannot satisfy — the same snapshot the Round will be shown from.
// Old-side Excerpts are not checked here: an old-side range that cannot be read
// is surfaced as a render Problem in place of the code, never fabricated.
func validateNewSideResolves(steps []Step, resolver Resolver, round RoundSource) *Rejection {
	for i, step := range steps {
		for j, excerpt := range step.Excerpts {
			if excerpt.Side != NewSide {
				continue
			}
			if _, err := resolver.Resolve(excerpt, round); err != nil {
				return reject(RejectedUnresolvableExcerpt,
					"Excerpt %d of Step %d does not resolve: %v", j+1, i+1, err)
			}
		}
	}
	return nil
}

// Abandon discards the Round under review without submitting anything.
// It is the Reviewer's act, not the Authoring Agent's, so it is not exposed as
// an MCP tool: an agent cannot dismiss a review of its own work.
func (s *Session) Abandon() error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to abandon")
	}
	s.current = nil
	s.id = ""
	s.label = ""
	s.concluded = false
	return nil
}

// Results reports how the review is going. It answers immediately, whether or
// not the Reviewer has finished.
func (s *Session) Results() (Results, error) {
	if s.current == nil {
		return Results{Posted: false}, nil
	}
	reports := make([]StepReport, len(s.current.Steps))
	for i, step := range s.current.Steps {
		reports[i] = StepReport{Number: i + 1, Name: step.Name, Status: s.stepStatus(i + 1)}
	}
	return Results{
		Posted:      true,
		Finished:    s.finished,
		Comments:    s.Comments(),
		Brief:       s.current.Brief,
		StepReports: reports,
	}, nil
}

// Advance moves the Reviewer one position forward: from the Brief to Step 1, or
// from a Step to the next. At the last Step it stays put — running out of road
// is not an error.
func (s *Session) Advance() error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to advance through")
	}
	if s.position < len(s.current.Steps) {
		s.moveTo(s.position + 1)
	}
	return nil
}

// Back moves the Reviewer one position toward the Brief, so an earlier change
// can be revisited once a later one has given it meaning. At the Brief it stays.
func (s *Session) Back() error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to move back through")
	}
	if s.position > 0 {
		s.moveTo(s.position - 1)
	}
	return nil
}

// GoTo jumps straight to a position: 0 for the Brief, 1..len(Steps) for a Step.
// Navigation is entirely local — the Authoring Agent is never consulted.
func (s *Session) GoTo(position int) error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to navigate")
	}
	if position < 0 || position > len(s.current.Steps) {
		return reject(RejectedNoSuchStep,
			"there is no Step %d; this Round has %d", position, len(s.current.Steps))
	}
	s.moveTo(position)
	return nil
}
