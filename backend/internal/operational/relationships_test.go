package operational

import (
	"testing"
)

func TestEpisodeRelationshipsKeepObservedAndInferredDistinct(t *testing.T) {
	r := syntheticAuthenticatedShape()
	r.Contracts = []Contract{{ID: "contract-1", Version: 3, Service: "checkout/checkout"}}
	ep, err := episodeFromReport("w", "e", "s", r)
	if err != nil {
		t.Fatal(err)
	}
	ep.ID = "episode-1"
	edges := episodeRelationships(ep)
	if len(edges) == 0 {
		t.Fatal("no relationships")
	}
	seenAction, seenService, seenContract := false, false, false
	for _, edge := range edges {
		switch edge.Relation {
		case "affects":
			seenAction = true
			if !edge.Observed || edge.SourceReference != "audit-1" {
				t.Fatalf("authenticated action lost provenance: %+v", edge)
			}
		case "associated_service":
			seenService = true
			if edge.Observed {
				t.Fatal("service association promoted to observation")
			}
		case "evaluated_against":
			seenContract = true
			if edge.ToRef != "contract-1/v3" {
				t.Fatalf("contract version lost: %+v", edge)
			}
		}
	}
	if !seenAction || !seenService || !seenContract {
		t.Fatalf("missing relationships: %+v", edges)
	}
	r.ObservedActions[0].OriginAuthenticated = false
	ep, err = episodeFromReport("w", "e", "s", r)
	if err != nil {
		t.Fatal(err)
	}
	ep.ID = "episode-2"
	for _, edge := range episodeRelationships(ep) {
		if edge.Relation == "affects" && edge.Observed {
			t.Fatal("unauthenticated action promoted")
		}
	}
}
