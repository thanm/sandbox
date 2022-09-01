package plex

import "testing"

func TestBasic(t *testing.T) {
	if v1 := Single() + Multiple(); v1 != 4 {
		t.Fatalf("bad: %d", v1)
	}
}
