package pricing

import (
	"math"
	"sort"
)

// Fence is a robust plausibility band for one population of prices (a brand, or a brand and
// product tier). It is built from the median and the median absolute deviation on a log scale,
// so a handful of wild OCR values cannot stretch it the way a mean and standard deviation would.
type Fence struct {
	N        int
	Median   int64
	Lo, Hi   int64
	LogMed   float64
	LogMAD   float64
	TooSmall bool // fewer than MinSample prices: only the hard bounds apply
}

// MinSample is the smallest population that yields a statistical fence.
const MinSample = 12

// K is the fence width in scaled MADs (1.4826 turns a MAD into a standard deviation for normal
// data). The width is then clamped: never tighter than ×MinRatio above the median and
// ÷MinRatioLow below it (a book of near-identical prices must still admit its accessories and
// its showpieces) and never wider than ×MaxRatio (a genuine brand runs from a $30 aerator to a
// $6,000 tub filler, well inside 50× of its median either way; a SKU or an index reference read
// as money is not). ×40 is wide: the gross OCR errors (a dropped decimal point is ×100, an
// index reference is ×1000) are caught by ocr.linePrice and the hard bounds before the fence,
// and a $400-median plumbing brand really sells $12 aerators and $1,600 appliance pulls
// (Chicago Faucets, Classic Brass, Ashley Norton).
const (
	K           = 3.5
	MinRatio    = 40.0 // a hardware book: $45 knobs and $1,600 appliance pulls are both genuine (Ashley Norton)
	MinRatioLow = 40.0
	MaxRatio    = 50.0
)

// NewFence builds the fence for cents values; zeros and negatives are ignored.
func NewFence(cents []int64) Fence {
	logs := make([]float64, 0, len(cents))
	for _, c := range cents {
		if c > 0 {
			logs = append(logs, math.Log(float64(c)))
		}
	}
	f := Fence{N: len(logs), Lo: MinCents, Hi: MaxCents}
	if f.N < MinSample {
		f.TooSmall = true
		return f
	}
	sort.Float64s(logs)
	f.LogMed = median(logs)
	f.Median = int64(math.Round(math.Exp(f.LogMed)))
	dev := make([]float64, len(logs))
	for i, l := range logs {
		dev[i] = math.Abs(l - f.LogMed)
	}
	sort.Float64s(dev)
	f.LogMAD = median(dev)
	spread := math.Min(math.Max(1.4826*f.LogMAD*K, math.Log(MinRatio)), math.Log(MaxRatio))
	spreadLo := math.Min(math.Max(1.4826*f.LogMAD*K, math.Log(MinRatioLow)), math.Log(MaxRatio))
	lo := int64(math.Round(math.Exp(f.LogMed - spreadLo)))
	hi := int64(math.Round(math.Exp(f.LogMed + spread)))
	if lo > MinCents {
		f.Lo = lo
	}
	if hi < MaxCents {
		f.Hi = hi
	}
	return f
}

// Plausible reports whether a price sits inside the fence (and the hard bounds).
func (f Fence) Plausible(cents int64) bool {
	return cents >= f.Lo && cents <= f.Hi
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// TierMax is the highest list price a family of a given tier can plausibly carry: finished
// fixtures (0) up to the hard maximum, families whose title could not be read (1) $30,000, and
// parts / rough-ins / cartridges (2) $7,500. A $61,000 riser pipe passes a brand fence but not
// this.
func TierMax(tier int) int64 {
	return TierMaxFor(tier, 0)
}

// TierMaxFor scales TierMax to the brand: in a brand whose median list price is high the parts
// tier admits ten times that median (Barber Wilsons' $12,000 bath mixers sit in "Bathtub
// Parts", Hydro Systems' $14,000 whirlpool combos under an unread title) and the unread tier
// twenty times, never less than the flat caps.
func TierMaxFor(tier int, median int64) int64 {
	switch tier {
	case 2:
		return max(750_000, 10*median)
	case 1:
		return max(3_000_000, 20*median)
	}
	return MaxCents
}
