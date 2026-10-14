package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/config"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
)

// bucketCORS keeps the bucket's allowed origins in step with the API's CORS_ORIGINS, on every deploy.
func bucketCORS() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Bucket == nil {
		fmt.Println("bucket-cors: no bucket configured, skipped")
		return nil
	}
	// Railway's buckets do not match wildcard origins; listing one would only mislead.
	var origins []string
	for _, o := range cfg.CORSOrigins {
		if !strings.Contains(o, "*") {
			origins = append(origins, o)
		}
	}
	if err := platform.NewBucket(platform.BucketConfig(*cfg.Bucket)).SetCORS(context.Background(), origins); err != nil {
		return fmt.Errorf("bucket-cors: %w", err)
	}
	fmt.Println("bucket-cors: allowed", strings.Join(origins, ", "))
	return nil
}
