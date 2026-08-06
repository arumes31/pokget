// Command detection_fixtures downloads and renders deterministic detection fixtures.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"pokget/internal/detectiontest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "detection fixtures: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	manifest, err := detectiontest.LoadManifest()
	if err != nil {
		return err
	}

	seed := flag.Int64(
		"seed",
		manifest.DefaultSeed,
		"deterministic fixture selection seed",
	)
	count := flag.Int(
		"count",
		detectiontest.SupportedGameCount,
		"number of cards to select (must equal the six supported TCGs)",
	)
	timeout := flag.Duration(
		"timeout",
		30*time.Second,
		"timeout for each source image request",
	)
	flag.Parse()

	downloader, err := detectiontest.NewDownloader(detectiontest.DownloaderConfig{
		Client: &http.Client{Timeout: *timeout},
	})
	if err != nil {
		return err
	}
	generator, err := detectiontest.NewGenerator(detectiontest.GeneratorConfig{
		Downloader: downloader,
		OutputRoot: "artifacts/detection",
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	runPath, err := generator.Generate(ctx, *seed, *count)
	if err != nil {
		return err
	}
	fmt.Println(runPath)
	return nil
}
