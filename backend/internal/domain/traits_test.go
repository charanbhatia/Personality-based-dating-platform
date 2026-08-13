package domain

import (
	"math"
	"testing"
)

func fullTraits(v float64) Traits {
	t := make(Traits, 5)
	for _, k := range TraitKeys() {
		t[k] = v
	}
	return t
}

func TestTraitsComplete(t *testing.T) {
	cases := []struct {
		name   string
		traits Traits
		want   bool
	}{
		{"all present", fullTraits(0.5), true},
		{"bounds inclusive", Traits{
			TraitOpenness: 0, TraitConscientiousness: 1, TraitExtraversion: 0,
			TraitAgreeableness: 1, TraitNeuroticism: 0.5,
		}, true},
		{"empty", Traits{}, false},
		{"nil", nil, false},
		{"missing one", Traits{
			TraitOpenness: 0.5, TraitConscientiousness: 0.5,
			TraitExtraversion: 0.5, TraitAgreeableness: 0.5,
		}, false},
		{"above range", Traits{
			TraitOpenness: 1.2, TraitConscientiousness: 0.5, TraitExtraversion: 0.5,
			TraitAgreeableness: 0.5, TraitNeuroticism: 0.5,
		}, false},
		{"negative", Traits{
			TraitOpenness: -0.1, TraitConscientiousness: 0.5, TraitExtraversion: 0.5,
			TraitAgreeableness: 0.5, TraitNeuroticism: 0.5,
		}, false},
		{"NaN", Traits{
			TraitOpenness: math.NaN(), TraitConscientiousness: 0.5, TraitExtraversion: 0.5,
			TraitAgreeableness: 0.5, TraitNeuroticism: 0.5,
		}, false},
		{"Inf", Traits{
			TraitOpenness: math.Inf(1), TraitConscientiousness: 0.5, TraitExtraversion: 0.5,
			TraitAgreeableness: 0.5, TraitNeuroticism: 0.5,
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.traits.Complete(); got != tc.want {
				t.Fatalf("Complete() = %v, want %v", got, tc.want)
			}
			// Validate must agree with Complete on every one of these.
			if err := tc.traits.Validate(); (err == nil) != tc.want {
				t.Fatalf("Validate() error = %v, but Complete() = %v", err, tc.want)
			}
		})
	}
}

func TestTraitsOrderedFollowsCanonicalOrder(t *testing.T) {
	traits := Traits{
		TraitOpenness: 0.1, TraitConscientiousness: 0.2, TraitExtraversion: 0.3,
		TraitAgreeableness: 0.4, TraitNeuroticism: 0.5,
	}
	got := traits.Ordered()
	want := []float64{0.1, 0.2, 0.3, 0.4, 0.5}
	if len(got) != len(want) {
		t.Fatalf("Ordered() length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Ordered()[%d] = %v, want %v (order must match the discovery query)", i, got[i], want[i])
		}
	}
}

func TestTraitKeysIsACopy(t *testing.T) {
	keys := TraitKeys()
	keys[0] = "tampered"
	if TraitKeys()[0] != TraitOpenness {
		t.Fatal("TraitKeys() exposed the package slice; callers can corrupt the canonical order")
	}
}

