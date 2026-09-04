package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode   string       `yaml:"mode"`
	Server ServerConfig `yaml:"server"`
	Auth   AuthConfig   `yaml:"auth"`
	Admin  AdminConfig  `yaml:"admin"`
	State  StateConfig  `yaml:"state"`
	Mock   MockConfig   `yaml:"mock"`
	Engine EngineConfig `yaml:"engine"`
	Phase5 Phase5Config `yaml:"phase5"`
	Log    LogConfig    `yaml:"log"`
}

type ServerConfig struct {
	Host              string        `yaml:"host"`
	Port              int           `yaml:"port"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout"`
}

type AuthConfig struct {
	Token string `yaml:"token"`
}

type AdminConfig struct {
	Enabled          bool          `yaml:"enabled"`
	Path             string        `yaml:"path"`
	LogPath          string        `yaml:"log_path"`
	ExpectationsPath string        `yaml:"expectations_path"`
	ReportPath       string        `yaml:"report_path"`
	RefreshInterval  time.Duration `yaml:"refresh_interval"`
}
type StateConfig struct {
	TTL           time.Duration `yaml:"ttl"`
	MaxHands      int           `yaml:"max_hands"`
	PruneInterval time.Duration `yaml:"prune_interval"`
}
type MockConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Action   string   `yaml:"action"`
	Value    float64  `yaml:"value"`
	AdviseOn []string `yaml:"advise_on"`
}
type LogConfig struct {
	Level    string `yaml:"level"`
	Access   bool   `yaml:"access"`
	Events   bool   `yaml:"events"`
	Strategy bool   `yaml:"strategy"`
}

type EngineConfig struct {
	Enabled         bool                `yaml:"enabled"`
	PolicyVersion   string              `yaml:"policy_version"`
	DecisionTimeout time.Duration       `yaml:"decision_timeout"`
	MaxConcurrent   int                 `yaml:"max_concurrent"`
	AdviseOn        []string            `yaml:"advise_on"`
	FallbackToMock  bool                `yaml:"fallback_to_mock"`
	DefaultLevel    int                 `yaml:"default_level"`
	GameAliases     map[string]string   `yaml:"game_aliases"`
	Equity          EquityConfig        `yaml:"equity"`
	Strategy        StrategyConfig      `yaml:"strategy"`
	Personality     PersonalityConfig   `yaml:"personality"`
	OpponentModel   OpponentConfig      `yaml:"opponent_model"`
	ProfitControl   ProfitControlConfig `yaml:"profit_control"`
}

type Phase5Config struct {
	Replay ReplayConfig `yaml:"replay"`
	Gray   GrayConfig   `yaml:"gray"`
}

type ReplayConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Directory      string `yaml:"directory"`
	FilePrefix     string `yaml:"file_prefix"`
	FlushEachWrite bool   `yaml:"flush_each_write"`
}

type GrayConfig struct {
	Enabled    bool                `yaml:"enabled"`
	Mode       string              `yaml:"mode"`
	Percentage int                 `yaml:"percentage"`
	Salt       string              `yaml:"salt"`
	Candidate  GrayCandidateConfig `yaml:"candidate"`
}

type GrayCandidateConfig struct {
	PolicyVersion     string   `yaml:"policy_version"`
	DefaultLevel      *int     `yaml:"default_level"`
	OpenCallGap       *float64 `yaml:"preflop_open_call_gap"`
	ReraiseEquity     *float64 `yaml:"preflop_reraise_equity"`
	ExtraRaisePenalty *float64 `yaml:"preflop_extra_raise_penalty"`
	MultiwayPenalty   *float64 `yaml:"preflop_multiway_penalty"`
	CallMargin        *float64 `yaml:"preflop_call_margin"`
}

