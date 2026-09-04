package strategy

import "math"

func applyProfitControl(req Request, decision Decision, desired Action, amount float64, ruleID string, tags []string, aggressive bool) (Action, float64, string, []string, bool, ProfitControlMetrics) {
	if !req.ProfitControl.Enabled || req.BehaviorMode == "aggressive_never_fold" {
		return desired, amount, ruleID, tags, aggressive, ProfitControlMetrics{}
	}
	// A calibrated profile-specific large-pot equity floor is more precise than
	// the generic table ledger. Do not stack two guards and accidentally remove
	// profitable value lines before or after its threshold (notably FPCH_90_5).
	if req.LargePotThresholdBB > 0 && req.LargePotMinEquity > 0 {
		return desired, amount, ruleID, tags, aggressive, ProfitControlMetrics{}
	}
	metrics := profitControlMetrics(req, amount, desired)
	if metrics.TotalMargin <= 0 {
		return desired, amount, ruleID, tags, aggressive, metrics
	}
	equity := decision.Equity.Equity
	callFloor := decision.PotOdds + metrics.TotalMargin
	switch desired {
	case Call:
		if equity+1e-9 < callFloor {
			metrics.Applied = true
			return Fold, 0, "PROFIT_CONTROL_CALL_FOLD", append(tags, "profit_control", "large_call_guard"), false, metrics
		}
	case Raise, AllIn:
		if req.ToCall > 0 && equity+1e-9 < math.Max(.50, decision.PotOdds+.18)+metrics.TotalMargin {
			metrics.Applied = true
			if equity+1e-9 >= callFloor {
				return Call, req.ToCall, "PROFIT_CONTROL_AGGRESSION_CALL", append(tags, "profit_control", "aggression_downgrade"), false, metrics
			}
			return Fold, 0, "PROFIT_CONTROL_AGGRESSION_FOLD", append(tags, "profit_control", "aggression_guard"), false, metrics
		}
		if req.ToCall == 0 && equity+1e-9 < .50+metrics.TotalMargin {
			metrics.Applied = true
			return Check, 0, "PROFIT_CONTROL_AGGRESSION_CHECK", append(tags, "profit_control", "pot_control"), false, metrics
		}
	case Bet:
		if equity+1e-9 < .50+metrics.TotalMargin {
			metrics.Applied = true
			return Check, 0, "PROFIT_CONTROL_AGGRESSION_CHECK", append(tags, "profit_control", "pot_control"), false, metrics
		}
	}
	return desired, amount, ruleID, tags, aggressive, metrics
}

func profitControlMetrics(req Request, amount float64, action Action) ProfitControlMetrics {
	cfg, table := req.ProfitControl, req.TablePerformance
	metrics := ProfitControlMetrics{}
	actionAmount := amount
	if action == Call {
		actionAmount = req.ToCall
	}
	if req.BigBlind > 0 {
		metrics.ActionBB = actionAmount / req.BigBlind
		metrics.ProfitBB = table.NetProfit / req.BigBlind
	}
	if req.Stack > 0 {
		metrics.ActionStackRatio = actionAmount / req.Stack
	}
	if table.InitialStack > 0 {
		metrics.ProfitStackRatio = table.NetProfit / table.InitialStack
	}
	if table.Actions > 0 {
		metrics.CallRate = float64(table.Calls) / float64(table.Actions)
		metrics.AggressionRate = float64(table.Aggressive) / float64(table.Actions)
	}

	bbPressure := positiveProgress(metrics.ActionBB, cfg.LargeActionBB)
	stackPressure := positiveProgress(metrics.ActionStackRatio, cfg.LargeActionStackRatio)
	metrics.ExposureMargin = cfg.MaxExposureMargin * math.Max(bbPressure, stackPressure)
	if metrics.ExposureMargin == 0 {
		return metrics
	}

	profitPressure := math.Max(positiveProgress(metrics.ProfitBB, cfg.ProfitTriggerBB), positiveProgress(metrics.ProfitStackRatio, cfg.ProfitTriggerStackRatio)) * .5
	lossPressure := math.Max(positiveProgress(-metrics.ProfitBB, cfg.LossTriggerBB), positiveProgress(-metrics.ProfitStackRatio, cfg.LossTriggerStackRatio))
	metrics.PerformanceMargin = cfg.MaxPerformanceMargin * math.Max(profitPressure, lossPressure)

	if table.Actions >= uint64(cfg.MinimumActions) {
		mixPressure := positiveProgress(metrics.CallRate, cfg.CallRateTarget)
		if action == Bet || action == Raise || action == AllIn {
			mixPressure = math.Max(mixPressure, positiveProgress(metrics.AggressionRate, cfg.AggressionRateTarget))
		}
		metrics.ActionMixMargin = cfg.MaxActionMixMargin * mixPressure
	}
	metrics.TotalMargin = math.Min(cfg.MaxTotalMargin, metrics.ExposureMargin+metrics.PerformanceMargin+metrics.ActionMixMargin)
	return metrics
}

// positiveProgress is zero until the configured threshold, then reaches one
// when the observed value is twice that threshold.
func positiveProgress(value, threshold float64) float64 {
	if threshold <= 0 || value <= threshold {
		return 0
	}
	return math.Min(1, (value-threshold)/threshold)
}
