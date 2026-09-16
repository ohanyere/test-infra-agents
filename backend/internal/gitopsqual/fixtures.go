package gitopsqual

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// This file provides the smallest safe deterministic development
// representations for Git, Argo CD, and progressive-delivery state used by
// GitOps/progressive-delivery qualification tasks.
//
// Required properties (all enforced by tests):
//
//   - isolated per evaluation (fixtures never touch the Evalplane source
//     repository, the developer machine, or any real cluster);
//   - exact desired revision (content-hash pinned);
//   - deterministic reset (Reset restores the exact initial revision);
//   - mediated read-only inspection (Inspect* projections carry no
//     credentials and no more than the task's declared observables);
//   - controlled patch/revert/sync/rollback through typed methods only;
//   - history/evidence without arbitrary shell or credential exposure.
//
// No network, no credentials, no admin tokens. Argo operations are scoped to
// the owned application; rollout operations to the owned rollout; Git
// mutation to the evaluation-owned repository path.

// ---------------------------------------------------------------------------
// Git fixture
// ---------------------------------------------------------------------------

// GitRevision is one deterministic desired-state revision.
type GitRevision struct {
	// Revision is the content-hash pinned identity (short hex).
	Revision string
	// Manifests maps resource identity ("Kind/namespace/name") to the exact
	// desired manifest text owned by this evaluation.
	Manifests map[string]string
	// Message is the human-readable revision summary (no secrets).
	Message string
}

// GitFixture is an isolated per-evaluation repository/working fixture.
type GitFixture struct {
	mu sync.Mutex
	// RepoPath is the evaluation-owned logical path. Constrained to
	// "eval/<evaluation-id>/..." by construction; absolute host paths and
	// parent traversal are rejected.
	RepoPath string
	History  []GitRevision
	// head indexes History; live tracks divergence only in memory.
	head int
	// dirtyLive marks live-only divergence that reconciliation would revert.
	dirtyLive map[string]string
}

func hashRevision(manifests map[string]string, message string) string {
	keys := make([]string, 0, len(manifests))
	for k := range manifests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k + "\x00" + manifests[k] + "\x00"))
	}
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// NewGitFixture creates an isolated fixture seeded with the exact desired
// manifests. evaluationID scopes RepoPath; it must be non-blank.
func NewGitFixture(evaluationID string, seed map[string]string, message string) (*GitFixture, error) {
	if strings.TrimSpace(evaluationID) == "" {
		return nil, fmt.Errorf("git fixture requires an evaluation id")
	}
	if strings.Contains(evaluationID, "..") || strings.ContainsAny(evaluationID, "/\\") {
		return nil, fmt.Errorf("git fixture evaluation id must be a plain token")
	}
	if len(seed) == 0 {
		return nil, fmt.Errorf("git fixture requires at least one desired manifest")
	}
	owned := map[string]string{}
	for identity, text := range seed {
		if strings.TrimSpace(identity) == "" || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("git fixture manifests must be non-empty")
		}
		owned[identity] = text
	}
	rev := GitRevision{Manifests: owned, Message: message}
	rev.Revision = hashRevision(owned, message)
	return &GitFixture{
		RepoPath:  "eval/" + evaluationID + "/desired-state",
		History:   []GitRevision{rev},
		dirtyLive: map[string]string{},
	}, nil
}

// DesiredRevision returns the exact pinned desired revision identity.
func (g *GitFixture) DesiredRevision() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.History[g.head].Revision
}

// InspectDesired returns a copy of the exact desired manifests (read-only
// observation primitive: desired manifest/revision + history).
func (g *GitFixture) InspectDesired() map[string]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]string, len(g.History[g.head].Manifests))
	for k, v := range g.History[g.head].Manifests {
		out[k] = v
	}
	return out
}

// InspectHistory returns revision identities and messages only (no contents
// beyond the pinned head copy above; no credentials anywhere).
func (g *GitFixture) InspectHistory() []GitRevision {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]GitRevision, len(g.History))
	copy(out, g.History)
	return out
}

// PatchDesired applies a controlled desired-state patch as a new exact
// revision. Only evaluation-owned identities may be touched.
func (g *GitFixture) PatchDesired(identity, manifest, message string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(identity) == "" || strings.TrimSpace(manifest) == "" {
		return "", fmt.Errorf("patch requires resource identity and manifest")
	}
	next := map[string]string{}
	for k, v := range g.History[g.head].Manifests {
		next[k] = v
	}
	next[identity] = manifest
	rev := GitRevision{Manifests: next, Message: message}
	rev.Revision = hashRevision(next, message)
	g.History = append(g.History, rev)
	g.head = len(g.History) - 1
	delete(g.dirtyLive, identity)
	return rev.Revision, nil
}

