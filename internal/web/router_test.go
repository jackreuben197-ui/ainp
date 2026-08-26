package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/dashboard"
	"gitlab.com/smoothsics/ainp/internal/protocol"
	"gitlab.com/smoothsics/ainp/internal/service"
	"gitlab.com/smoothsics/ainp/internal/store"
)

func TestAiconCompatibleFlow(t *testing.T) {
	router := testRouter()

	check := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	check.Header.Set("Authorization", "Bearer test-token")
	checkResponse := httptest.NewRecorder()
	router.ServeHTTP(checkResponse, check)
	if checkResponse.Code != http.StatusOK {
		t.Fatalf("check status = %d", checkResponse.Code)
	}

	start := eventBody(1, "start_hand_extended", map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "test", "time": 1,
		"small_blind": 1, "big_blind": 2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "bot", "nick": "bot", "stack": 100}, {"player_id": "human", "nick": "human", "stack": 100}},
	})
	response := performEvent(router, start, "test-token")
	if response.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", response.Code, response.Body.String())
	}
	var startResult protocol.EventResponse
	if err := json.Unmarshal(response.Body.Bytes(), &startResult); err != nil {
		t.Fatal(err)
	}
	if startResult.SeqNum != 1 || startResult.Advise != nil {
		t.Fatalf("start result = %+v", startResult)
	}

	deal := eventBody(2, "deal_cards", map[string]any{"cards": "AsKd"})
	response = performEvent(router, deal, "test-token")
	if response.Code != http.StatusOK {
		t.Fatalf("deal status = %d, body = %s", response.Code, response.Body.String())
	}
	var dealResult protocol.EventResponse
	if err := json.Unmarshal(response.Body.Bytes(), &dealResult); err != nil {
		t.Fatal(err)
	}
	if dealResult.Advise == nil || dealResult.Advise.Type != protocol.ActionCheck {
		t.Fatalf("deal result = %+v", dealResult)
	}

	response = performEvent(router, deal, "test-token")
	if response.Code != http.StatusAlreadyReported {
		t.Fatalf("duplicate status = %d", response.Code)
	}
}

func TestAuthenticationAndSequenceErrors(t *testing.T) {
	router := testRouter()
	response := performEvent(router, eventBody(1, "deal_cards", map[string]any{"cards": "AsKd"}), "wrong")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}

	response = performEvent(router, eventBody(1, "deal_cards", map[string]any{"cards": "AsKd"}), "test-token")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("broken hand status = %d", response.Code)
	}
	var failure protocol.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.ErrorCode != protocol.ErrorBrokenHand || failure.RequestID == "" {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestEveryAPICallAndEventOutcomeAreLoggedWithoutCredentials(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cfg := config.Config{Auth: config.AuthConfig{Token: "secret-token"}, Mock: config.MockConfig{Enabled: true, Action: "check", AdviseOn: []string{"deal_cards"}}, Log: config.LogConfig{Access: true, Events: true}}
	events := service.NewEventService(store.NewMemoryHandStore(time.Hour), service.NewMockDecisionProvider(cfg.Mock), logger)
	router := NewRouter(cfg, events, logger)

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	_ = performEvent(router, []byte(`{"bad":`), "secret-token")
	_ = performEvent(router, eventBody(1, "deal_cards", map[string]any{"cards": "AsKd"}), "wrong-token")
	start := eventBody(1, "start_hand_extended", map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "test", "time": 1,
		"small_blind": 1, "big_blind": 2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "bot", "nick": "bot", "stack": 100}, {"player_id": "human", "nick": "human", "stack": 100}},
	})
	_ = performEvent(router, start, "secret-token")
	_ = performEvent(router, start, "secret-token")

	text := logs.String()
	if count := strings.Count(text, `"msg":"http_access"`); count != 5 {
		t.Fatalf("http access count=%d logs=%s", count, text)
	}
	for _, expected := range []string{`"msg":"event_decode_error"`, `"msg":"auth_rejected"`, `"outcome":"applied"`, `"outcome":"duplicate"`, `"decision_id":`, `"event_fingerprint":`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in logs=%s", expected, text)
		}
	}
	if strings.Contains(text, "secret-token") || strings.Contains(text, "wrong-token") {
		t.Fatalf("credential leaked in logs=%s", text)
	}
}

