package main

import (
	"context"
	"fmt"
	"log"
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
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	database, err := db.Open(cfg.DB.DSN())
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	registry := workflow.NewRegistry(cfg.Workflows.Dir)
	if err := registry.Load(); err != nil {
		log.Fatalf("loading workflows from %s: %v", cfg.Workflows.Dir, err)
	}
	log.Printf("loaded %d workflow(s) from %s", len(registry.List()), cfg.Workflows.Dir)

	var pub publisher.Publisher = publisher.Noop{}
	var poller *workflow.Poller

	if cfg.QueueTi.Enabled {
		qClient, err := buildQueueTiClient(context.Background(), cfg.QueueTi)
		if err != nil {
			log.Fatalf("connecting to queue-ti: %v", err)
		}
		defer qClient.Close()

		prod, err := publisher.NewQueueTiProducer(context.Background(),
			cfg.QueueTi.GRPCAddr, cfg.QueueTi.AdminURL,
			cfg.QueueTi.Username, cfg.QueueTi.Password,
		)
		if err != nil {
			log.Fatalf("creating queue-ti producer: %v", err)
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

	handler := api.NewHandler(engine, registry, repo)

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

	api.RegisterRoutes(app, handler)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		log.Printf("queuetask listening on %s", addr)
		if err := app.Listen(addr); err != nil {
			log.Printf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down...")
	if err := app.Shutdown(); err != nil {
		log.Printf("shutdown error: %v", err)
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
