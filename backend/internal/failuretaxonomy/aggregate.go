package failuretaxonomy

import "sort"

func Aggregate(executionID string, rows []Occurrence) Results {
	type bucket struct {
		group     FailureGroup
		trials    map[string]bool
		sources   map[string]bool
		scenarios map[string]*scenarioBucket
	}
	typeKey := func(r Occurrence) string { return r.AgentVersionID + "\x00" + Category(r.Code) + "\x00" + r.Code }
	buckets := map[string]*bucket{}
	for _, row := range rows {
		if row.Code == "" || row.SourceID == "" {
			continue
		}
		key := typeKey(row)
		b := buckets[key]
		if b == nil {
			category := Category(row.Code)
			b = &bucket{group: FailureGroup{AgentVersionID: row.AgentVersionID, Category: category, Code: row.Code, AgentRelated: IsAgentRelated(category), PlatformRelated: category == CategoryPlatform}, trials: map[string]bool{}, sources: map[string]bool{}, scenarios: map[string]*scenarioBucket{}}
			buckets[key] = b
		}
		if b.sources[row.SourceID] {
			continue
		}
		b.sources[row.SourceID] = true
		b.group.InvalidPlatformEvidence = b.group.InvalidPlatformEvidence || row.PlatformInvalid
		b.group.TotalOccurrences++
		b.trials[row.RunID] = true
		sb := b.scenarios[row.ScenarioVersionID]
		if sb == nil {
			sb = &scenarioBucket{trials: map[string]bool{}}
			b.scenarios[row.ScenarioVersionID] = sb
		}
		sb.occurrences++
		sb.trials[row.RunID] = true
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := Results{ExecutionID: executionID, TaxonomyVersion: Version}
	for _, key := range keys {
		b := buckets[key]
		b.group.AffectedTrials = len(b.trials)
		ids := make([]string, 0, len(b.scenarios))
		for id := range b.scenarios {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			sb := b.scenarios[id]
			b.group.Scenarios = append(b.group.Scenarios, ScenarioCount{ScenarioVersionID: id, AffectedTrials: len(sb.trials), TotalOccurrences: sb.occurrences})
		}
		out.Failures = append(out.Failures, b.group)
	}
	return out
}

type scenarioBucket struct {
	trials      map[string]bool
	occurrences int
}
