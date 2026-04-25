package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/seelly/llm-auto-fallback/internal/config"
	"github.com/seelly/llm-auto-fallback/internal/fallback"
	"github.com/seelly/llm-auto-fallback/internal/forwarder"
	"github.com/seelly/llm-auto-fallback/internal/prober"
	"github.com/seelly/llm-auto-fallback/internal/proxy"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize components
	pb := prober.New(cfg)
	engine := fallback.New(cfg, pb)
	fwd := forwarder.New(cfg, engine)
	handler := proxy.New(fwd)

	// Start prober
	go pb.Start(ctx)

	// HTTP server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		pb.Stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	log.Printf("model-auto-fallback listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
