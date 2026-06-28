// Command gen refreshes the bundled LiteLLM price snapshot. Run occasionally
// from the repo root to update internal/pricing/data; it is NOT part of the
// build (a network fetch must stay out of `go build`/CI).
//
//	go run ./internal/pricing/gen
//
// ponytail: manual regen — the live cache (`council cost prices refresh`) keeps
// runtime prices fresh; this only updates the offline fallback shipped in the
// binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/umutarmut38/council/internal/pricing"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tuples, err := pricing.FetchLiteLLM(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch litellm:", err)
		os.Exit(1)
	}
	dir := filepath.Join("internal", "pricing", "data")
	data, err := json.Marshal(tuples)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(dir, "litellm-snapshot.json"), data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// time.Now() is fine here: this is a manual command, not a workflow/test.
	date := time.Now().UTC().Format("2006-01-02") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "snapshot-date.txt"), []byte(date), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d models to %s (snapshot date %s)\n", len(tuples), dir, date)
}
