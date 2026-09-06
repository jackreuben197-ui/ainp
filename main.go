package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.com/masseffect/ainp/internal/buildinfo"
	"gitlab.com/masseffect/ainp/internal/config"
	"gitlab.com/masseffect/ainp/internal/dashboard"
	"gitlab.com/masseffect/ainp/internal/replay"
	"gitlab.com/masseffect/ainp/internal/service"
	"gitlab.com/masseffect/ainp/internal/store"
	"gitlab.com/masseffect/ainp/internal/web"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		panic(fmt.Errorf("load config: %w", err))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel()}))
	handStore := store.NewMemoryHandStoreWithLimit(cfg.State.TTL, cfg.State.MaxHands, cfg.State.PruneInterval)
	var decisionProvider service.DecisionProvider
	if cfg.Mode == "engine" {
		if cfg.Phase5.Gray.Enabled {
			decisionProvider = service.NewGrayDecisionProvider(cfg, logger)
		} else {
			decisionProvider = service.NewEngineDecisionProvider(cfg, logger)
		}
	} else {
		decisionProvider = service.NewMockDecisionProvider(cfg.Mock)
	}
	eventService := service.NewEventService(handStore, decisionProvider, logger, cfg.Log.Events)
	var replayRecorder replay.Recorder
	if cfg.Phase5.Replay.Enabled {
		replayRecorder, err = replay.NewJSONLRecorder(cfg.Phase5.Replay.Directory, cfg.Phase5.Replay.FilePrefix, cfg.Phase5.Replay.FlushEachWrite, time.Now())
		if err != nil {
			panic(fmt.Errorf("initialize replay recorder: %w", err))
		}
		eventService.SetRecorder(replayRecorder)
		logger.Info("replay_recorder_started", "path", replayRecorder.Path())
	}
	var dashboardManager *dashboard.Manager
	if cfg.Admin.Enabled {
		dashboardManager = dashboard.NewManager(cfg.Admin, logger)
		defer dashboardManager.Close()
	}
	router := web.NewRouterWithDashboard(cfg, eventService, logger, dashboardManager)

	server := &http.Server{
		Addr:              cfg.Server.Address(),
		Handler:           router,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	go func() {
		build := buildinfo.Current()
		logger.Info("ainp started",
			"address", server.Addr,
			"mode", cfg.Mode,
			"policy_version", cfg.Engine.PolicyVersion,
			"build_revision", build.Revision,
			"build_time", build.Time,
			"build_modified", build.Modified,
			"go_version", build.Go,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("ainp stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	if replayRecorder != nil {
		if err := replayRecorder.Close(); err != nil {
			logger.Error("replay recorder close failed", "error", err)
		}
	}
}
