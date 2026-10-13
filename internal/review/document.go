package review

import (
	"path/filepath"
	"strings"
)

// isDocument is dbn's own classification of a file: whether its content is
// prose for a person to read rather than code. dbn judges by file type alone,
// so Markdown is a Document and everything else is not; the agent, which knows
// what a file is for, may overrule it (#159). The path is the one the file is
// described under: its new path, or the path it had if it was deleted.
func isDocument(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	}
	return false
}
