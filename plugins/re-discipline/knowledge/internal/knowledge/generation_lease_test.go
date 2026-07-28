package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func publishTestGeneration(
	t *testing.T,
	service *Service,
	root string,
	marker int,
) Generation {
	t.Helper()
	writeTestFile(t, filepath.Join(root, "docs", "truth", "engine.md"), fmt.Sprintf(
		`# Engine contract

The exact engine identifier is A1B2C3D4.

Frame serialization uses a checksum before durable commit.

The canonical protocol name is engine-frame-v7.

publication-marker-%03d
`, marker))
	generation, _, rebuilt, err := service.Index.Ensure(context.Background())
	if err != nil {
		t.Fatalf("publication %d: %v", marker, err)
	}
	if !rebuilt {
		t.Fatalf("publication %d did not produce a new generation", marker)
	}
	return generation
}

func generationDatabaseCount(t *testing.T, cacheRoot string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheRoot, "generations"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sqlite") {
			count++
		}
	}
	return count
}

func leaseRecordNames(t *testing.T, cacheRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheRoot, "leases"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func writeOrphanLease(
	t *testing.T,
	cacheRoot string,
	name string,
	record generationLeaseRecord,
) string {
	t.Helper()
	directory := filepath.Join(cacheRoot, "leases")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdversarialMeasurementLeaseSurvivesConcurrentPublication(t *testing.T) {
	t.Run("retention skips a leased generation and reclaims it after release", func(t *testing.T) {
		root := makeAdversarialProject(t)
		reader := newAdversarialService(t, root, nil)
		publisher := newAdversarialService(t, root, nil)
		ctx := context.Background()

		generation, _, lease, err := reader.leaseMeasurementGeneration(ctx, "benchmark")
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Release()
		reader.PinGeneration(generation)

		// Retention keeps only the active generation plus the two newest
		// superseded ones, so five publications retire the leased generation
		// several times over.
		for marker := 0; marker < 5; marker++ {
			publishTestGeneration(t, publisher, root, marker)
		}
		if _, err := os.Stat(generation.Database); err != nil {
			t.Fatalf("retention deleted a leased generation: %v", err)
		}
		response, err := reader.Search(ctx, SearchOptions{
			Query: "A1B2C3D4", QueryClass: "exact",
			AllowedTiers: []string{"truth"}, Limit: 5, TokenBudget: 512,
		})
		if err != nil {
			t.Fatalf("pinned reader failed against its leased generation: %v", err)
		}
		if response.Metadata.Generation != generation.ID {
			t.Fatalf("pinned reader drifted to generation %s, expected %s",
				response.Metadata.Generation, generation.ID)
		}

		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
		if err := lease.Release(); err != nil {
			t.Fatalf("releasing a released lease is not idempotent: %v", err)
		}
		if names := leaseRecordNames(t, reader.Index.CacheRoot); len(names) != 0 {
			t.Fatalf("release left lease records behind: %v", names)
		}
		for marker := 5; marker < 7; marker++ {
			publishTestGeneration(t, publisher, root, marker)
		}
		if _, err := os.Stat(generation.Database); !os.IsNotExist(err) {
			t.Fatalf("retention never reclaimed the released generation: %v", err)
		}
	})

	t.Run("a benchmark-style read loop completes while another session publishes", func(t *testing.T) {
		root := makeAdversarialProject(t)
		reader := newAdversarialService(t, root, nil)
		publisher := newAdversarialService(t, root, nil)
		ctx := context.Background()

		generation, _, lease, err := reader.leaseMeasurementGeneration(ctx, "benchmark")
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Release()
		reader.PinGeneration(generation)

		stop := make(chan struct{})
		done := make(chan struct{})
		var published atomic.Int64
		go func() {
			defer close(done)
			for marker := 0; ; marker++ {
				select {
				case <-stop:
					return
				default:
				}
				// Each publication adds its own source file, so the concurrent
				// session never races the reader for one path.
				path := filepath.Join(root, "docs", "truth",
					fmt.Sprintf("concurrent-%03d.md", marker))
				body := fmt.Sprintf(
					"# Concurrent %03d\n\nconcurrent-publication-marker-%03d\n",
					marker, marker)
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					continue
				}
				if _, _, rebuilt, err := publisher.Index.Ensure(ctx); err == nil && rebuilt {
					published.Add(1)
				}
			}
		}()

		deadline := time.Now().Add(90 * time.Second)
		iteration := 0
		for iteration < 24 || published.Load() < 4 {
			if time.Now().After(deadline) {
				break
			}
			response, err := reader.Search(ctx, SearchOptions{
				Query: "A1B2C3D4", QueryClass: "exact",
				AllowedTiers: []string{"truth"}, Limit: 5, TokenBudget: 512,
			})
			if err != nil {
				close(stop)
				<-done
				t.Fatalf("iteration %d failed under concurrent publication: %v",
					iteration, err)
			}
			if response.Metadata.Generation != generation.ID {
				close(stop)
				<-done
				t.Fatalf("iteration %d drifted to generation %s, expected %s",
					iteration, response.Metadata.Generation, generation.ID)
			}
			iteration++
		}
		close(stop)
		<-done
		if iteration < 24 || published.Load() < 4 {
			t.Fatalf("the race did not run: %d reads against %d concurrent publications",
				iteration, published.Load())
		}
		if _, err := os.Stat(generation.Database); err != nil {
			t.Fatalf("retention deleted the generation under an active run: %v", err)
		}
	})
}

func TestAdversarialStaleGenerationLeaseCannotPinForever(t *testing.T) {
	root := makeAdversarialProject(t)
	reader := newAdversarialService(t, root, nil)
	publisher := newAdversarialService(t, root, nil)
	ctx := context.Background()

	generation, _, _, _, err := reader.ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot := reader.Index.CacheRoot
	now := time.Now()

	// A crashed run leaves a well-formed record whose operating-system lock
	// died with its process.
	crashed := writeOrphanLease(t, cacheRoot, generation.ID+".999999-crashed.lease",
		generationLeaseRecord{
			SchemaVersion: 1, GenerationID: generation.ID, Purpose: "benchmark",
			ProcessID: 999999, AcquiredAt: RFC3339UTC(now),
			ExpiresAt: RFC3339UTC(now.Add(generationLeaseTTL)),
		})
	expired := writeOrphanLease(t, cacheRoot, generation.ID+".999998-expired.lease",
		generationLeaseRecord{
			SchemaVersion: 1, GenerationID: generation.ID, Purpose: "calibration",
			ProcessID: 999998, AcquiredAt: RFC3339UTC(now.Add(-48 * time.Hour)),
			ExpiresAt: RFC3339UTC(now.Add(-24 * time.Hour)),
		})
	malformed := filepath.Join(cacheRoot, "leases", generation.ID+".1-malformed.lease")
	if err := os.WriteFile(malformed, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	for marker := 0; marker < 5; marker++ {
		publishTestGeneration(t, publisher, root, marker)
	}
	for _, path := range []string{crashed, expired, malformed} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale lease %s survived the sweep: %v", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(generation.Database); !os.IsNotExist(err) {
		t.Fatalf("a crashed run pinned its generation forever: %v", err)
	}
}

func TestAdversarialPinnedMeasurementNeverPublishesOrDrifts(t *testing.T) {
	root := makeAdversarialProject(t)
	service := newAdversarialService(t, root, nil)
	ctx := context.Background()

	generation, _, _, _, err := service.ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service.PinGeneration(generation)
	before := generationDatabaseCount(t, service.Index.CacheRoot)

	writeTestFile(t, filepath.Join(root, "docs", "truth", "engine.md"),
		"# Engine contract\n\nThe exact engine identifier is A1B2C3D4.\n\npinned-drift-marker\n")

	pinned, _, _, rebuilt, err := service.ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt || pinned.ID != generation.ID {
		t.Fatalf("a pinned service rebuilt to generation %s (rebuilt=%v)",
			pinned.ID, rebuilt)
	}
	response, err := service.Search(ctx, SearchOptions{
		Query: "A1B2C3D4", QueryClass: "exact",
		AllowedTiers: []string{"truth"}, Limit: 5, TokenBudget: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Metadata.Generation != generation.ID {
		t.Fatalf("a pinned search reported generation %s, expected %s",
			response.Metadata.Generation, generation.ID)
	}
	if after := generationDatabaseCount(t, service.Index.CacheRoot); after != before {
		t.Fatalf("a pinned service published %d new generation(s)", after-before)
	}
}
