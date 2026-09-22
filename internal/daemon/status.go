package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// StatusWire is what /status answers: who the daemon is and whether it is in the
// middle of something. Not part of the view — nothing here is drawn during a
// Round — but it shares the wire types' audience, the TUI and the CLI.
type StatusWire struct {
	Version string `json:"version"`
	// Executable is the binary this daemon is running, so `dbn update` can tell
	// "the copy I just replaced" from "a different dbn someone's launchd starts".
	Executable   string `json:"executable,omitempty"`
	ActiveReview bool   `json:"active_review"`
}

// ErrNoStatus is what a daemon older than this endpoint looks like: it answers on
// the port, but not there. Telling that apart from nothing listening is the whole
// point — it is exactly the daemon a Reviewer who just updated is left with, and
// "restart your daemon" is worth saying where silence is not.
var ErrNoStatus = errors.New("the daemon on this port cannot report its status")

// statusTimeout bounds a status read. The daemon is on loopback and answers from
// memory, so anything slower than this is something other than dbn.
const statusTimeout = 3 * time.Second

// FetchStatus asks the daemon at baseURL (e.g. http://127.0.0.1:7373) who it is.
// A transport error means nothing is listening; ErrNoStatus means something is,
// but could not say what it is.
func FetchStatus(ctx context.Context, baseURL string) (*StatusWire, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: statusTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: it answered %s", ErrNoStatus, response.Status)
	}
	var status StatusWire
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoStatus, err)
	}
	return &status, nil
}
