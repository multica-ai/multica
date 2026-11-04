package metrics

// EstimateCostUSD prices one usage row's tokens with the rate table that
// RecordLLMUsage uses (PriceForRuntimeModel). It is the server's one estimate of
// what a row costs when the provider reported no price of its own; budgets
// (spec 4.2) read it so their figures match the cost metrics. It returns false
// when the model has no rate row, so the caller can tell "free" from "unknown".
func EstimateCostUSD(rawProvider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64) (float64, bool) {
	price, ok := PriceForRuntimeModel(rawProvider, model)
	if !ok {
		return 0, false
	}
	return tokenCostUSD(inputTokens, price.InputPerM) +
		tokenCostUSD(outputTokens, price.OutputPerM) +
		tokenCostUSD(cacheReadTokens, price.CacheReadPerM) +
		tokenCostUSD(cacheWriteTokens, price.CacheWritePerM), true
}
