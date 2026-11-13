package strategy

import (
	"testing"

	"gitlab.com/smoothsics/ainp/internal/equity"
)

func standardProfitControl() ProfitControl {
	return ProfitControl{
		Enabled: true, MinimumActions: 20,
		LargeActionBB: 8, LargeActionStackRatio: .20,
		ProfitTriggerBB: 20, ProfitTriggerStackRatio: .20,
		LossTriggerBB: 20, LossTriggerStackRatio: .20,
		CallRateTarget: .55, AggressionRateTarget: .35,
		MaxExposureMargin: .10, MaxPerformanceMargin: .08,
		MaxActionMixMargin: .05, MaxTotalMargin: .18,
	}
}

func TestProfitControlFoldsLargeMarginalCallWhenLosingAndOverCalling(t *testing.T) {
	req := Request{
		BigBlind: 1, Stack: 100, ToCall: 40, ProfitControl: standardProfitControl(),
		TablePerformance: TablePerformance{InitialStack: 100, NetProfit: -40, Hands: 30, Actions: 100, Calls: 75, Aggressive: 10},
	}
	decision := Decision{Equity: equity.Result{Equity: .34}, PotOdds: .25}
	action, amount, rule, _, aggressive, metrics := applyProfitControl(req, decision, Call, 40, "POSTFLOP_CALL", nil, false)
	if action != Fold || amount != 0 || rule != "PROFIT_CONTROL_CALL_FOLD" || aggressive || !metrics.Applied {
		t.Fatalf("action=%s amount=%v rule=%s aggressive=%v metrics=%+v", action, amount, rule, aggressive, metrics)
	}
	if metrics.ExposureMargin <= 0 || metrics.PerformanceMargin <= 0 || metrics.ActionMixMargin <= 0 || metrics.TotalMargin > req.ProfitControl.MaxTotalMargin {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestProfitControlLeavesSmallCallsAlone(t *testing.T) {
	req := Request{
		BigBlind: 1, Stack: 100, ToCall: 2, ProfitControl: standardProfitControl(),
		TablePerformance: TablePerformance{InitialStack: 100, NetProfit: -80, Actions: 100, Calls: 90},
	}
	decision := Decision{Equity: equity.Result{Equity: .12}, PotOdds: .10}
	action, amount, rule, _, _, metrics := applyProfitControl(req, decision, Call, 2, "POSTFLOP_CALL", nil, false)
	if action != Call || amount != 2 || rule != "POSTFLOP_CALL" || metrics.TotalMargin != 0 || metrics.Applied {
		t.Fatalf("action=%s amount=%v rule=%s metrics=%+v", action, amount, rule, metrics)
	}
}

func TestProfitControlDowngradesMarginalRaiseAndPreservesStrongValue(t *testing.T) {
	req := Request{
		BigBlind: 1, Stack: 100, ToCall: 20, ProfitControl: standardProfitControl(),
		TablePerformance: TablePerformance{InitialStack: 100, NetProfit: 40, Actions: 100, Calls: 20, Aggressive: 60},
	}
	weak := Decision{Equity: equity.Result{Equity: .53}, PotOdds: .20}
	action, amount, rule, _, aggressive, metrics := applyProfitControl(req, weak, Raise, 50, "POSTFLOP_VALUE_RAISE", nil, true)
	if action != Call || amount != 20 || rule != "PROFIT_CONTROL_AGGRESSION_CALL" || aggressive || !metrics.Applied {
		t.Fatalf("weak action=%s amount=%v rule=%s aggressive=%v metrics=%+v", action, amount, rule, aggressive, metrics)
	}
	strong := Decision{Equity: equity.Result{Equity: .90}, PotOdds: .20}
	action, amount, rule, _, aggressive, metrics = applyProfitControl(req, strong, Raise, 50, "POSTFLOP_VALUE_RAISE", nil, true)
	if action != Raise || amount != 50 || rule != "POSTFLOP_VALUE_RAISE" || !aggressive || metrics.Applied {
		t.Fatalf("strong action=%s amount=%v rule=%s aggressive=%v metrics=%+v", action, amount, rule, aggressive, metrics)
	}
}

func TestNeverFoldProfileBypassesProfitControl(t *testing.T) {
	req := Request{
		BehaviorMode: "aggressive_never_fold", BigBlind: 1, Stack: 100, ToCall: 100,
		ProfitControl: standardProfitControl(), TablePerformance: TablePerformance{InitialStack: 100, NetProfit: -100},
	}
	action, amount, rule, _, _, metrics := applyProfitControl(req, Decision{Equity: equity.Result{Equity: .01}}, Call, 100, "SPECIAL", nil, false)
	if action != Call || amount != 100 || rule != "SPECIAL" || metrics != (ProfitControlMetrics{}) {
		t.Fatalf("action=%s amount=%v rule=%s metrics=%+v", action, amount, rule, metrics)
	}
}

func TestProfileLargePotGuardIsNotStackedWithProfitControl(t *testing.T) {
	req := Request{
		BigBlind: 1, Pot: 10, Stack: 100, ToCall: 20, LargePotThresholdBB: 20, LargePotMinEquity: .58,
		ProfitControl: standardProfitControl(), TablePerformance: TablePerformance{InitialStack: 100, NetProfit: -100},
	}
	action, amount, rule, _, _, metrics := applyProfitControl(req, Decision{Equity: equity.Result{Equity: .60}, PotOdds: .20}, Raise, 50, "POSTFLOP_VALUE_RAISE", nil, true)
	if action != Raise || amount != 50 || rule != "POSTFLOP_VALUE_RAISE" || metrics != (ProfitControlMetrics{}) {
		t.Fatalf("action=%s amount=%v rule=%s metrics=%+v", action, amount, rule, metrics)
	}
}
