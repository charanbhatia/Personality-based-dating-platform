package domain

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// TraitsVersion is the scoring-algorithm version persisted alongside every
// trait vector. Bump it whenever normalization or the question bank changes
// meaning, so old and new vectors stay distinguishable.
const TraitsVersion = 1

// Big Five trait keys. These are the JSONB keys in personality_scores.traits
// and the keys accepted in preferences.trait_weights.
const (
	TraitOpenness          = "openness"
	TraitConscientiousness = "conscientiousness"
	TraitExtraversion      = "extraversion"
	TraitAgreeableness     = "agreeableness"
	TraitNeuroticism       = "neuroticism"
)

// traitKeys is the canonical ordering used by the discovery SQL, so the Go and
// SQL scorers stay in lockstep.
var traitKeys = []string{
	TraitOpenness,
	TraitConscientiousness,
	TraitExtraversion,
	TraitAgreeableness,
	TraitNeuroticism,
}

// TraitKeys returns the Big Five keys in canonical order.
func TraitKeys() []string {
	out := make([]string, len(traitKeys))
	copy(out, traitKeys)
	return out
}

// IsTraitKey reports whether key is one of the Big Five.
func IsTraitKey(key string) bool {
	for _, k := range traitKeys {
		if k == key {
			return true
		}
	}
	return false
}

// NeutralScore is returned when compatibility cannot be computed, e.g. one side
// has no assessment. It is deliberately the midpoint so an unscored user is
// neither promoted nor buried.
const NeutralScore = 0.5

// MaxTraitWeight bounds preference weights, preventing a single trait from
// dominating so heavily that the score loses meaning.
const MaxTraitWeight = 5.0

var ErrIncompleteTraits = errors.New("trait vector is missing one or more Big Five keys")

// Traits is a Big Five vector with each value normalized to [0, 1].
type Traits map[string]float64

// Complete reports whether all five traits are present and within range.
func (t Traits) Complete() bool {
	if len(t) == 0 {
		return false
	}
	for _, k := range traitKeys {
		v, ok := t[k]
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return false
		}
	}
	return true
}

// Validate returns a descriptive error for an unusable vector.
func (t Traits) Validate() error {
	for _, k := range traitKeys {
		v, ok := t[k]
		if !ok {
			return fmt.Errorf("%w: %s", ErrIncompleteTraits, k)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("trait %s is not a finite number", k)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("trait %s must be within [0, 1], got %v", k, v)
		}
	}
	return nil
}

// Ordered returns the vector as parallel key/value slices in canonical order,
// which is how the values are bound into the discovery query.
func (t Traits) Ordered() []float64 {
	out := make([]float64, len(traitKeys))
	for i, k := range traitKeys {
		out[i] = t[k]
	}
	return out
}

// TraitWeights are per-trait multipliers from user preferences. A missing key
// means 1.0 (neutral).
type TraitWeights map[string]float64

// Weight returns the multiplier for key, defaulting to 1.0. Non-finite and
// out-of-range values are clamped rather than rejected, so a bad stored value
// degrades the ranking instead of failing the request.
func (w TraitWeights) Weight(key string) float64 {
	v, ok := w[key]
	if !ok || math.IsNaN(v) {
		return 1
	}
	if v < 0 {
		return 0
	}
	if v > MaxTraitWeight {
		return MaxTraitWeight
	}
	return v
}

// Ordered returns the effective weights in canonical trait order. When the
// weights sum to zero (all traits disabled) it falls back to a neutral vector,
// guaranteeing the scorer never divides by zero.
func (w TraitWeights) Ordered() []float64 {
	out := make([]float64, len(traitKeys))
	var total float64
	for i, k := range traitKeys {
		out[i] = w.Weight(k)
		total += out[i]
	}
	if total <= 0 {
		for i := range out {
			out[i] = 1
		}
	}
	return out
}

// Validate rejects unknown keys and out-of-range multipliers supplied by a
// client. Keys are reported sorted so the message is deterministic.
func (w TraitWeights) Validate() error {
	unknown := make([]string, 0, len(w))
	for k, v := range w {
		if !IsTraitKey(k) {
			unknown = append(unknown, k)
			continue
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("weight for %s must be a finite number", k)
		}
		if v < 0 || v > MaxTraitWeight {
			return fmt.Errorf("weight for %s must be within [0, %g]", k, MaxTraitWeight)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown trait keys: %v", unknown)
	}
	return nil
}

// Compatibility scores two trait vectors as the weighted mean of per-trait
// agreement, where agreement is 1 - |a - b|. The result is in [0, 1] and is
// symmetric when both sides use the same weights.
//
// This must stay numerically equivalent to the expression in the discovery
// query (matching.discoverScoreSQL); matching_test asserts the two agree.
func Compatibility(a, b Traits, weights TraitWeights) float64 {
	if !a.Complete() || !b.Complete() {
		return NeutralScore
	}
	effective := weights.Ordered()
	var weighted, total float64
	for i, k := range traitKeys {
		agreement := 1 - math.Abs(a[k]-b[k])
		weighted += effective[i] * agreement
		total += effective[i]
	}
	if total <= 0 {
		return NeutralScore
	}
	return clamp01(weighted / total)
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return NeutralScore
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ScorePrecision is the number of decimal places compatibility scores are
// rounded to. Discovery pagination compares scores for equality to break ties,
// so both the SQL and Go scorers must round identically.
const ScorePrecision = 6

// RoundScore rounds to ScorePrecision decimal places, matching
// round(x::numeric, 6) in Postgres.
func RoundScore(v float64) float64 {
	factor := math.Pow(10, ScorePrecision)
	return math.Round(v*factor) / factor
}
