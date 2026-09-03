package gp

import "math"

// noteValueQuarters is how long each GPIF note value lasts, measured in
// quarter notes. Quarter notes are the unit the rest of this package works in
// because BPM counts them: converting to frames then needs only the tempo of
// the bar the note falls in.
var noteValueQuarters = map[string]float64{
	"Long":        16,
	"DoubleWhole": 8,
	"Whole":       4,
	"Half":        2,
	"Quarter":     1,
	"Eighth":      0.5,
	"16th":        0.25,
	"32nd":        0.125,
	"64th":        0.0625,
	"128th":       0.03125,
	"256th":       0.015625,
}

// maxAugmentationDots bounds the dot count. Guitar Pro writes at most three,
// and an absurd count in a hand-edited file must not turn into an absurd
// duration.
const maxAugmentationDots = 4

// quarters is the rhythm's duration in quarter notes, dots and tuplet
// included.
func (r *rhythmNode) quarters() (float64, bool) {
	if r == nil {
		return 0, false
	}
	base, ok := noteValueQuarters[r.NoteValue]
	if !ok {
		return 0, false
	}

	// A dot adds half of what precedes it: one dot is 3/2, two is 7/4, three
	// is 15/8. That series is 2 - 2^-n, which is why the count is exponentiated
	// rather than looped over.
	if r.AugmentationDot != nil {
		n := intOr(r.AugmentationDot.Count, 0)
		if n > maxAugmentationDots {
			n = maxAugmentationDots
		}
		if n > 0 {
			base *= 2 - math.Ldexp(1, -n)
		}
	}

	// A tuplet squeezes num of these into the space of den, so each one lasts
	// den/num of its written value: a triplet is num=3 den=2, giving 2/3.
	if t := r.PrimaryTuplet; t != nil {
		num, numOK := parseInt(t.Num)
		den, denOK := parseInt(t.Den)
		if numOK && denOK && num > 0 && den > 0 {
			base *= float64(den) / float64(num)
		}
	}

	if base <= 0 || math.IsNaN(base) || math.IsInf(base, 0) {
		return 0, false
	}
	return base, true
}
