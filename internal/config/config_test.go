package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineConfigurationIsValid(t *testing.T) {
	cfg := Default()
	cfg.Auth.Token = "test"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "engine" || !cfg.Engine.Personality.HumanizationEnabled || !cfg.Log.Access {
		t.Fatalf("default config=%+v", cfg)
	}
	if !cfg.Engine.Strategy.CollapseNearAllIn || cfg.Engine.Strategy.NearAllInRemainingChips != .01 {
		t.Fatalf("near-all-in defaults=%+v", cfg.Engine.Strategy)
	}
	if cfg.Engine.Strategy.PreflopReraiseRangeFactor != .4 {
		t.Fatalf("preflop reraise range factor=%v", cfg.Engine.Strategy.PreflopReraiseRangeFactor)
	}
}

func TestNearAllInConfigurationCanBeDisabled(t *testing.T) {
	cfg := Default()
	cfg.Auth.Token = "test"
	cfg.Engine.Strategy.CollapseNearAllIn = false
	cfg.Engine.Strategy.NearAllInRemainingChips = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled near-all-in collapse: %v", err)
	}
	cfg.Engine.Strategy.CollapseNearAllIn = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled near-all-in collapse must require a positive threshold")
	}
}

func TestLoadRejectsUnknownConfigurationField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("auth:\n  token: test\nunknown_option: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}

func TestApplyThinkTimeRemainsOptionalAndBackwardCompatible(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy.yaml")
	if err := os.WriteFile(legacyPath, []byte("auth:\n  token: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.Engine.Personality.ApplyThinkTime {
		t.Fatal("omitting apply_think_time must preserve the legacy enabled default")
	}

	externalDelayPath := filepath.Join(t.TempDir(), "external-delay.yaml")
	content := "auth:\n  token: test\nengine:\n  personality:\n    apply_think_time: false\n"
	if err := os.WriteFile(externalDelayPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	externalDelay, err := Load(externalDelayPath)
	if err != nil {
		t.Fatal(err)
	}
	if externalDelay.Engine.Personality.ApplyThinkTime {
		t.Fatal("explicit apply_think_time=false was not applied")
	}
}

func TestAirCallGuardConfigurationIsOptional(t *testing.T) {
	cfg := Default()
	if cfg.Engine.Strategy.FlopAirCallMargin <= 0 || cfg.Engine.Strategy.TurnAirCallMargin <= cfg.Engine.Strategy.FlopAirCallMargin || cfg.Engine.Strategy.RiverAirCallMargin <= cfg.Engine.Strategy.TurnAirCallMargin || cfg.Engine.Strategy.TurnWeakDrawCallMargin <= 0 || cfg.Engine.Strategy.RiverBoardPairCallMargin <= 0 || cfg.Engine.Strategy.RiverMissedDrawMargin <= 0 || !cfg.Engine.Strategy.RejectNegativeEVCalls {
		t.Fatalf("air-call defaults=%+v", cfg.Engine.Strategy)
	}
	cfg.Engine.Strategy.FlopAirCallMargin = 0
	cfg.Engine.Strategy.TurnAirCallMargin = 0
	cfg.Engine.Strategy.RiverAirCallMargin = 0
	cfg.Engine.Strategy.RepeatedAirCallPenalty = 0
	cfg.Engine.Strategy.TurnWeakDrawCallMargin = 0
	cfg.Engine.Strategy.RiverBoardPairCallMargin = 0
	cfg.Engine.Strategy.RiverMissedDrawMargin = 0
	cfg.Engine.Strategy.RejectNegativeEVCalls = false
	cfg.Auth.Token = "test"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled optional air-call guard: %v", err)
	}
}

func TestAdminConfigurationIsOptionalAndValidated(t *testing.T) {
	cfg := Default()
	cfg.Auth.Token = "test"
	if cfg.Admin.Enabled || cfg.Admin.Path != "/admin" {
		t.Fatalf("admin defaults=%+v", cfg.Admin)
	}
	cfg.Admin.Enabled = true
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Admin.Path = "/"
	if err := cfg.Validate(); err == nil {
		t.Fatal("root admin path must be rejected")
	}
}

func TestGrayCandidateInheritsAndOverridesEngine(t *testing.T) {
	cfg := Default()
	cfg.Auth.Token = "test"
	cfg.Phase5.Gray.Enabled = true
	level, margin := 4, .05
	cfg.Phase5.Gray.Candidate.DefaultLevel = &level
	cfg.Phase5.Gray.Candidate.CallMargin = &margin
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	candidate := cfg.GrayCandidateEngine()
	if candidate.DefaultLevel != 4 || candidate.Strategy.PreflopCallMargin != .05 || candidate.Strategy.PreflopReraiseEquity != cfg.Engine.Strategy.PreflopReraiseEquity {
		t.Fatalf("candidate=%+v", candidate)
	}
	cfg.Phase5.Gray.Percentage = 101
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid percentage accepted")
	}
}

func TestBotProfilesValidatePersonalityAndLevel(t *testing.T) {
	cfg := Default()
	cfg.Auth.Token = "test"
	cfg.Engine.Personality.Profiles["AICON_TAG_L4"] = BotProfileConfig{Personality: "tag", Level: 4}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Engine.Personality.Profiles["BROKEN"] = BotProfileConfig{Personality: "tag", Level: 6}
	if err := cfg.Validate(); err == nil {
		t.Fatal("level 6 profile accepted")
	}
	delete(cfg.Engine.Personality.Profiles, "BROKEN")
	cfg.Engine.Personality.Profiles["BROKEN_RATE"] = BotProfileConfig{Personality: "tag", Level: 3, TargetVPIP: .30, TargetPFR: .31}
	if err := cfg.Validate(); err == nil {
		t.Fatal("PFR greater than VPIP accepted")
	}
}

func TestShippedAiConProfiles(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "conf", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]BotProfileConfig{
		"FPCH_100_50":    {Personality: "lag", Level: 5, TargetVPIP: 1, TargetPFR: .5},
		"FPCH_default":   {Personality: "balanced", Level: 5, TargetVPIP: .32, TargetPFR: .16},
		"FPCH_30_15":     {Personality: "tag", Level: 4, TargetVPIP: .30, TargetPFR: .15},
		"FPCH_39_14":     {Personality: "balanced", Level: 3, TargetVPIP: .39, TargetPFR: .14},
		"FPCH_54_11":     {Personality: "calling_station", Level: 2, TargetVPIP: .54, TargetPFR: .11},
		"FPCH_60_5":      {Personality: "calling_station", Level: 1, TargetVPIP: .60, TargetPFR: .05},
		"FPCH_60_10":     {Personality: "calling_station", Level: 2, TargetVPIP: .60, TargetPFR: .10},
		"FPCH_90_5":      {Personality: "calling_station", Level: 1, TargetVPIP: .90, TargetPFR: .05},
		"FPCH_defaul_S1": {Personality: "balanced", Level: 5, TargetVPIP: .32, TargetPFR: .16},
		"FPCH_defaul_S2": {Personality: "balanced", Level: 5, TargetVPIP: .32, TargetPFR: .16},
	}
	special := cfg.Engine.Personality.Profiles["FPCH_100_50"]
	if special.BehaviorMode != "aggressive_never_fold" || special.PreflopRaiseProbability != .5 || special.PostflopAggressionChance != .75 || !special.NeverFold || !special.AuditExempt {
		t.Fatalf("FPCH_100_50 special controls=%+v", special)
	}
	for id, want := range expected {
		got, ok := cfg.Engine.Personality.Profiles[id]
		if !ok || got.Personality != want.Personality || got.Level != want.Level || got.TargetVPIP != want.TargetVPIP || got.TargetPFR != want.TargetPFR || got.Description == "" {
			t.Errorf("profile %s got=%+v present=%v", id, got, ok)
		}
	}
	guarded := cfg.Engine.Personality.Profiles["FPCH_90_5"]
	if guarded.LargePotThreshold <= 0 || guarded.LargePotMinEquity <= 0 {
		t.Fatalf("FPCH_90_5 risk controls=%+v", guarded)
	}
	for _, base := range []string{"FPCH_30_15", "FPCH_39_14", "FPCH_54_11", "FPCH_60_5", "FPCH_60_10"} {
		for _, suffix := range []string{"_S1", "_S2"} {
			variant, ok := cfg.Engine.Personality.Profiles[base+suffix]
			if !ok || variant.Personality != cfg.Engine.Personality.Profiles[base].Personality || variant.Level != cfg.Engine.Personality.Profiles[base].Level || variant.TargetVPIP != cfg.Engine.Personality.Profiles[base].TargetVPIP || variant.TargetPFR != cfg.Engine.Personality.Profiles[base].TargetPFR {
				t.Errorf("profile variant %s got=%+v", base+suffix, variant)
			}
		}
	}
	wantSizings := map[string][]float64{
		"FPCH_defaul_S2": {0.50, 0.66, 0.75, 1.00},
		"FPCH_30_15_S1":  {0.50, 0.66, 1.00}, "FPCH_30_15_S2": {0.33, 0.50, 0.66},
		"FPCH_39_14_S1": {0.33, 0.50, 0.66}, "FPCH_39_14_S2": {0.33, 0.50, 0.75},
		"FPCH_54_11_S1": {0.50, 0.66}, "FPCH_54_11_S2": {0.33, 0.66},
		"FPCH_60_5_S1": {0.50, 0.66}, "FPCH_60_5_S2": {0.33, 0.75},
		"FPCH_60_10_S1": {0.50, 0.66}, "FPCH_60_10_S2": {0.50, 0.75},
	}
	for profile, want := range wantSizings {
		if got := cfg.Engine.Personality.Profiles[profile].PostflopSizings; !reflect.DeepEqual(got, want) {
			t.Errorf("profile %s postflop_sizings=%v want=%v", profile, got, want)
		}
	}
	if got := cfg.Engine.Personality.Profiles["FPCH_defaul_S1"].PostflopSizings; len(got) != 0 {
		t.Errorf("FPCH_defaul_S1 source sizing is blank, got=%v", got)
	}
}

func TestProfilePostflopSizingsMustBeSortedAndUnique(t *testing.T) {
	cfg := Default()
	cfg.Auth.Token = "test"
	cfg.Engine.Personality.Profiles["BROKEN"] = BotProfileConfig{
		Personality: "balanced", Level: 3, PostflopSizings: []float64{.66, .50},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("descending postflop_sizings accepted")
	}
}
