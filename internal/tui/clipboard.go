package tui

import (
	"os/exec"
	"runtime"
)

// copyToClipboard writes text to the system clipboard. dbn runs locally beside
// the Reviewer, so shelling out to the platform tool is enough and avoids a
// dependency. It returns whether it succeeded, so the UI can be honest when no
// clipboard tool is present rather than claim a copy that did not happen.
func copyToClipboard(text string) bool {
	name, args := clipboardCommand()
	if name == "" {
		return false
	}
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	_, _ = stdin.Write([]byte(text))
	stdin.Close()
	return cmd.Wait() == nil
}

func clipboardCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbcopy", nil
	default:
		for _, candidate := range [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		} {
			if _, err := exec.LookPath(candidate[0]); err == nil {
				return candidate[0], candidate[1:]
			}
		}
		return "", nil
	}
}