func TestTraitWeightsWeightClamping(t *testing.T) {
	w := TraitWeights{
		TraitOpenness:          -3,
		TraitConscientiousness: MaxTraitWeight + 10,
		TraitExtraversion:      math.NaN(),
		TraitAgreeableness:     2.5,
	}
	cases := map[string]float64{
		TraitOpenness:          0,
		TraitConscientiousness: MaxTraitWeight,
		TraitExtraversion:      1,
		TraitAgreeableness:     2.5,
		TraitNeuroticism:       1, // absent means neutral
	}
	for key, want := range cases {
		if got := w.Weight(key); got != want {
			t.Errorf("Weight(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestTraitWeightsOrderedNeverSumsToZero(t *testing.T) {
	allZero := TraitWeights{}
	for _, k := range TraitKeys() {
		allZero[k] = 0
	}
	got := allZero.Ordered()
	var total float64
	for _, v := range got {
		total += v
	}
	if total == 0 {
		t.Fatal("Ordered() summed to zero; the scorer would divide by zero")
	}
	for i, v := range got {
		if v != 1 {
			t.Fatalf("Ordered()[%d] = %v, want the neutral fallback 1", i, v)
		}
	}
}

func TestTraitWeightsValidate(t *testing.T) {
	cases := []struct {
		name    string
		weights TraitWeights
		wantErr bool
	}{
		{"empty", TraitWeights{}, false},
		{"in range", TraitWeights{TraitOpenness: 0, TraitNeuroticism: MaxTraitWeight}, false},
		{"unknown key", TraitWeights{"charisma": 1}, true},
		{"negative", TraitWeights{TraitOpenness: -0.1}, true},
		{"too large", TraitWeights{TraitOpenness: MaxTraitWeight + 0.1}, true},
		{"NaN", TraitWeights{TraitOpenness: math.NaN()}, true},
		{"Inf", TraitWeights{TraitOpenness: math.Inf(-1)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.weights.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestTraitWeightsValidateReportsUnknownKeysDeterministically(t *testing.T) {
	w := TraitWeights{"zeta": 1, "alpha": 1, "mu": 1}
	first := w.Validate()
	if first == nil {
		t.Fatal("expected an error for unknown keys")
	}
	// Map iteration order is random; the message must not be.
	for i := 0; i < 20; i++ {
		if got := w.Validate(); got.Error() != first.Error() {
			t.Fatalf("Validate() message varies between calls: %q vs %q", got, first)
		}
	}
}

func TestCompatibility(t *testing.T) {
	identical := fullTraits(0.5)
	opposite := Traits{
		TraitOpenness: 1, TraitConscientiousness: 1, TraitExtraversion: 1,
		TraitAgreeableness: 1, TraitNeuroticism: 1,
	}
	zeros := fullTraits(0)

	if got := Compatibility(identical, identical, nil); got != 1 {
		t.Errorf("identical vectors scored %v, want 1", got)
	}
	if got := Compatibility(zeros, opposite, nil); got != 0 {
		t.Errorf("maximally different vectors scored %v, want 0", got)
	}

	// Half a unit of disagreement on every trait: 1 - 0.5 = 0.5.
	if got := Compatibility(zeros, fullTraits(0.5), nil); math.Abs(got-0.5) > 1e-12 {
		t.Errorf("uniform 0.5 difference scored %v, want 0.5", got)
	}
}

func TestCompatibilityIsSymmetric(t *testing.T) {
	a := Traits{
		TraitOpenness: 0.2, TraitConscientiousness: 0.9, TraitExtraversion: 0.35,
		TraitAgreeableness: 0.6, TraitNeuroticism: 0.05,
	}
	b := Traits{
		TraitOpenness: 0.8, TraitConscientiousness: 0.1, TraitExtraversion: 0.5,
		TraitAgreeableness: 0.62, TraitNeuroticism: 0.99,
	}
	w := TraitWeights{TraitOpenness: 2, TraitNeuroticism: 0.25}
	if Compatibility(a, b, w) != Compatibility(b, a, w) {
		t.Fatal("Compatibility is not symmetric; A and B would see different scores for the same pair")
	}
}

func TestCompatibilityWeightsShiftTheScore(t *testing.T) {
	// Agree perfectly on openness, disagree completely on neuroticism.
	a := Traits{
		TraitOpenness: 0.5, TraitConscientiousness: 0.5, TraitExtraversion: 0.5,
		TraitAgreeableness: 0.5, TraitNeuroticism: 0,
	}
	b := Traits{
		TraitOpenness: 0.5, TraitConscientiousness: 0.5, TraitExtraversion: 0.5,
		TraitAgreeableness: 0.5, TraitNeuroticism: 1,
	}
	neutral := Compatibility(a, b, nil)
	discounted := Compatibility(a, b, TraitWeights{TraitNeuroticism: 0})
	emphasised := Compatibility(a, b, TraitWeights{TraitNeuroticism: MaxTraitWeight})

	if discounted <= neutral {
		t.Errorf("zeroing the disagreeing trait scored %v, want more than the neutral %v", discounted, neutral)
	}
	if emphasised >= neutral {
		t.Errorf("emphasising the disagreeing trait scored %v, want less than the neutral %v", emphasised, neutral)
	}
	if discounted != 1 {
		t.Errorf("with neuroticism disabled the remaining traits agree exactly; scored %v, want 1", discounted)
	}
}

func TestCompatibilityFallsBackToNeutral(t *testing.T) {
	complete := fullTraits(0.5)
	incomplete := Traits{TraitOpenness: 0.5}

	if got := Compatibility(incomplete, complete, nil); got != NeutralScore {
		t.Errorf("incomplete left vector scored %v, want %v", got, NeutralScore)
	}
	if got := Compatibility(complete, incomplete, nil); got != NeutralScore {
		t.Errorf("incomplete right vector scored %v, want %v", got, NeutralScore)
	}
	if got := Compatibility(nil, nil, nil); got != NeutralScore {
		t.Errorf("two nil vectors scored %v, want %v", got, NeutralScore)
	}
}

func TestCompatibilityStaysInRange(t *testing.T) {
	values := []float64{0, 0.01, 0.25, 0.5, 0.75, 0.99, 1}
	weights := []TraitWeights{
		nil,
		{TraitOpenness: 0},
		{TraitOpenness: MaxTraitWeight, TraitNeuroticism: 0},
	}
	for _, av := range values {
		for _, bv := range values {
			for _, w := range weights {
				got := Compatibility(fullTraits(av), fullTraits(bv), w)
				if got < 0 || got > 1 || math.IsNaN(got) {
					t.Fatalf("Compatibility(%v, %v, %v) = %v, want a value within [0, 1]", av, bv, w, got)
				}
			}
		}
	}
}

func TestRoundScore(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0.123456789, 0.123457},
		{0.1234564, 0.123456},
		{1, 1},
		{0, 0},
		{0.8604000000001, 0.8604},
	}
	for _, tc := range cases {
		if got := RoundScore(tc.in); got != tc.want {
			t.Errorf("RoundScore(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsTraitKey(t *testing.T) {
	for _, k := range TraitKeys() {
		if !IsTraitKey(k) {
			t.Errorf("IsTraitKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "Openness", "charisma", "openness "} {
		if IsTraitKey(k) {
			t.Errorf("IsTraitKey(%q) = true, want false", k)
		}
	}
}