func TestDashboardPageAndAPIs(t *testing.T) {
	directory := t.TempDir()
	reportPath := filepath.Join(directory, "report.json")
	if err := os.WriteFile(reportPath, []byte(`{"generated_at":"2026-08-05T00:00:00Z","passed":true,"strategy_decisions":12}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Auth.Token = "test-token"
	cfg.Admin = config.AdminConfig{Enabled: true, Path: "/admin", LogPath: filepath.Join(directory, "ainp.log"), ExpectationsPath: filepath.Join(directory, "audit.yaml"), ReportPath: reportPath}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := dashboard.NewManager(cfg.Admin, logger)
	defer manager.Close()
	router := NewRouterWithDashboard(cfg, service.NewEventService(store.NewMemoryHandStore(time.Hour), service.NewMockDecisionProvider(cfg.Mock), logger, false), logger, manager)

	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "AinP 运行审计") {
		t.Fatalf("page=%d %s", page.Code, page.Body.String())
	}
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/api/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized=%d", unauthorized.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/api/report", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"strategy_decisions":12`) {
		t.Fatalf("report=%d %s", response.Code, response.Body.String())
	}
}

func TestRealEngineEventFlowReturnsStrategyAdviceAndDeviation(t *testing.T) {
	router := realEngineRouter(t)
	start := eventBody(1, "start_hand_extended", map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1,
		"small_blind": 1, "big_blind": 2, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{
			{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"},
			{"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"},
			{"player_id": "bot", "nick": "bot", "stack": 100, "role": "bt"},
		},
		"blinds": []map[string]any{{"player_id": "sb", "value": 1, "type": "small_blind"}, {"player_id": "bb", "value": 2, "type": "big_blind"}},
	})
	if response := performEvent(router, start, "test-token"); response.Code != http.StatusOK {
		t.Fatalf("start=%d %s", response.Code, response.Body.String())
	}
	deal := decodeEventResponse(t, performEvent(router, eventBody(2, "deal_cards", map[string]any{"cards": "AsAh"}), "test-token"))
	if deal.Advise == nil || deal.Advise.Type != protocol.ActionRaise || deal.Advise.Value == nil {
		t.Fatalf("preflop advice=%+v", deal)
	}
	raiseValue := *deal.Advise.Value
	heroAction := decodeEventResponse(t, performEvent(router, eventBody(3, "action", map[string]any{"player_id": "bot", "type": "raise", "value": raiseValue}), "test-token"))
	if heroAction.Deviation != nil || heroAction.Advise != nil {
		t.Fatalf("hero action response=%+v", heroAction)
	}
	_ = decodeEventResponse(t, performEvent(router, eventBody(4, "action", map[string]any{"player_id": "sb", "type": "fold", "value": 0}), "test-token"))
	callValue := raiseValue - 2
	_ = decodeEventResponse(t, performEvent(router, eventBody(5, "action", map[string]any{"player_id": "bb", "type": "call", "value": callValue}), "test-token"))
	flop := decodeEventResponse(t, performEvent(router, eventBody(6, "flop", map[string]any{"cards": "2c3d7h"}), "test-token"))
	if flop.Advise != nil {
		t.Fatalf("hero is not first postflop: %+v", flop)
	}
	postflop := decodeEventResponse(t, performEvent(router, eventBody(7, "action", map[string]any{"player_id": "bb", "type": "check", "value": 0}), "test-token"))
	if postflop.Advise == nil || postflop.Advise.Type != protocol.ActionBet || postflop.Advise.Value == nil {
		t.Fatalf("postflop advice=%+v", postflop)
	}
	deviation := decodeEventResponse(t, performEvent(router, eventBody(8, "action", map[string]any{"player_id": "bot", "type": "check", "value": 0}), "test-token"))
	if deviation.Deviation == nil || deviation.Deviation.ByType == nil || deviation.Deviation.ByType.Expected != protocol.ActionBet {
		t.Fatalf("deviation=%+v", deviation)
	}
}

func TestRealEngineSupportsPLO6ThroughHTTP(t *testing.T) {
	router := realEngineRouter(t)
	start := eventBody(1, "start_hand_extended", map[string]any{
		"game_type": "PLO6", "club_id": "1", "ai_profile": "balanced", "time": 1,
		"small_blind": 1, "big_blind": 2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}, {"player_id": "bot", "nick": "bot", "stack": 100, "role": "bt"}},
		"blinds":  []map[string]any{{"player_id": "bb", "value": 2, "type": "big_blind"}},
	})
	if response := performEvent(router, start, "test-token"); response.Code != http.StatusOK {
		t.Fatalf("start=%d %s", response.Code, response.Body.String())
	}
	deal := decodeEventResponse(t, performEvent(router, eventBody(2, "deal_cards", map[string]any{"cards": "AsKsQdJc5h4h"}), "test-token"))
	if deal.Advise == nil || !protocol.ValidAction(deal.Advise.Type) {
		t.Fatalf("PLO6 advice=%+v", deal)
	}
}

