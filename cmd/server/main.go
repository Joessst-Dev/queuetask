package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/lib/pq"

	queueti "github.com/Joessst-Dev/queue-ti/clients/go-client"

	"github.com/Joessst-Dev/queuetask/internal/api"
	"github.com/Joessst-Dev/queuetask/internal/config"
	"github.com/Joessst-Dev/queuetask/internal/db"
	"github.com/Joessst-Dev/queuetask/internal/notify"
	"github.com/Joessst-Dev/queuetask/internal/publisher"
	"github.com/Joessst-Dev/queuetask/internal/ui"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.DB.DSN())
	if err != nil {
		slog.Error("opening database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("running migrations", "error", err)
		os.Exit(1)
	}

	registry := workflow.NewRegistry(cfg.Workflows.Dir)
	if err := registry.Load(); err != nil {
		slog.Error("loading workflows", "dir", cfg.Workflows.Dir, "error", err)
		os.Exit(1)
	}
	slog.Info("workflows loaded", "count", len(registry.List()), "dir", cfg.Workflows.Dir)

	var pub publisher.Publisher = publisher.Noop{}
	var poller *workflow.Poller
	var qClient *queueti.Client

	if cfg.QueueTi.Enabled {
		var err error
		qClient, err = publisher.DialClient(context.Background(),
			cfg.QueueTi.GRPCAddr, cfg.QueueTi.AdminURL,
			cfg.QueueTi.Username, cfg.QueueTi.Password,
		)
		if err != nil {
			slog.Error("connecting to queue-ti", "error", err)
			os.Exit(1)
		}
		defer qClient.Close()

		prod, err := publisher.NewQueueTiProducer(context.Background(),
			cfg.QueueTi.GRPCAddr, cfg.QueueTi.AdminURL,
			cfg.QueueTi.Username, cfg.QueueTi.Password,
		)
		if err != nil {
			slog.Error("creating queue-ti producer", "error", err)
			os.Exit(1)
		}
		defer prod.Close()
		pub = prod

		poller = workflow.NewPoller(qClient)
	}

	notifier := notify.Build(cfg.Notifications)

	repo := workflow.NewRepository(database)
	engine := workflow.NewEngine(repo, registry, pub, poller, notifier)
	if poller != nil {
		poller.SetEngine(engine)
	}

	// Wire the broadcaster: engine notifies PG, PG notifies all nodes' broadcasters.
	broadcaster := ui.NewBroadcaster()
	engine.SetOnStateChange(func() {
		_ = repo.PGNotify(context.Background())
	})

	pgListener := pq.NewListener(cfg.DB.DSN(), 10*time.Second, time.Minute,
		func(ev pq.ListenerEventType, err error) {
			if err != nil {
				slog.Warn("pg listener event", "error", err)
			}
			// On reconnect, force a refresh so SSE clients don't miss state changes
			// that occurred while the listener was disconnected.
			if ev == pq.ListenerEventReconnected {
				broadcaster.Notify()
			}
		},
	)
	if err := pgListener.Listen("queuetask_state_change"); err != nil {
		slog.Error("pg listen failed", "error", err)
		os.Exit(1)
	}
	defer pgListener.Close()
	go func() {
		for range pgListener.Notify {
			broadcaster.Notify()
		}
	}()

	// Cron scheduler — always active, no queue-ti dependency.
	cronScheduler := workflow.NewCronScheduler(engine, repo)
	cronScheduler.Start()
	defer cronScheduler.Stop()

	// Instance poller — only when queue-ti is enabled.
	var instancePoller *workflow.InstancePoller
	if qClient != nil {
		instancePoller = workflow.NewInstancePoller(qClient, engine)
		defer instancePoller.Stop()
	}

	// Hook into registry reloads so schedulers stay in sync.
	syncSchedulers := func(defs []*workflow.Definition) {
		cronScheduler.Sync(defs)
		if instancePoller != nil {
			instancePoller.Sync(defs)
		}
	}
	registry.AddReloadHook(syncSchedulers)
	syncSchedulers(registry.List())

	handler := api.NewHandler(engine, registry, repo, notifier)

	uiHandler, err := ui.NewHandler(engine, repo, registry, broadcaster)
	if err != nil {
		slog.Error("building UI handler", "error", err)
		os.Exit(1)
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		},
	})
	app.Use(recover.New())
	app.Use(logger.New())

	uiHandler.RegisterRoutes(app)
	api.RegisterRoutes(app, handler)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		slog.Info("listening", "addr", addr)
		if err := app.Listen(addr); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	<-quit
	slog.Info("shutting down")
	if err := app.Shutdown(); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}