type EquityConfig struct {
	Enabled              bool   `yaml:"enabled"`
	DefaultSamples       int    `yaml:"default_samples"`
	PLO4Samples          int    `yaml:"plo4_samples"`
	PLO5Samples          int    `yaml:"plo5_samples"`
	PLO6Samples          int    `yaml:"plo6_samples"`
	MaxExactOutcomes     uint64 `yaml:"max_exact_outcomes"`
	CacheEnabled         bool   `yaml:"cache_enabled"`
	CacheCapacity        int    `yaml:"cache_capacity"`
	PreflopLookupEnabled bool   `yaml:"preflop_lookup_enabled"`
	AutoExactEnabled     bool   `yaml:"auto_exact_enabled"`
}

type StrategyConfig struct {
	Enabled                   bool    `yaml:"enabled"`
	InferLegalActions         bool    `yaml:"infer_legal_actions"`
	MinRaiseBigBlinds         float64 `yaml:"min_raise_big_blinds"`
	CollapseNearAllIn         bool    `yaml:"collapse_near_allin"`
	NearAllInRemainingChips   float64 `yaml:"near_allin_remaining_chips"`
	PreflopOpenCallGap        float64 `yaml:"preflop_open_call_gap"`
	PreflopReraiseEquity      float64 `yaml:"preflop_reraise_equity"`
	PreflopReraiseRangeFactor float64 `yaml:"preflop_reraise_range_factor"`
	PreflopExtraRaisePenalty  float64 `yaml:"preflop_extra_raise_penalty"`
	PreflopMultiwayPenalty    float64 `yaml:"preflop_multiway_penalty"`
	PreflopCallMargin         float64 `yaml:"preflop_call_margin"`
	PreflopLargeCallBB        float64 `yaml:"preflop_large_call_threshold_bb"`
	FlopAirCallMargin         float64 `yaml:"flop_air_call_margin"`
	TurnAirCallMargin         float64 `yaml:"turn_air_call_margin"`
	RiverAirCallMargin        float64 `yaml:"river_air_call_margin"`
	RepeatedAirCallPenalty    float64 `yaml:"repeated_air_call_penalty"`
	UnderpairCallMargin       float64 `yaml:"underpair_call_margin"`
	TurnWeakDrawCallMargin    float64 `yaml:"turn_weak_draw_call_margin"`
	RiverBoardPairCallMargin  float64 `yaml:"river_board_pair_call_margin"`
	RiverMissedDrawMargin     float64 `yaml:"river_missed_draw_call_margin"`
	RejectNegativeEVCalls     bool    `yaml:"reject_negative_ev_calls"`
}

// ProfitControlConfig bounds large voluntary investments using the bot's
// realized result and action mix at the same table. It is deliberately global
// rather than profile-specific so every normal profile gets the same bankroll
// protection; aggressive_never_fold profiles bypass it.
type ProfitControlConfig struct {
	Enabled                 bool          `yaml:"enabled"`
	TTL                     time.Duration `yaml:"ttl"`
	MaxPlayers              int           `yaml:"max_players"`
	MinimumActions          int           `yaml:"minimum_actions"`
	LargeActionBB           float64       `yaml:"large_action_bb"`
	LargeActionStackRatio   float64       `yaml:"large_action_stack_ratio"`
	ProfitTriggerBB         float64       `yaml:"profit_trigger_bb"`
	ProfitTriggerStackRatio float64       `yaml:"profit_trigger_stack_ratio"`
	LossTriggerBB           float64       `yaml:"loss_trigger_bb"`
	LossTriggerStackRatio   float64       `yaml:"loss_trigger_stack_ratio"`
	CallRateTarget          float64       `yaml:"call_rate_target"`
	AggressionRateTarget    float64       `yaml:"aggression_rate_target"`
	MaxExposureMargin       float64       `yaml:"max_exposure_margin"`
	MaxPerformanceMargin    float64       `yaml:"max_performance_margin"`
	MaxActionMixMargin      float64       `yaml:"max_action_mix_margin"`
	MaxTotalMargin          float64       `yaml:"max_total_margin"`
}

