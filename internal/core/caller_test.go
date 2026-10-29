package core

import "testing"

// TestValidProjectNameLength checks the 64-character limit on a project name
// at its edge: 64 lower-case letters are valid, and 65 are not. Mutation testing
// showed that the edge was not pinned, so an off-by-one bound would pass.
func TestValidProjectNameLength(t *testing.T) {
	long64 := make([]byte, 64)
	for i := range long64 {
		long64[i] = 'a'
	}
	if !ValidProjectName(string(long64)) {
		t.Errorf("a 64-character name is refused")
	}
	if ValidProjectName(string(long64) + "a") {
		t.Errorf("a 65-character name is accepted")
	}
}
