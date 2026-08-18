package knowledge

import (
	"encoding/json"
	"testing"
)

// A profile ratified at zero hard-negative hits must hold the floor at zero.
//
// The ratified count is what a calibration compares a challenger against, and
// zero is its best possible value. Recorded as a plain integer with omitempty,
// a profile accepted at zero persisted nothing, decoded back as zero, and was
// then read as "no ratified value" - so the ceiling followed the incumbent's
// live score upward in exactly the case where it must not move at all. A
// corpus edit that gave the incumbent two hard-negative hits would license a
// challenger to have two as well, and the drift went unrecorded.
func TestRatchetHoldsAtZeroRatifiedHardNegativeHits(t *testing.T) {
	zero := 0
	ratified := BenchmarkEvidence{
		Suite: "project-calibration-v1", Status: "passed",
		RatifiedHardNegativeHits: &zero,
	}
	// The round trip is the defect: an in-memory value that never survives
	// serialization is not a ratchet.
	body, err := json.Marshal(ratified)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded BenchmarkEvidence
	if err := json.Unmarshal(body, &reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.RatifiedHardNegativeHits == nil {
		t.Fatalf(
			"a profile ratified at zero hard-negative hits persisted nothing: %s",
			body)
	}

	live := QualityMetrics{HardNegativeHits: 2, AbstentionAccuracy: 0.5}
	clamped := ratchetBaseline(reloaded, live)
	if clamped.HardNegativeHits != 0 {
		t.Fatalf(
			"the ratchet let the hard-negative floor drift to %d after "+
				"ratification at 0",
			clamped.HardNegativeHits)
	}
}

// The ratchet tightens and never loosens, and stays disengaged for profiles
// promoted before the ratified fields existed.
func TestRatchetTightensNeverLoosensAndStaysBackCompatible(t *testing.T) {
	three := 3
	cases := []struct {
		name     string
		ratified BenchmarkEvidence
		live     QualityMetrics
		wantHits int
		wantRate float64
	}{
		{
			name:     "absent leaves the live baseline untouched",
			ratified: BenchmarkEvidence{},
			live:     QualityMetrics{HardNegativeHits: 2, AbstentionAccuracy: 0.5},
			wantHits: 2, wantRate: 0.5,
		},
		{
			name: "a looser ratified count never raises the ceiling",
			ratified: BenchmarkEvidence{
				RatifiedHardNegativeHits: &three,
			},
			live:     QualityMetrics{HardNegativeHits: 2, AbstentionAccuracy: 0.5},
			wantHits: 2, wantRate: 0.5,
		},
		{
			name: "a tighter ratified abstention accuracy raises the floor",
			ratified: BenchmarkEvidence{
				RatifiedAbstentionAccuracy: 0.9,
			},
			live:     QualityMetrics{HardNegativeHits: 2, AbstentionAccuracy: 0.5},
			wantHits: 2, wantRate: 0.9,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clamped := ratchetBaseline(testCase.ratified, testCase.live)
			if clamped.HardNegativeHits != testCase.wantHits ||
				clamped.AbstentionAccuracy != testCase.wantRate {
				t.Fatalf(
					"clamped baseline = %d hits / %.2f accuracy, want %d / %.2f",
					clamped.HardNegativeHits, clamped.AbstentionAccuracy,
					testCase.wantHits, testCase.wantRate)
			}
		})
	}
}

// A promoted profile that records a ratified zero must survive a clone, and a
// clone must not alias the original's recorded value.
func TestRatifiedHardNegativeHitsSurviveProfileCloning(t *testing.T) {
	zero := 0
	original := EffectiveProfile{
		Name: "hybrid-local-v1",
		Benchmark: BenchmarkEvidence{
			Suite: "project-calibration-v1", Status: "passed",
			RatifiedHardNegativeHits: &zero,
		},
	}
	clone := cloneEffectiveProfile(original)
	if clone.Benchmark.RatifiedHardNegativeHits == nil ||
		*clone.Benchmark.RatifiedHardNegativeHits != 0 {
		t.Fatalf("cloning dropped the ratified hard-negative count: %#v",
			clone.Benchmark)
	}
	if clone.Benchmark.RatifiedHardNegativeHits ==
		original.Benchmark.RatifiedHardNegativeHits {
		t.Fatal("cloned profile aliases the original ratified count")
	}
}