type PersonalityConfig struct {
	Enabled             bool                        `yaml:"enabled"`
	HumanizationEnabled bool                        `yaml:"humanization_enabled"`
	ThinkTimeEnabled    bool                        `yaml:"think_time_enabled"`
	ApplyThinkTime      bool                        `yaml:"apply_think_time"`
	UseAIProfile        bool                        `yaml:"use_ai_profile"`
	Default             string                      `yaml:"default"`
	ProfileMap          map[string]string           `yaml:"profile_map"`
	Profiles            map[string]BotProfileConfig `yaml:"profiles"`
}

type BotProfileConfig struct {
	Personality              string    `yaml:"personality"`
	Level                    int       `yaml:"level"`
	TargetVPIP               float64   `yaml:"target_vpip"`
	TargetPFR                float64   `yaml:"target_pfr"`
	PostflopSizings          []float64 `yaml:"postflop_sizings"`
	PostflopCallMargin       float64   `yaml:"postflop_call_margin"`
	LargePotThreshold        float64   `yaml:"large_pot_threshold_bb"`
	LargePotMinEquity        float64   `yaml:"large_pot_min_equity"`
	BehaviorMode             string    `yaml:"behavior_mode"`
	PreflopRaiseProbability  float64   `yaml:"preflop_raise_probability"`
	PostflopAggressionChance float64   `yaml:"postflop_aggression_probability"`
	NeverFold                bool      `yaml:"never_fold"`
	AuditExempt              bool      `yaml:"audit_exempt"`
	Description              string    `yaml:"description"`
}