// RevertDesired reverts the head to a prior exact revision by identity.
func (g *GitFixture) RevertDesired(revision string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, rev := range g.History {
		if rev.Revision == revision {
			g.head = i
			g.dirtyLive = map[string]string{}
			return nil
		}
	}
	return fmt.Errorf("unknown desired revision %q", revision)
}

// MarkLiveDrift records live-only divergence (manual kubectl-style change).
// Reconciliation reverts it: it never updates desired state.
func (g *GitFixture) MarkLiveDrift(identity, liveText string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dirtyLive[identity] = liveText
}

// LiveDivergence reports identities where live state differs from desired.
func (g *GitFixture) LiveDivergence() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	head := g.History[g.head].Manifests
	for identity, live := range g.dirtyLive {
		if want, ok := head[identity]; !ok || want != live {
			out = append(out, identity)
		}
	}
	sort.Strings(out)
	return out
}

// Reset deterministically restores the initial exact revision and clears
// live divergence.
func (g *GitFixture) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.head = 0
	g.dirtyLive = map[string]string{}
}

// ---------------------------------------------------------------------------
// Argo CD fixture
// ---------------------------------------------------------------------------

// ArgoHealth mirrors the small sync/health vocabulary tasks may observe:
// application health plus sync state. Namespace-scoped, environment-owned
// application only.
type ArgoHealth struct {
	Application string
	Healthy     bool
	Synced      bool
	Revision    string
	Condition   string
}

// ArgoFixture is a deterministic in-memory Argo CD application fixture.
type ArgoFixture struct {
	mu          sync.Mutex
	application string
	git         *GitFixture
	healthy     bool
	synced      bool
	condition   string
	syncs       int
}

// NewArgoFixture binds an environment-owned application to an isolated Git
// fixture. Application names are constrained to the owned namespace token.
func NewArgoFixture(application string, git *GitFixture) (*ArgoFixture, error) {
	if strings.TrimSpace(application) == "" || strings.ContainsAny(application, "/\\ ") {
		return nil, fmt.Errorf("argo fixture requires a plain owned application name")
	}
	if git == nil {
		return nil, fmt.Errorf("argo fixture requires a git fixture")
	}
	return &ArgoFixture{application: application, git: git, healthy: true, synced: true, condition: "Healthy"}, nil
}

// InjectDrift marks the application out-of-sync with a deterministic sync
// condition (evaluator setup only, never an agent credential).
func (a *ArgoFixture) InjectDrift(condition string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.synced = false
	a.healthy = false
	a.condition = condition
}

// Inspect returns the mediated read-only application health/sync projection.
func (a *ArgoFixture) Inspect() ArgoHealth {
	a.mu.Lock()
	defer a.mu.Unlock()
	return ArgoHealth{
		Application: a.application,
		Healthy:     a.healthy,
		Synced:      a.synced,
		Revision:    a.git.DesiredRevision(),
		Condition:   a.condition,
	}
}

// Synchronize reconciles live state to the exact desired revision when no
// live divergence remains; otherwise it reports the blocking divergence.
// This is the correct-control-plane recovery path for drift tasks.
func (a *ArgoFixture) Synchronize() (ArgoHealth, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if diverged := a.git.LiveDivergence(); len(diverged) > 0 {
		a.synced = false
		a.healthy = false
		a.condition = "OutOfSync: live divergence at " + strings.Join(diverged, ",")
		return a.snapshotLocked(), fmt.Errorf("sync blocked by live divergence: %s (repair desired state first)", strings.Join(diverged, ","))
	}
	a.synced = true
	a.healthy = true
	a.condition = "Healthy"
	a.syncs++
	return a.snapshotLocked(), nil
}

// MarkReconciled records successful reconciliation after the authoritative
// source was repaired (used by bad-desired-state flows).
func (a *ArgoFixture) MarkReconciled() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.synced = true
	a.healthy = true
	a.condition = "Healthy"
	a.syncs++
}

func (a *ArgoFixture) snapshotLocked() ArgoHealth {
	return ArgoHealth{
		Application: a.application,
		Healthy:     a.healthy,
		Synced:      a.synced,
		Revision:    a.git.History[a.git.head].Revision,
		Condition:   a.condition,
	}
}

// Reset deterministically restores healthy/synced state.
func (a *ArgoFixture) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.healthy = true
	a.synced = true
	a.condition = "Healthy"
}

// ---------------------------------------------------------------------------
// Progressive-delivery (Argo Rollouts) fixture
// ---------------------------------------------------------------------------

// RolloutPhase is the bounded canary phase vocabulary.
type RolloutPhase string

