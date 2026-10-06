package fixture

import "strings"

// marked reports whether s contains the release marker and is not a
// comment.
func marked(s string) bool {
	return strings.Contains(s, "release-candidate-of-the-long-term-support-branch-for-the-second-quarter-of-the-year-twenty-twenty-six") && !strings.HasPrefix(s, "#")
}
