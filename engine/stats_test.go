package engine

import "testing"

func TestLinesAddEveryField(t *testing.T) {
	b := BatterLine{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	var sum BatterLine
	sum.Add(b)
	sum.Add(b)
	if sum != (BatterLine{2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28}) {
		t.Errorf("batter add = %+v", sum)
	}
	if b.Hits() != 3+4+5+6 {
		t.Errorf("Hits() = %d, want 18", b.Hits())
	}
	p := PitcherLine{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	var ps PitcherLine
	ps.Add(p)
	ps.Add(p)
	if ps != (PitcherLine{2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26}) {
		t.Errorf("pitcher add = %+v", ps)
	}
}
