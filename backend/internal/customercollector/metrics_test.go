package customercollector

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMetricCandidateValidation(t *testing.T) {
	value := float64(0.5)
	in := MetricCandidateInput{SampleID: uuid.NewString(), ClaimedClusterIdentity: "cluster", Namespace: "checkout", Service: "checkout", SampledAt: time.Now(), ErrorRatePct: &value}
	if !validMetricCandidate(in) {
		t.Fatal("valid scoped metric rejected")
	}
	in.ErrorRatePct = nil
	if validMetricCandidate(in) {
		t.Fatal("empty metric accepted")
	}
	invalid := math.NaN()
	in.ErrorRatePct = &invalid
	if validMetricCandidate(in) {
		t.Fatal("NaN metric accepted")
	}
	in.ErrorRatePct = &value
	in.Namespace = "default/other"
	if validMetricCandidate(in) {
		t.Fatal("invalid namespace accepted")
	}
}