func TestRealEngineSupportsCardsDealtOnlyWhenHeroMustAct(t *testing.T) {
	router := realEngineRouter(t)
	start := eventBody(1, "start_hand_extended", map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "balanced", "time": 1,
		"small_blind": 1, "big_blind": 2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{
			{"player_id": "human", "nick": "human", "stack": 100, "role": "sb"},
			{"player_id": "bot", "nick": "bot", "stack": 100, "role": "bb"},
		},
		"blinds": []map[string]any{{"player_id": "human", "value": 1, "type": "small_blind"}, {"player_id": "bot", "value": 2, "type": "big_blind"}},
	})
	if response := performEvent(router, start, "test-token"); response.Code != http.StatusOK {
		t.Fatalf("start=%d %s", response.Code, response.Body.String())
	}
	humanCall := decodeEventResponse(t, performEvent(router, eventBody(2, "action", map[string]any{"player_id": "human", "type": "call", "value": 1}), "test-token"))
	if humanCall.Advise != nil {
		t.Fatalf("advice before delayed cards=%+v", humanCall)
	}
	started := time.Now()
	deal := decodeEventResponse(t, performEvent(router, eventBody(3, "deal_cards", map[string]any{"cards": "AsKd"}), "test-token"))
	if deal.Advise == nil || !protocol.ValidAction(deal.Advise.Type) {
		t.Fatalf("delayed deal advice=%+v", deal)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("delayed deal decision took %s", elapsed)
	}
}

func TestEngineFallbackToMockIsExplicitlyConfigurable(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Token = "test-token"
	cfg.Engine.DecisionTimeout = time.Nanosecond
	cfg.Engine.FallbackToMock = true
	cfg.Engine.Equity.PreflopLookupEnabled = false
	cfg.Engine.Personality.ApplyThinkTime = false
	cfg.Log = config.LogConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := service.NewEngineDecisionProvider(cfg, logger)
	router := NewRouter(cfg, service.NewEventService(store.NewMemoryHandStore(time.Hour), provider, logger, false), logger)
	start := eventBody(1, "start_hand_extended", map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "balanced", "time": 1,
		"small_blind": 1, "big_blind": 2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}, {"player_id": "bot", "nick": "bot", "stack": 100, "role": "bt"}},
		"blinds":  []map[string]any{{"player_id": "bb", "value": 2, "type": "big_blind"}},
	})
	_ = decodeEventResponse(t, performEvent(router, start, "test-token"))
	deal := decodeEventResponse(t, performEvent(router, eventBody(2, "deal_cards", map[string]any{"cards": "AsAh"}), "test-token"))
	if deal.Advise == nil || deal.Advise.Type != protocol.ActionCheck {
		t.Fatalf("fallback advice=%+v", deal)
	}
}

func testRouter() http.Handler {
	cfg := config.Config{Auth: config.AuthConfig{Token: "test-token"}, Mock: config.MockConfig{Enabled: true, Action: "check", AdviseOn: []string{"deal_cards"}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := service.NewEventService(store.NewMemoryHandStore(time.Hour), service.NewMockDecisionProvider(cfg.Mock), logger)
	return NewRouter(cfg, events, logger)
}

func realEngineRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.Auth.Token = "test-token"
	cfg.Engine.Personality.ApplyThinkTime = false
	cfg.Engine.Personality.HumanizationEnabled = false
	cfg.Engine.Equity.DefaultSamples = 2_000
	cfg.Engine.Equity.PLO6Samples = 300
	cfg.Log = config.LogConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := service.NewEngineDecisionProvider(cfg, logger)
	events := service.NewEventService(store.NewMemoryHandStore(time.Hour), provider, logger, false)
	return NewRouter(cfg, events, logger)
}

func decodeEventResponse(t *testing.T, response *httptest.ResponseRecorder) protocol.EventResponse {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("event status=%d body=%s", response.Code, response.Body.String())
	}
	var result protocol.EventResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func eventBody(sequence int, command string, payload any) []byte {
	body, _ := json.Marshal(map[string]any{"seq_num": sequence, "player_id": "bot", "room_id": "fishcn", "table_id": "1", "hand_id": "2", "cmd": command, "payload": payload})
	return body
}

func performEvent(router http.Handler, body []byte, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/event", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
