package cost

type Calculator struct {
	gpuUSDPerHour float64
	tokenThroughput *TokenThroughput
}

func NewCalculator(gpuUSDPerHour float64, tokenThroughput *TokenThroughput) *Calculator {
	if tokenThroughput == nil {
		panic("tokenThroughput is nil")
	}
	return &Calculator{
		gpuUSDPerHour: gpuUSDPerHour,
		tokenThroughput: tokenThroughput,
	}
}

func (c *Calculator) ClusterTokensPerHour() (float64, bool) {
	rate := c.tokenThroughput.ClusterRequestTokensPerHour()
	return rate, rate > 0
}

func (c *Calculator) ClusterCostPerHour() (float64, bool) {
	if c.gpuUSDPerHour <= 0 {
		return 0, false
	}
	if _, ok := c.ClusterTokensPerHour(); !ok {
		return 0, false
	}
	return c.gpuUSDPerHour, true
}

func (c *Calculator) USDPerToken() (float64, bool) {
	tokensPerHour, ok := c.ClusterTokensPerHour()
	if !ok {
		return 0, false
	}
	return c.gpuUSDPerHour / tokensPerHour, true
}

func (c *Calculator) USDPerMillionTokens() (float64, bool) {
	usdPerToken, ok := c.USDPerToken()
	if !ok {
		return 0, false
	}
	return usdPerToken * 1_000_000, true
}

func (c *Calculator) TeamTokensPerHour(teamID string) (float64, bool) {
	rate := c.tokenThroughput.TotalRequestTokensPerHour(teamID)
	return rate, rate > 0
}

func (c *Calculator) TeamTokenShare(teamID string) (float64, bool) {
	rate, ok := c.TeamTokensPerHour(teamID)
	if !ok {
		return 0, false
	}
	clusterRate, ok := c.ClusterTokensPerHour()
	if !ok {
		return 0, false
	}
	return rate / clusterRate, true
}

func (c *Calculator) RequestCost(promptTokens, completionTokens uint32) (float64, bool) {
	billable := uint64(promptTokens) + uint64(completionTokens)
	if billable == 0 {
		return 0, false
	}
	usdPerToken, ok := c.USDPerToken()
	if !ok {
		return 0, false
	}
	return float64(billable) * usdPerToken, true
}