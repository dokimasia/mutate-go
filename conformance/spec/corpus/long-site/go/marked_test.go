package fixture

import "testing"

// marker is the release marker that marked looks for.
const marker = "release-candidate-of-the-long-term-support-branch-for-the-second-quarter-of-the-year-twenty-twenty-six"

func TestMarked(t *testing.T) {
	if !marked("v1 "+marker) || marked("# "+marker) || marked("v1") {
		t.Fatal("marked does not report exactly the lines that contain the marker and are no comment")
	}
}
