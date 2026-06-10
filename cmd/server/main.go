package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	queueti "github.com/Joessst-Dev/queue-ti/clients/go-client"

	"github.com/Joessst-Dev/queuetask/internal/api"
	"github.com/Joessst-Dev/queuetask/internal/config"
	"github.com/Joessst-Dev/queuetask/internal/db"
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
		qClient, err = buildQueueTiClient(context.Background(), cfg.QueueTi)
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

	repo := workflow.NewRepository(database)
	engine := workflow.NewEngine(repo, registry, pub, poller)
	if poller != nil {
		poller.SetEngine(engine)
	}

	// Cron scheduler — always active, no queue-ti dependency.
	cronScheduler := workflow.NewCronScheduler(engine)
	cronScheduler.Start()
	defer cronScheduler.Stop()

	// Instance poller — only when queue-ti is enabled.
	var instancePoller *workflow.InstancePoller
	if qClient != nil {
		instancePoller = workflow.NewInstancePoller(qClient, engine)
		defer instancePoller.Stop()
	}

	// Hook into registry reloads so schedulers stay in sync.
	registry.AddReloadHook(func(defs []*workflow.Definition) {
		cronScheduler.Sync(defs)
		if instancePoller != nil {
			instancePoller.Sync(defs)
		}
	})

	// Initial sync with the already-loaded definitions.
	cronScheduler.Sync(registry.List())
	if instancePoller != nil {
		instancePoller.Sync(registry.List())
	}

	handler := api.NewHandler(engine, registry, repo)

	uiHandler, err := ui.NewHandler(engine, repo, registry)
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

// buildQueueTiClient creates an authenticated (or unauthenticated) queue-ti client
// used by the Poller for consumer subscriptions.
func buildQueueTiClient(ctx context.Context, cfg config.QueueTiConfig) (*queueti.Client, error) {
	opts := []queueti.DialOption{queueti.WithInsecure()}

	if cfg.Username != "" || cfg.Password != "" {
		auth, err := queueti.NewAuth(ctx, cfg.AdminURL, cfg.Username, cfg.Password)
		if err != nil {
			return nil, fmt.Errorf("queue-ti auth: %w", err)
		}
		if token := auth.Token(); token != "" {
			opts = append(opts,
				queueti.WithBearerToken(token),
				queueti.WithTokenRefresher(auth.Refresh),
			)
		}
	}

	return queueti.Dial(cfg.GRPCAddr, opts...)
}
