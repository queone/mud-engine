package engine

// BatterLine is one batter's totals for a game.
type BatterLine struct {
	PA      int `json:"pa"`
	AB      int `json:"ab"`
	Singles int `json:"1b"`
	Doubles int `json:"2b"`
	Triples int `json:"3b"`
	HR      int `json:"hr"`
	R       int `json:"r"`
	RBI     int `json:"rbi"`
	BB      int `json:"bb"`
	HBP     int `json:"hbp"`
	K       int `json:"k"`
	SB      int `json:"sb"`
	CS      int `json:"cs"`
	SF      int `json:"sf"`
}

// Hits is the batter's total hits.
func (b BatterLine) Hits() int { return b.Singles + b.Doubles + b.Triples + b.HR }

// Add accumulates another line into b.
func (b *BatterLine) Add(o BatterLine) {
	b.PA += o.PA
	b.AB += o.AB
	b.Singles += o.Singles
	b.Doubles += o.Doubles
	b.Triples += o.Triples
	b.HR += o.HR
	b.R += o.R
	b.RBI += o.RBI
	b.BB += o.BB
	b.HBP += o.HBP
	b.K += o.K
	b.SB += o.SB
	b.CS += o.CS
	b.SF += o.SF
}

// PitcherLine is one pitcher's totals for a game.
type PitcherLine struct {
	Outs int `json:"outs"`
	H    int `json:"h"`
	R    int `json:"r"`
	ER   int `json:"er"`
	BB   int `json:"bb"`
	HBP  int `json:"hbp"`
	K    int `json:"k"`
	W    int `json:"w"`
	L    int `json:"l"`
	SV   int `json:"sv"`
	HLD  int `json:"hld"`
	BS   int `json:"bs"`
	QS   int `json:"qs"`
}

// Add accumulates another line into p.
func (p *PitcherLine) Add(o PitcherLine) {
	p.Outs += o.Outs
	p.H += o.H
	p.R += o.R
	p.ER += o.ER
	p.BB += o.BB
	p.HBP += o.HBP
	p.K += o.K
	p.W += o.W
	p.L += o.L
	p.SV += o.SV
	p.HLD += o.HLD
	p.BS += o.BS
	p.QS += o.QS
}

// Totals are one club's summed lines for a game.
type Totals struct {
	Name     string      `json:"name"`
	Runs     int         `json:"runs"`
	Batting  BatterLine  `json:"batting"`
	Pitching PitcherLine `json:"pitching"`
}
