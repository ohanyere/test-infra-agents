package observablecatalog

import (
	"errors"
	"testing"
)

func TestCanonicalAndLegacyObservableNames(t *testing.T) {
	for _, definition := range Definitions() {
		t.Run(string(definition.ID), func(t *testing.T) {
			if definition.Capability == "" {
				t.Fatal("missing runtime capability")
			}
			for _, name := range append([]string{string(definition.ID)}, definition.Aliases...) {
				id, err := Parse(name)
				if err != nil || id != definition.ID {
					t.Fatalf("Parse(%q) = %q, %v", name, id, err)
				}
				canonical, err := Canonical(name)
				if err != nil || canonical != string(definition.ID) {
					t.Fatalf("Canonical(%q) = %q, %v", name, canonical, err)
				}
			}
		})
	}
}

func TestUnsupportedAndMalformedObservables(t *testing.T) {
	for _, name := range []string{"cpu", "workloads", "latency", "desired-manifest"} {
		if _, err := Parse(name); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Parse(%q) = %v, want unsupported", name, err)
		}
	}
	for _, name := range []string{"", "CPU", "request__rate", "bad value"} {
		if _, err := Parse(name); !errors.Is(err, ErrMalformed) {
			t.Errorf("Parse(%q) = %v, want malformed", name, err)
		}
	}
}

func TestHistoricalAliasesCanonicalizeWithoutRewritingUnknownValues(t *testing.T) {
	got := CanonicalizeKnown([]string{"kubernetes_workloads", "request_rate", "cpu"})
	want := []string{"kubernetes-workloads", "request-rate", "cpu"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("canonicalized[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	known := KnownOnly([]string{"kubernetes_workloads", "kubernetes-workloads", "cpu"})
	if len(known) != 1 || known[0] != "kubernetes-workloads" {
		t.Fatalf("KnownOnly = %v", known)
	}
}
