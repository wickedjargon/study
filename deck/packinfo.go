package deck

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// PackInfoName returns the pack's display name from the optional .deck-info
// file in a pack directory, or "" when there isn't one. The file uses the
// same "# key: value" grammar as deck headers, with no cards — pack-scoped
// facts in a pack-scoped location, instead of arbitrarily electing a member
// file to carry them:
//
//	# pack: US Presidents
//
// Unknown keys are ignored so the format can grow. Called with a deck file
// path (not a directory) it naturally returns "": files name themselves on
// their first line.
func PackInfoName(dir string) string {
	f, err := os.Open(filepath.Join(dir, ".deck-info"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if after, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "# pack:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}
