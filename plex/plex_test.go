package plex

import "testing"

func TestBasic(t *testing.T) {
	if v := Single() + Multiple(); v != 4 {
		t.Fatalf("bad: %d", v)
	}
}