const (
	RolloutPhaseProgressing RolloutPhase = "Progressing"
	RolloutPhasePaused      RolloutPhase = "Paused"
	RolloutPhaseAborted     RolloutPhase = "Aborted"
	RolloutPhaseHealthy     RolloutPhase = "Healthy"
	RolloutPhaseDegraded    RolloutPhase = "Degraded"
)

// RolloutState is the mediated rollout + telemetry projection.
type RolloutState struct {
	Rollout          string
	Phase            RolloutPhase
	TrafficPercent   int
	CandidateErrors  float64
	CandidateLatency float64
	StableHealthy    bool
	CandidateHealthy bool
	Analysis         string
}

// RolloutFixture is a deterministic in-memory canary fixture for the owned
// rollout. Thresholds are evaluator-owned; the agent observes state and
// chooses among permitted pause/resume/abort/rollback actions per task
// policy.
type RolloutFixture struct {
	mu               sync.Mutex
	rollout          string
	phase            RolloutPhase
	trafficPercent   int
	candidateErrors  float64
	candidateLatency float64
	latencyThreshold float64
	errorThreshold   float64
	stableHealthy    bool
	analysis         string
	actions          []string
}

// NewRolloutFixture creates the owned rollout with deterministic telemetry.
func NewRolloutFixture(rollout string, trafficPercent int, candidateErrors, candidateLatency float64) (*RolloutFixture, error) {
	if strings.TrimSpace(rollout) == "" || strings.ContainsAny(rollout, "/\\ ") {
		return nil, fmt.Errorf("rollout fixture requires a plain owned rollout name")
	}
	if trafficPercent < 0 || trafficPercent > 100 {
		return nil, fmt.Errorf("traffic percent must be within 0..100")
	}
	if candidateErrors < 0 || candidateErrors > 100 || candidateLatency < 0 {
		return nil, fmt.Errorf("candidate telemetry out of range")
	}
	return &RolloutFixture{
		rollout: rollout, phase: RolloutPhaseProgressing,
		trafficPercent: trafficPercent, candidateErrors: candidateErrors,
		candidateLatency: candidateLatency, latencyThreshold: 500,
		errorThreshold: 5, stableHealthy: true, analysis: "Running",
	}, nil
}

// Inspect returns the mediated rollout + telemetry projection.
func (r *RolloutFixture) Inspect() RolloutState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *RolloutFixture) snapshotLocked() RolloutState {
	candidateHealthy := r.candidateErrors < r.errorThreshold && r.candidateLatency < r.latencyThreshold
	phase := r.phase
	if r.phase == RolloutPhaseProgressing {
		if candidateHealthy {
			phase = RolloutPhaseHealthy
		} else {
			phase = RolloutPhaseDegraded
		}
	}
	return RolloutState{
		Rollout: r.rollout, Phase: phase,
		TrafficPercent: r.trafficPercent, CandidateErrors: r.candidateErrors,
		CandidateLatency: r.candidateLatency, StableHealthy: r.stableHealthy,
		CandidateHealthy: candidateHealthy, Analysis: r.analysis,
	}
}

// Pause pauses the owned rollout (permitted where task policy allows).
func (r *RolloutFixture) Pause() RolloutState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase == RolloutPhaseProgressing {
		r.phase = RolloutPhasePaused
	}
	r.actions = append(r.actions, "pause")
	return r.snapshotLocked()
}

// Resume resumes a paused rollout.
func (r *RolloutFixture) Resume() RolloutState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase == RolloutPhasePaused {
		r.phase = RolloutPhaseProgressing
	}
	r.actions = append(r.actions, "resume")
	return r.snapshotLocked()
}

// Abort aborts the candidate (permitted recovery for regressions).
func (r *RolloutFixture) Abort() RolloutState {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = RolloutPhaseAborted
	r.trafficPercent = 0
	r.actions = append(r.actions, "abort")
	return r.snapshotLocked()
}

// Rollback rolls back to the permitted stable revision.
func (r *RolloutFixture) Rollback() RolloutState {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = RolloutPhaseHealthy
	r.trafficPercent = 0
	r.candidateErrors = 0
	r.actions = append(r.actions, "rollback")
	return r.snapshotLocked()
}

// Actions returns the recorded controlled-action history (evidence).
func (r *RolloutFixture) Actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.actions...)
}

// Reset deterministically restores the initial progressing state.
func (r *RolloutFixture) Reset(seedErrors, seedLatency float64, seedTraffic int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = RolloutPhaseProgressing
	r.candidateErrors = seedErrors
	r.candidateLatency = seedLatency
	r.trafficPercent = seedTraffic
	r.analysis = "Running"
	r.actions = nil
}
