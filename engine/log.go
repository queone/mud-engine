package engine

// Play is one logged event: a plate appearance or a stolen-base attempt.
// Bases hold the ids of the runners on first, second, and third, or "".
type Play struct {
	Inning      int       `json:"inning"`
	Half        string    `json:"half"`
	Batter      string    `json:"batter"`
	Pitcher     string    `json:"pitcher"`
	Outcome     string    `json:"outcome"`
	BasesBefore [3]string `json:"bases_before"`
	OutsBefore  int       `json:"outs_before"`
	BasesAfter  [3]string `json:"bases_after"`
	OutsAfter   int       `json:"outs_after"`
	Runs        int       `json:"runs"`
	Charged     []string  `json:"charged,omitempty"`
	Away        int       `json:"away"`
	Home        int       `json:"home"`
	Text        string    `json:"text"`
}

// Play outcomes.
const (
	OutcomeK   = "K"   // strikeout
	OutcomeBB  = "BB"  // walk
	OutcomeHBP = "HBP" // hit by pitch
	Outcome1B  = "1B"  // single
	Outcome2B  = "2B"  // double
	Outcome3B  = "3B"  // triple
	OutcomeHR  = "HR"  // home run
	OutcomeGO  = "GO"  // ground out
	OutcomeDP  = "DP"  // ground into a double play
	OutcomeAO  = "AO"  // air out
	OutcomeSF  = "SF"  // sacrifice fly
	OutcomeSB  = "SB"  // stolen base
	OutcomeCS  = "CS"  // caught stealing
)

// Half names.
const (
	Top    = "top"
	Bottom = "bottom"
)
