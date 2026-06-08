package metrics

import "github.com/arnab-guin/vllm-gateway/internal/cost"

// CostGaugeRefresher updates rolling-window token and cost gauges at Prometheus scrape time.
type CostGaugeRefresher struct {
	lastSeenTeams map[string]struct{}
}

func NewCostGaugeRefresher() *CostGaugeRefresher {
	return &CostGaugeRefresher{}
}

func (r *CostGaugeRefresher) Refresh(calc *cost.Calculator, tp *cost.TokenThroughput) {
	if calc == nil || tp == nil {
		return
	}

	usdPerToken, okToken := calc.USDPerToken()
	if usdPerMillion, ok := calc.USDPerMillionTokens(); ok {
		GatewayClusterUSDPerMillionTokens.Set(usdPerMillion)
	} else {
		GatewayClusterUSDPerMillionTokens.Set(0)
	}

	seen := make(map[string]struct{})
	tp.ForEachRequestTeam(func(teamID string) {
		seen[teamID] = struct{}{}

		prompt := float64(tp.TotalTokens(cost.TokenPrompt, teamID))
		completion := float64(tp.TotalTokens(cost.TokenCompletion, teamID))
		total := prompt + completion

		GatewayTeamPromptTokens10m.WithLabelValues(teamID).Set(prompt)
		GatewayTeamCompletionTokens10m.WithLabelValues(teamID).Set(completion)
		GatewayTeamTokens10m.WithLabelValues(teamID).Set(total)

		if okToken {
			GatewayTeamEstimatedCostUSD10m.WithLabelValues(teamID).Set(total * usdPerToken)
			if teamRate, ok := calc.TeamTokensPerHour(teamID); ok {
				GatewayTeamEstimatedCostUSDPerHour.WithLabelValues(teamID).Set(teamRate * usdPerToken)
			} else {
				GatewayTeamEstimatedCostUSDPerHour.WithLabelValues(teamID).Set(0)
			}
		} else {
			GatewayTeamEstimatedCostUSD10m.WithLabelValues(teamID).Set(0)
			GatewayTeamEstimatedCostUSDPerHour.WithLabelValues(teamID).Set(0)
		}
	})

	for teamID := range r.lastSeenTeams {
		if _, active := seen[teamID]; active {
			continue
		}
		GatewayTeamPromptTokens10m.DeleteLabelValues(teamID)
		GatewayTeamCompletionTokens10m.DeleteLabelValues(teamID)
		GatewayTeamTokens10m.DeleteLabelValues(teamID)
		GatewayTeamEstimatedCostUSD10m.DeleteLabelValues(teamID)
		GatewayTeamEstimatedCostUSDPerHour.DeleteLabelValues(teamID)
	}
	r.lastSeenTeams = seen
}
