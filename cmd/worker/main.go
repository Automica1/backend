// cmd/worker/main.go — Automica background job worker (GPU pool, scheduled tasks).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/database"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/internal/services"
	workerhandlers "chi-mongo-backend/internal/worker/handlers"
	"chi-mongo-backend/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.NewMongoDB(cfg)
	if err != nil {
		log.Fatalf("failed to connect mongodb: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Close(ctx)
	}()

	jobRepo := repository.NewJobRepository(db.GetCollection("jobs"))
	poolRepo := repository.NewGPUPoolRepository(db.GetCollection("gpu_pools"))
	provisionConfigRepo := repository.NewGPUProvisionConfigRepository(db.GetCollection("gpu_provision_configs"))
	userRepo := repository.NewUserRepository(db.GetCollection("users"))
	creditsRepo := repository.NewCreditsRepository(db.GetCollection("credits"))
	jobSvc := services.NewJobService(jobRepo)
	creditsSvc := services.NewCreditsService(creditsRepo, userRepo)
	provisionConfigSvc := services.NewGPUProvisionConfigService(provisionConfigRepo)
	gpuPoolSvc := services.NewGPUPoolService(poolRepo, jobSvc, creditsSvc, provisionConfigSvc, cfg)

	registry := worker.NewRegistry()
	workerhandlers.RegisterSystemHandlers(registry)
	gpuHandlers := workerhandlers.NewGPUPoolHandlers(cfg, poolRepo, jobRepo, gpuPoolSvc, provisionConfigSvc)
	gpuHandlers.Register(registry)

	w := worker.New(cfg, jobRepo, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := gpuPoolSvc.ReconcileIdleWarmPools(ctx); err != nil {
					log.Printf("idle warm pool reconcile: %v", err)
				}
			}
		}
	}()

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("worker shutting down...")
		cancel()
		w.Stop()
	}()

	w.Run(ctx)
	log.Println("worker exited")
}
