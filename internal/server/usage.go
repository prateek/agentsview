package server

import (
	"go.kenn.io/agentsview/internal/db"
	"go.kenn.io/agentsview/internal/service"
)

type Comparison struct {
	PriorFrom      string  `json:"priorFrom"`
	PriorTo        string  `json:"priorTo"`
	PriorTotalCost float64 `json:"priorTotalCost"`
	DeltaPct       float64 `json:"deltaPct"`
}

type ProjectTotal struct {
	Project             string  `json:"project"`
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheCreationTokens int     `json:"cacheCreationTokens"`
	CacheReadTokens     int     `json:"cacheReadTokens"`
	Cost                float64 `json:"cost"`
}

type ModelTotal struct {
	Model               string  `json:"model"`
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheCreationTokens int     `json:"cacheCreationTokens"`
	CacheReadTokens     int     `json:"cacheReadTokens"`
	Cost                float64 `json:"cost"`
}

type AgentTotal struct {
	Agent               string  `json:"agent"`
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheCreationTokens int     `json:"cacheCreationTokens"`
	CacheReadTokens     int     `json:"cacheReadTokens"`
	Cost                float64 `json:"cost"`
}

type BranchTotal struct {
	Project             string  `json:"project"`
	Branch              string  `json:"branch"`
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheCreationTokens int     `json:"cacheCreationTokens"`
	CacheReadTokens     int     `json:"cacheReadTokens"`
	Cost                float64 `json:"cost"`
}

type CacheStats struct {
	CacheReadTokens     int     `json:"cacheReadTokens"`
	CacheCreationTokens int     `json:"cacheCreationTokens"`
	UncachedInputTokens int     `json:"uncachedInputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	HitRate             float64 `json:"hitRate"`
	SavingsVsUncached   float64 `json:"savingsVsUncached"`
}

type UnsupportedUsage struct {
	Kind string `json:"kind"`
}

// UsageSummaryResponse preserves the public OpenAPI schema for
// GET /api/v1/usage/summary while the handler delegates assembly to
// the transport-neutral service layer.
type UsageSummaryResponse struct {
	From             string                `json:"from"`
	To               string                `json:"to"`
	Totals           db.UsageTotals        `json:"totals"`
	Daily            []db.DailyUsageEntry  `json:"daily"`
	ProjectTotals    []ProjectTotal        `json:"projectTotals"`
	ModelTotals      []ModelTotal          `json:"modelTotals"`
	AgentTotals      []AgentTotal          `json:"agentTotals"`
	BranchTotals     []BranchTotal         `json:"branchTotals"`
	SessionCounts    db.UsageSessionCounts `json:"sessionCounts"`
	CacheStats       CacheStats            `json:"cacheStats"`
	UnsupportedUsage *UnsupportedUsage     `json:"unsupportedUsage,omitempty"`
	Comparison       *Comparison           `json:"comparison,omitempty"`
}

func usageSummaryResponseFromService(
	res *service.UsageSummaryResult,
) UsageSummaryResponse {
	return UsageSummaryResponse{
		From:             res.From,
		To:               res.To,
		Totals:           res.Totals,
		Daily:            res.Daily,
		ProjectTotals:    projectTotalsFromService(res.ProjectTotals),
		ModelTotals:      modelTotalsFromService(res.ModelTotals),
		AgentTotals:      agentTotalsFromService(res.AgentTotals),
		BranchTotals:     branchTotalsFromService(res.BranchTotals),
		SessionCounts:    res.SessionCounts,
		CacheStats:       cacheStatsFromService(res.CacheStats),
		UnsupportedUsage: unsupportedUsageFromService(res.UnsupportedUsage),
	}
}

func unsupportedUsageFromService(
	in *service.UnsupportedUsage,
) *UnsupportedUsage {
	if in == nil {
		return nil
	}
	return &UnsupportedUsage{Kind: in.Kind}
}

func projectTotalsFromService(in []service.ProjectTotal) []ProjectTotal {
	out := make([]ProjectTotal, len(in))
	for i, total := range in {
		out[i] = ProjectTotal{
			Project:             total.Project,
			InputTokens:         total.InputTokens,
			OutputTokens:        total.OutputTokens,
			CacheCreationTokens: total.CacheCreationTokens,
			CacheReadTokens:     total.CacheReadTokens,
			Cost:                total.Cost,
		}
	}
	return out
}

func modelTotalsFromService(in []service.ModelTotal) []ModelTotal {
	out := make([]ModelTotal, len(in))
	for i, total := range in {
		out[i] = ModelTotal{
			Model:               total.Model,
			InputTokens:         total.InputTokens,
			OutputTokens:        total.OutputTokens,
			CacheCreationTokens: total.CacheCreationTokens,
			CacheReadTokens:     total.CacheReadTokens,
			Cost:                total.Cost,
		}
	}
	return out
}

func agentTotalsFromService(in []service.AgentTotal) []AgentTotal {
	out := make([]AgentTotal, len(in))
	for i, total := range in {
		out[i] = AgentTotal{
			Agent:               total.Agent,
			InputTokens:         total.InputTokens,
			OutputTokens:        total.OutputTokens,
			CacheCreationTokens: total.CacheCreationTokens,
			CacheReadTokens:     total.CacheReadTokens,
			Cost:                total.Cost,
		}
	}
	return out
}

func branchTotalsFromService(in []service.BranchTotal) []BranchTotal {
	out := make([]BranchTotal, len(in))
	for i, total := range in {
		out[i] = BranchTotal{
			Project:             total.Project,
			Branch:              total.Branch,
			InputTokens:         total.InputTokens,
			OutputTokens:        total.OutputTokens,
			CacheCreationTokens: total.CacheCreationTokens,
			CacheReadTokens:     total.CacheReadTokens,
			Cost:                total.Cost,
		}
	}
	return out
}

func cacheStatsFromService(in service.CacheStats) CacheStats {
	return CacheStats{
		CacheReadTokens:     in.CacheReadTokens,
		CacheCreationTokens: in.CacheCreationTokens,
		UncachedInputTokens: in.UncachedInputTokens,
		OutputTokens:        in.OutputTokens,
		HitRate:             in.HitRate,
		SavingsVsUncached:   in.SavingsVsUncached,
	}
}