type OpponentConfig struct {
	Enabled      bool `yaml:"enabled"`
	MaxPlayers   int  `yaml:"max_players"`
	DedupeWindow int  `yaml:"dedupe_window"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := defaultConfig()
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	applyEnvironment(&cfg)
	normalize(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultConfig() Config {
	return Default()
}

func Default() Config {
	return Config{
		Mode:   "engine",
		Server: ServerConfig{Host: "0.0.0.0", Port: 8090, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second},
		Admin:  AdminConfig{Path: "/admin", LogPath: "build/nohup.out", ExpectationsPath: "conf/audit.yaml", ReportPath: "reports/audit-runtime.json"},
		State:  StateConfig{TTL: 24 * time.Hour, MaxHands: 50_000, PruneInterval: time.Minute},
		Mock:   MockConfig{Enabled: true, Action: "check", AdviseOn: []string{"deal_cards", "action", "flop", "turn", "river"}},
		Engine: EngineConfig{
			Enabled: true, PolicyVersion: "phase5-postflop-discipline-v2", DecisionTimeout: 150 * time.Millisecond, MaxConcurrent: 3, FallbackToMock: false, DefaultLevel: 3,
			AdviseOn:    []string{"deal_cards", "action", "flop", "turn", "river"},
			GameAliases: map[string]string{"NLH": "NLH", "NLHB": "NLH", "NLP": "NLH", "PLO": "PLO4", "PLO4": "PLO4", "PLO5": "PLO5", "PLO6": "PLO6", "SHORT_DECK": "SHORT_DECK", "SHORT_DECK_FIXED": "SHORT_DECK_FIXED"},
			Equity:      EquityConfig{Enabled: true, DefaultSamples: 5_000, PLO4Samples: 3_000, PLO5Samples: 2_000, PLO6Samples: 1_500, MaxExactOutcomes: 5_000, CacheEnabled: true, CacheCapacity: 4_096, PreflopLookupEnabled: true, AutoExactEnabled: true},
			Strategy: StrategyConfig{
				Enabled: true, InferLegalActions: true, MinRaiseBigBlinds: 1,
				CollapseNearAllIn: true, NearAllInRemainingChips: .01,
				PreflopOpenCallGap: .06, PreflopReraiseEquity: .76, PreflopReraiseRangeFactor: .4, PreflopExtraRaisePenalty: .025,
				PreflopMultiwayPenalty: .005, PreflopCallMargin: .035, PreflopLargeCallBB: 10,
				FlopAirCallMargin: .03, TurnAirCallMargin: .08, RiverAirCallMargin: .15,
				RepeatedAirCallPenalty: .08, UnderpairCallMargin: .25, TurnWeakDrawCallMargin: .18,
				RiverBoardPairCallMargin: .15, RiverMissedDrawMargin: .08, RejectNegativeEVCalls: true,
			},
			Personality: PersonalityConfig{
				Enabled: true, HumanizationEnabled: true, ThinkTimeEnabled: true, ApplyThinkTime: true, UseAIProfile: true, Default: "balanced",
				ProfileMap: map[string]string{}, Profiles: map[string]BotProfileConfig{},
			},
			OpponentModel: OpponentConfig{Enabled: true, MaxPlayers: 10_000, DedupeWindow: 256},
			ProfitControl: ProfitControlConfig{
				Enabled: true, TTL: 12 * time.Hour, MaxPlayers: 10_000, MinimumActions: 20,
				LargeActionBB: 8, LargeActionStackRatio: .20,
				ProfitTriggerBB: 20, ProfitTriggerStackRatio: .20,
				LossTriggerBB: 20, LossTriggerStackRatio: .20,
				CallRateTarget: .55, AggressionRateTarget: .35,
				MaxExposureMargin: .10, MaxPerformanceMargin: .08,
				MaxActionMixMargin: .05, MaxTotalMargin: .18,
			},
		},
		Phase5: Phase5Config{
			Replay: ReplayConfig{Enabled: false, Directory: "data/replay", FilePrefix: "events", FlushEachWrite: true},
			Gray:   GrayConfig{Enabled: false, Mode: "shadow", Percentage: 10, Salt: "ainp-gray", Candidate: GrayCandidateConfig{PolicyVersion: "candidate-v1"}},
		},
		Log: LogConfig{Level: "info", Access: true, Events: true, Strategy: true},
	}
}

func normalize(cfg *Config) {
	aliases := make(map[string]string, len(cfg.Engine.GameAliases))
	for alias, game := range cfg.Engine.GameAliases {
		aliases[strings.ToUpper(strings.TrimSpace(alias))] = strings.ToUpper(strings.TrimSpace(game))
	}
	cfg.Engine.GameAliases = aliases
}

func applyEnvironment(cfg *Config) {
	if value := os.Getenv("AINP_MODE"); value != "" {
		cfg.Mode = value
	}
	if value := os.Getenv("AINP_SERVER_HOST"); value != "" {
		cfg.Server.Host = value
	}
	if value := os.Getenv("AINP_SERVER_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			cfg.Server.Port = port
		}
	}
	if value := os.Getenv("AINP_AUTH_TOKEN"); value != "" {
		cfg.Auth.Token = value
	}
	if value := os.Getenv("AINP_LOG_LEVEL"); value != "" {
		cfg.Log.Level = value
	}
}

func (cfg Config) Validate() error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Auth.Token == "" {
		return fmt.Errorf("auth.token must not be empty")
	}
	if cfg.Admin.Enabled {
		if !strings.HasPrefix(cfg.Admin.Path, "/") || cfg.Admin.Path == "/" || strings.Contains(cfg.Admin.Path, "?") {
			return fmt.Errorf("admin.path must be an absolute non-root URL path")
		}
		if strings.TrimSpace(cfg.Admin.LogPath) == "" || strings.TrimSpace(cfg.Admin.ExpectationsPath) == "" || strings.TrimSpace(cfg.Admin.ReportPath) == "" {
			return fmt.Errorf("admin log_path, expectations_path and report_path must not be empty")
		}
		if cfg.Admin.RefreshInterval < 0 {
			return fmt.Errorf("admin.refresh_interval must not be negative")
		}
	}
	if cfg.State.TTL <= 0 || cfg.State.MaxHands < 1 || cfg.State.PruneInterval <= 0 {
		return fmt.Errorf("state ttl, max_hands and prune_interval must be greater than zero")
	}
	if cfg.Mock.Enabled && !isAction(cfg.Mock.Action) {
		return fmt.Errorf("mock.action %q is invalid", cfg.Mock.Action)
	}
	if cfg.Mode != "engine" && cfg.Mode != "mock" {
		return fmt.Errorf("mode must be engine or mock")
	}
	if cfg.Mode == "engine" {
		if !cfg.Engine.Enabled || !cfg.Engine.Equity.Enabled || !cfg.Engine.Strategy.Enabled || !cfg.Engine.Strategy.InferLegalActions {
			return fmt.Errorf("engine mode requires engine, equity, strategy and legal-action inference to be enabled")
		}
		if cfg.Engine.DecisionTimeout <= 0 || cfg.Engine.MaxConcurrent < 1 {
			return fmt.Errorf("engine decision_timeout and max_concurrent must be greater than zero")
		}
		if strings.TrimSpace(cfg.Engine.PolicyVersion) == "" {
			return fmt.Errorf("engine.policy_version must not be empty")
		}
		if cfg.Engine.DefaultLevel < 1 || cfg.Engine.DefaultLevel > 5 {
			return fmt.Errorf("engine.default_level must be between 1 and 5")
		}
		if cfg.Engine.Strategy.MinRaiseBigBlinds <= 0 {
			return fmt.Errorf("engine.strategy.min_raise_big_blinds must be greater than zero")
		}
		if cfg.Engine.Strategy.CollapseNearAllIn && cfg.Engine.Strategy.NearAllInRemainingChips <= 0 {
			return fmt.Errorf("engine.strategy.near_allin_remaining_chips must be greater than zero when collapse_near_allin is enabled")
		}
		strategy := cfg.Engine.Strategy
		if strategy.PreflopOpenCallGap <= 0 || strategy.PreflopOpenCallGap >= .5 || strategy.PreflopReraiseEquity <= .5 || strategy.PreflopReraiseEquity > 1 || strategy.PreflopReraiseRangeFactor <= 0 || strategy.PreflopReraiseRangeFactor > 1 || strategy.PreflopExtraRaisePenalty < 0 || strategy.PreflopMultiwayPenalty < 0 || strategy.PreflopCallMargin < 0 || strategy.PreflopLargeCallBB <= 0 || invalidAirCallTuning(strategy) {
			return fmt.Errorf("engine.strategy preflop tuning values are out of range")
		}
		if cfg.Engine.FallbackToMock && !cfg.Mock.Enabled {
			return fmt.Errorf("engine.fallback_to_mock requires mock.enabled")
		}
		equity := cfg.Engine.Equity
		if equity.DefaultSamples < 1 || equity.PLO4Samples < 1 || equity.PLO5Samples < 1 || equity.PLO6Samples < 1 || equity.MaxExactOutcomes < 1 || equity.CacheCapacity < 0 {
			return fmt.Errorf("engine equity samples/limits must be positive and cache_capacity non-negative")
		}
		if equity.CacheEnabled && equity.CacheCapacity < 1 {
			return fmt.Errorf("engine.equity.cache_capacity must be positive when cache is enabled")
		}
		if cfg.Engine.Personality.Default == "" {
			return fmt.Errorf("engine.personality.default must not be empty")
		}
		if !isPersonality(cfg.Engine.Personality.Default) {
			return fmt.Errorf("engine.personality.default %q is invalid", cfg.Engine.Personality.Default)
		}
		for source, target := range cfg.Engine.Personality.ProfileMap {
			if strings.TrimSpace(source) == "" || !isPersonality(target) {
				return fmt.Errorf("engine.personality.profile_map contains invalid mapping %q=%q", source, target)
			}
		}
		for source, profile := range cfg.Engine.Personality.Profiles {
			targetsConfigured := profile.TargetVPIP != 0 || profile.TargetPFR != 0
			invalidTargets := targetsConfigured && (profile.TargetVPIP <= 0 || profile.TargetVPIP > 1 || profile.TargetPFR < 0 || profile.TargetPFR > profile.TargetVPIP)
			invalidRisk := profile.PostflopCallMargin < 0 || profile.PostflopCallMargin > .5 || profile.LargePotThreshold < 0 || profile.LargePotMinEquity < 0 || profile.LargePotMinEquity > 1 || (profile.LargePotMinEquity > 0 && profile.LargePotThreshold <= 0)
			invalidSpecial := (profile.BehaviorMode != "" && profile.BehaviorMode != "aggressive_never_fold") || profile.PreflopRaiseProbability < 0 || profile.PreflopRaiseProbability > 1 || profile.PostflopAggressionChance < 0 || profile.PostflopAggressionChance > 1 || (profile.BehaviorMode == "aggressive_never_fold" && (!profile.NeverFold || profile.TargetVPIP != 1))
			invalidSizings := false
			for index, sizing := range profile.PostflopSizings {
				if sizing <= 0 || sizing > 2 || (index > 0 && sizing <= profile.PostflopSizings[index-1]) {
					invalidSizings = true
					break
				}
			}
			if strings.TrimSpace(source) == "" || !isPersonality(profile.Personality) || profile.Level < 1 || profile.Level > 5 || invalidTargets || invalidRisk || invalidSpecial || invalidSizings {
				return fmt.Errorf("engine.personality.profiles contains invalid profile %q", source)
			}
			if _, exists := cfg.Engine.Personality.ProfileMap[source]; exists {
				return fmt.Errorf("engine.personality profile %q is defined in both profile_map and profiles", source)
			}
		}
		if cfg.Engine.OpponentModel.MaxPlayers < 1 || cfg.Engine.OpponentModel.DedupeWindow < 1 {
			return fmt.Errorf("engine opponent model limits must be greater than zero")
		}
		profit := cfg.Engine.ProfitControl
		if profit.Enabled && (profit.TTL <= 0 || profit.MaxPlayers < 1 || profit.MinimumActions < 1 ||
			profit.LargeActionBB <= 0 || profit.LargeActionStackRatio <= 0 || profit.LargeActionStackRatio > 1 ||
			profit.ProfitTriggerBB <= 0 || profit.ProfitTriggerStackRatio <= 0 ||
			profit.LossTriggerBB <= 0 || profit.LossTriggerStackRatio <= 0 ||
			profit.CallRateTarget < 0 || profit.CallRateTarget >= 1 || profit.AggressionRateTarget < 0 || profit.AggressionRateTarget >= 1 ||
			profit.MaxExposureMargin < 0 || profit.MaxPerformanceMargin < 0 || profit.MaxActionMixMargin < 0 ||
			profit.MaxTotalMargin <= 0 || profit.MaxTotalMargin > .5) {
			return fmt.Errorf("engine.profit_control values are out of range")
		}
		for _, command := range cfg.Engine.AdviseOn {
			if !validDecisionCommand(command) {
				return fmt.Errorf("engine.advise_on contains unsupported command %q", command)
			}
		}
		for alias, game := range cfg.Engine.GameAliases {
			if strings.TrimSpace(alias) == "" || !isGame(game) {
				return fmt.Errorf("engine.game_aliases contains invalid mapping %q=%q", alias, game)
			}
		}
	}
	if cfg.Phase5.Replay.Enabled && (strings.TrimSpace(cfg.Phase5.Replay.Directory) == "" || strings.TrimSpace(cfg.Phase5.Replay.FilePrefix) == "") {
		return fmt.Errorf("phase5 replay directory and file_prefix must not be empty")
	}
	if cfg.Phase5.Gray.Enabled {
		if cfg.Mode != "engine" {
			return fmt.Errorf("phase5 gray requires engine mode")
		}
		if cfg.Phase5.Gray.Mode != "shadow" && cfg.Phase5.Gray.Mode != "canary" {
			return fmt.Errorf("phase5.gray.mode must be shadow or canary")
		}
		if cfg.Phase5.Gray.Percentage < 0 || cfg.Phase5.Gray.Percentage > 100 {
			return fmt.Errorf("phase5.gray.percentage must be between 0 and 100")
		}
		if strings.TrimSpace(cfg.Phase5.Gray.Salt) == "" || strings.TrimSpace(cfg.Phase5.Gray.Candidate.PolicyVersion) == "" {
			return fmt.Errorf("phase5 gray salt and candidate policy_version must not be empty")
		}
		candidate := applyGrayCandidate(cfg.Engine, cfg.Phase5.Gray.Candidate)
		if candidate.DefaultLevel < 1 || candidate.DefaultLevel > 5 {
			return fmt.Errorf("phase5 gray candidate default_level must be between 1 and 5")
		}
		strategy := candidate.Strategy
		if strategy.PreflopOpenCallGap <= 0 || strategy.PreflopOpenCallGap >= .5 || strategy.PreflopReraiseEquity <= .5 || strategy.PreflopReraiseEquity > 1 || strategy.PreflopReraiseRangeFactor <= 0 || strategy.PreflopReraiseRangeFactor > 1 || strategy.PreflopExtraRaisePenalty < 0 || strategy.PreflopMultiwayPenalty < 0 || strategy.PreflopCallMargin < 0 || strategy.PreflopLargeCallBB <= 0 || invalidAirCallTuning(strategy) {
			return fmt.Errorf("phase5 gray candidate preflop tuning values are out of range")
		}
	}
	return nil
}

func invalidAirCallTuning(strategy StrategyConfig) bool {
	for _, value := range []float64{strategy.FlopAirCallMargin, strategy.TurnAirCallMargin, strategy.RiverAirCallMargin, strategy.RepeatedAirCallPenalty, strategy.UnderpairCallMargin, strategy.TurnWeakDrawCallMargin, strategy.RiverBoardPairCallMargin, strategy.RiverMissedDrawMargin} {
		if value < 0 || value > .5 {
			return true
		}
	}
	return false
}

func (cfg Config) GrayCandidateEngine() EngineConfig {
	return applyGrayCandidate(cfg.Engine, cfg.Phase5.Gray.Candidate)
}

func applyGrayCandidate(base EngineConfig, override GrayCandidateConfig) EngineConfig {
	base.PolicyVersion = override.PolicyVersion
	if override.DefaultLevel != nil {
		base.DefaultLevel = *override.DefaultLevel
	}
	if override.OpenCallGap != nil {
		base.Strategy.PreflopOpenCallGap = *override.OpenCallGap
	}
	if override.ReraiseEquity != nil {
		base.Strategy.PreflopReraiseEquity = *override.ReraiseEquity
	}
	if override.ExtraRaisePenalty != nil {
		base.Strategy.PreflopExtraRaisePenalty = *override.ExtraRaisePenalty
	}
	if override.MultiwayPenalty != nil {
		base.Strategy.PreflopMultiwayPenalty = *override.MultiwayPenalty
	}
	if override.CallMargin != nil {
		base.Strategy.PreflopCallMargin = *override.CallMargin
	}
	return base
}

func (cfg Config) LogLevel() slog.Level {
	switch strings.ToLower(cfg.Log.Level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (cfg ServerConfig) Address() string { return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port) }

func isAction(value string) bool {
	switch value {
	case "call", "raise", "bet", "allin", "fold", "check":
		return true
	default:
		return false
	}
}

func validDecisionCommand(value string) bool {
	switch value {
	case "deal_cards", "action", "flop", "turn", "river":
		return true
	default:
		return false
	}
}

func isGame(value string) bool {
	switch strings.ToUpper(value) {
	case "NLH", "PLO4", "PLO5", "PLO6", "SHORT_DECK", "SHORT_DECK_FIXED":
		return true
	default:
		return false
	}
}

func isPersonality(value string) bool {
	switch value {
	case "balanced", "tight_passive", "tag", "lag", "calling_station", "tricky":
		return true
	default:
		return false
	}
}
