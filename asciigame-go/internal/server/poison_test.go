package server

import "testing"

// TestPoisonSymmetry verifies the safe zone is symmetric: for any radius, the
// left/right and top/bottom edges enter the poison at the same shrink step.
// The old integer-center formula closed the left/top one step early.
func TestPoisonSymmetry(t *testing.T) {
	for radius := 0; radius <= 25; radius++ {
		// Horizontal symmetry: cell (x,cy) mirrors to (W-1-x, cy).
		cy := 9
		for x := 0; x < 10; x++ {
			mx := 49 - x
			if got, want := mapIsInPoison(x, cy, radius), mapIsInPoison(mx, cy, radius); got != want {
				t.Errorf("radius=%d: x=%d poison=%v but mirror x=%d poison=%v", radius, x, got, mx, want)
			}
		}
		// Vertical symmetry: cell (cx,y) mirrors to (cx, H-1-y).
		cx := 24
		for y := 0; y < 5; y++ {
			my := 19 - y
			if got, want := mapIsInPoison(cx, y, radius), mapIsInPoison(cx, my, radius); got != want {
				t.Errorf("radius=%d: y=%d poison=%v but mirror y=%d poison=%v", radius, y, got, my, want)
			}
		}
	}
}
