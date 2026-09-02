package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tedkulp/drops/internal/model"
)

// BlockedIssue pairs one candidate with its still-open blockers.
type BlockedIssue struct {
	Issue     model.Issue `json:"issue"`
	BlockedBy []model.ID  `json:"blocked_by"`
}

// Ready returns open, live, non-deferred Issues with no open blocks edge.
func (core *Core) Ready(ctx context.Context, filter IssueFilter) ([]model.Issue, error) {
	limit := filter.Limit
	filter.Limit = 0
	filter.Statuses = []model.Status{model.StatusOpen}
	candidates, err := core.store.Issues(ctx, filter)
	if err != nil {
		return nil, err
	}
	blockers, err := core.openBlockers(ctx)
	if err != nil {
		return nil, err
	}
	now := core.currentTime()
	ready := make([]model.Issue, 0, len(candidates))
	for _, issue := range candidates {
		if deferred(issue, now) || len(blockers[issue.ID]) != 0 {
			continue
		}
		ready = append(ready, issue)
		if limit > 0 && len(ready) == limit {
			break
		}
	}
	return ready, nil
}

// Blocked returns the exact complement of Ready over open, live,
// non-deferred candidates.
func (core *Core) Blocked(ctx context.Context, filter IssueFilter) ([]BlockedIssue, error) {
	limit := filter.Limit
	filter.Limit = 0
	filter.Statuses = []model.Status{model.StatusOpen}
	candidates, err := core.store.Issues(ctx, filter)
	if err != nil {
		return nil, err
	}
	blockers, err := core.openBlockers(ctx)
	if err != nil {
		return nil, err
	}
	now := core.currentTime()
	blocked := make([]BlockedIssue, 0)
	for _, issue := range candidates {
		if deferred(issue, now) || len(blockers[issue.ID]) == 0 {
			continue
		}
		blocked = append(blocked, BlockedIssue{Issue: issue, BlockedBy: append([]model.ID(nil), blockers[issue.ID]...)})
		if limit > 0 && len(blocked) == limit {
			break
		}
	}
	return blocked, nil
}

func (core *Core) openBlockers(ctx context.Context) (map[model.ID][]model.ID, error) {
	issues, err := core.store.Issues(ctx, IssueFilter{IncludeTombstoned: true})
	if err != nil {
		return nil, err
	}
	terminal := make(map[model.ID]bool, len(issues))
	for _, issue := range issues {
		terminal[issue.ID] = issue.Tombstone == model.Tombstoned || issue.Status == model.StatusClosed
	}
	dependencies, err := core.store.Dependencies(ctx)
	if err != nil {
		return nil, err
	}
	blockers := make(map[model.ID][]model.ID)
	for _, dependency := range dependencies {
		if dependency.Tombstone == model.Tombstoned || dependency.Type != model.DepBlocks {
			continue
		}
		if terminal[dependency.FromID] {
			continue
		}
		isTerminal, known := terminal[dependency.ToID]
		if !known || !isTerminal {
			blockers[dependency.FromID] = append(blockers[dependency.FromID], dependency.ToID)
		}
	}
	return blockers, nil
}

func deferred(issue model.Issue, now time.Time) bool {
	if issue.DeferredUntil == nil {
		return false
	}
	until, err := issue.DeferredUntil.Time()
	return err == nil && until.After(now)
}

// UnblockImpacts reports how many open candidates each named blocker alone
// prevents from becoming ready.
func (core *Core) UnblockImpacts(ctx context.Context, ids []model.ID) (map[model.ID]int, error) {
	blockers, err := core.openBlockers(ctx)
	if err != nil {
		return nil, err
	}
	wanted := make(map[model.ID]struct{}, len(ids))
	impact := make(map[model.ID]int, len(ids))
	for _, id := range ids {
		wanted[id], impact[id] = struct{}{}, 0
	}
	for _, open := range blockers {
		if len(open) != 1 {
			continue
		}
		if _, ok := wanted[open[0]]; ok {
			impact[open[0]]++
		}
	}
	return impact, nil
}

// LongestBlockerChain returns the deepest simple path of open blockers, with id
// first. Cycles terminate at the first repeated node.
func (core *Core) LongestBlockerChain(ctx context.Context, id model.ID) ([]model.ID, error) {
	if _, err := core.store.Issue(ctx, id); err != nil {
		return nil, err
	}
	graph, err := core.openBlockers(ctx)
	if err != nil {
		return nil, err
	}
	onPath := map[model.ID]bool{}
	var walk func(model.ID) []model.ID
	walk = func(node model.ID) []model.ID {
		if onPath[node] {
			return nil
		}
		onPath[node] = true
		defer func() { onPath[node] = false }()
		best := []model.ID{}
		for _, blocker := range graph[node] {
			if candidate := walk(blocker); len(candidate) > len(best) {
				best = candidate
			}
		}
		return append([]model.ID{node}, best...)
	}
	return walk(id), nil
}

// DependencyCycles returns canonical cycles of live blocks edges.
func (core *Core) DependencyCycles(ctx context.Context) ([][]model.ID, error) {
	dependencies, err := core.store.Dependencies(ctx)
	if err != nil {
		return nil, err
	}
	graph := map[model.ID][]model.ID{}
	for _, dependency := range dependencies {
		if dependency.Type == model.DepBlocks && dependency.Tombstone == model.Live {
			graph[dependency.FromID] = append(graph[dependency.FromID], dependency.ToID)
		}
	}
	for node := range graph {
		slices.Sort(graph[node])
	}
	cycles := [][]model.ID{}
	state := map[model.ID]uint8{}
	stack := []model.ID{}
	position := map[model.ID]int{}
	var visit func(model.ID)
	visit = func(node model.ID) {
		state[node], position[node] = 1, len(stack)
		stack = append(stack, node)
		for _, next := range graph[node] {
			if state[next] == 0 {
				visit(next)
			} else if state[next] == 1 {
				cycle := append([]model.ID(nil), stack[position[next]:]...)
				cycle = canonicalCycle(cycle)
				if !containsCycle(cycles, cycle) {
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(position, node)
		state[node] = 2
	}
	nodes := make([]model.ID, 0, len(graph))
	for node := range graph {
		nodes = append(nodes, node)
	}
	slices.Sort(nodes)
	for _, node := range nodes {
		if state[node] == 0 {
			visit(node)
		}
	}
	slices.SortFunc(cycles, func(left, right []model.ID) int {
		return strings.Compare(fmt.Sprint(left), fmt.Sprint(right))
	})
	return cycles, nil
}

func canonicalCycle(cycle []model.ID) []model.ID {
	if len(cycle) == 0 {
		return cycle
	}
	min := 0
	for i := 1; i < len(cycle); i++ {
		if cycle[i] < cycle[min] {
			min = i
		}
	}
	return append(append([]model.ID(nil), cycle[min:]...), cycle[:min]...)
}

func containsCycle(cycles [][]model.ID, want []model.ID) bool {
	for _, cycle := range cycles {
		if slices.Equal(cycle, want) {
			return true
		}
	}
	return false
}
