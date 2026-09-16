package evidence

import "testing"

func TestActionTransitions(t *testing.T) {
	for _, path := range [][]string{{"proposed", "allowed", "executed"}, {"proposed", "denied"}, {"proposed", "allowed", "failed"}, {"proposed", "failed"}, {"proposed", "allowed", "denied"}} {
		for i := 1; i < len(path); i++ {
			if !validActionTransition(path[i-1], path[i]) {
				t.Fatal(path)
			}
		}
	}
	for _, pair := range [][2]string{{"executed", "allowed"}, {"denied", "executed"}, {"proposed", "executed"}, {"failed", "proposed"}} {
		if validActionTransition(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
}
