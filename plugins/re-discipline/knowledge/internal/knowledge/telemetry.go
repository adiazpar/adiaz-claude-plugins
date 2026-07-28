package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const telemetryEntryLimit = 32

var telemetryProcessMu sync.Mutex

type TelemetryAggregate struct {
	SchemaVersion int              `json:"schemaVersion"`
	Mode          string           `json:"mode"`
	Entries       []TelemetryEntry `json:"entries"`
}

type TelemetryEntry struct {
	Operation        string `json:"operation"`
	EffectiveProfile string `json:"effectiveProfile"`
	Calls            uint64 `json:"calls"`
	Errors           uint64 `json:"errors"`
	LatencyMillis    uint64 `json:"latencyMillis"`
	EstimatedTokens  uint64 `json:"estimatedTokens"`
	Results          uint64 `json:"results"`
	Omissions        uint64 `json:"omissions"`
}

type telemetryObservation struct {
	Operation        string
	EffectiveProfile string
	Failed           bool
	Latency          time.Duration
	EstimatedTokens  int
	Results          int
	Omissions        int
}

func (service *Service) telemetryPath() (string, error) {
	return containedOutputPath(
		service.Index.CacheRoot, filepath.Join("telemetry", "metrics.json"))
}

func (service *Service) recordTelemetry(observation telemetryObservation) {
	if !service.Configuration.Valid ||
		!service.Configuration.Bootstrap.Knowledge.Enabled {
		return
	}
	settings := service.effectiveSettings()
	if settings.Telemetry.Mode != "metrics-only" {
		return
	}
	service.telemetryMu.Lock()
	if service.pinnedGeneration != nil {
		// A pinned service is a measurement run: thousands of queries issued
		// back to back against one frozen generation. Persisting each one costs
		// a read, a lock create, an atomic write and a lock remove - six file
		// operations per query, which measured as the single largest remaining
		// cost of a full benchmark. Buffer them instead and fold the whole run
		// into one read-modify-write at flushTelemetry.
		//
		// The counters are identical either way; only the moment they land
		// moves. A run that dies before flushing loses its own aggregate
		// metrics, never a durable artifact: the benchmark report is written
		// separately. Every interactive path is unpinned and still persists per
		// call, so status in a normal session is unaffected.
		service.telemetryBuffer = append(service.telemetryBuffer, observation)
		service.telemetryMu.Unlock()
		return
	}
	service.telemetryMu.Unlock()
	service.persistTelemetry(observation)
}

// flushTelemetry folds every observation buffered during a pinned measurement
// run into the persisted aggregate in one read-modify-write.
func (service *Service) flushTelemetry() {
	service.telemetryMu.Lock()
	buffered := service.telemetryBuffer
	service.telemetryBuffer = nil
	service.telemetryMu.Unlock()
	if len(buffered) == 0 {
		return
	}
	service.persistTelemetry(buffered...)
}

func (service *Service) persistTelemetry(observations ...telemetryObservation) {
	if len(observations) == 0 {
		return
	}
	service.telemetryMu.Lock()
	defer service.telemetryMu.Unlock()
	telemetryProcessMu.Lock()
	defer telemetryProcessMu.Unlock()

	path, err := service.telemetryPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	lock, err := acquireTelemetryLock(
		filepath.Join(filepath.Dir(path), "metrics.lock"),
	)
	if err != nil {
		return
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lock.Name())
	}()
	aggregate := TelemetryAggregate{
		SchemaVersion: 1, Mode: "metrics-only", Entries: []TelemetryEntry{},
	}
	if body, readErr := os.ReadFile(path); readErr == nil {
		if decodeStrict(body, &aggregate) != nil ||
			validateTelemetryAggregate(aggregate) != nil {
			return
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return
	}
	for _, observation := range observations {
		applyTelemetryObservation(&aggregate, observation)
	}
	sort.Slice(aggregate.Entries, func(i, j int) bool {
		if aggregate.Entries[i].Operation != aggregate.Entries[j].Operation {
			return aggregate.Entries[i].Operation < aggregate.Entries[j].Operation
		}
		return aggregate.Entries[i].EffectiveProfile <
			aggregate.Entries[j].EffectiveProfile
	})
	_ = AtomicWriteJSON(path, aggregate, 0o600)
}

// applyTelemetryObservation folds one observation into the bounded aggregate.
func applyTelemetryObservation(
	aggregate *TelemetryAggregate,
	observation telemetryObservation,
) {
	profile := observation.EffectiveProfile
	if separator := strings.IndexByte(profile, '@'); separator >= 0 {
		profile = profile[:separator]
	}
	if profile == "" {
		profile = "unavailable"
	}
	index := -1
	for candidate := range aggregate.Entries {
		entry := &aggregate.Entries[candidate]
		if entry.Operation == observation.Operation &&
			entry.EffectiveProfile == profile {
			index = candidate
			break
		}
	}
	if index < 0 {
		if len(aggregate.Entries) >= telemetryEntryLimit-1 {
			operation := "other"
			profile = "other"
			for candidate := range aggregate.Entries {
				entry := &aggregate.Entries[candidate]
				if entry.Operation == operation && entry.EffectiveProfile == profile {
					index = candidate
					break
				}
			}
			if index < 0 && len(aggregate.Entries) >= telemetryEntryLimit {
				// Convert the last bounded slot into a catch-all while
				// retaining its already-counted totals.
				index = telemetryEntryLimit - 1
				aggregate.Entries[index].Operation = operation
				aggregate.Entries[index].EffectiveProfile = profile
			}
			observation.Operation = operation
		}
		if index < 0 {
			aggregate.Entries = append(aggregate.Entries, TelemetryEntry{
				Operation: observation.Operation, EffectiveProfile: profile,
			})
			index = len(aggregate.Entries) - 1
		}
	}
	entry := &aggregate.Entries[index]
	entry.Calls = saturatingAdd(entry.Calls, 1)
	if observation.Failed {
		entry.Errors = saturatingAdd(entry.Errors, 1)
	}
	entry.LatencyMillis = saturatingAdd(
		entry.LatencyMillis,
		uint64(maxInt(0, int(observation.Latency.Milliseconds()))),
	)
	entry.EstimatedTokens = saturatingAdd(
		entry.EstimatedTokens, uint64(maxInt(0, observation.EstimatedTokens)))
	entry.Results = saturatingAdd(
		entry.Results, uint64(maxInt(0, observation.Results)))
	entry.Omissions = saturatingAdd(
		entry.Omissions, uint64(maxInt(0, observation.Omissions)))
}

func (service *Service) telemetryStatus() map[string]any {
	// Report what the aggregate will contain, not what a mid-run buffer has
	// not written yet.
	service.flushTelemetry()
	mode := service.effectiveSettings().Telemetry.Mode
	if mode != "metrics-only" {
		return map[string]any{"mode": "off", "persisted": false}
	}
	path, pathErr := service.telemetryPath()
	if pathErr != nil {
		return map[string]any{
			"mode": "metrics-only", "persisted": false,
			"diagnostic": "telemetry-path-unsafe",
		}
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{
			"mode": "metrics-only", "persisted": false,
			"entries": []TelemetryEntry{},
		}
	}
	if err != nil {
		return map[string]any{
			"mode": "metrics-only", "persisted": false,
			"diagnostic": "telemetry-unreadable",
		}
	}
	var aggregate TelemetryAggregate
	if decodeStrict(body, &aggregate) != nil ||
		validateTelemetryAggregate(aggregate) != nil {
		return map[string]any{
			"mode": "metrics-only", "persisted": false,
			"diagnostic": "telemetry-invalid",
		}
	}
	return map[string]any{
		"mode": "metrics-only", "persisted": true,
		"entries": aggregate.Entries,
	}
}

func acquireTelemetryLock(path string) (*os.File, error) {
	for attempt := 0; attempt < 100; attempt++ {
		lock, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil &&
			time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, errors.New("telemetry metrics lock is busy")
}

func validateTelemetryAggregate(aggregate TelemetryAggregate) error {
	if aggregate.SchemaVersion != 1 || aggregate.Mode != "metrics-only" ||
		len(aggregate.Entries) > telemetryEntryLimit {
		return errors.New("invalid telemetry aggregate identity or size")
	}
	for _, entry := range aggregate.Entries {
		switch entry.Operation {
		case "search", "context-pack", "other":
		default:
			return errors.New("invalid telemetry operation")
		}
		if entry.EffectiveProfile != "unavailable" &&
			entry.EffectiveProfile != "other" &&
			!managedSlugRE.MatchString(entry.EffectiveProfile) {
			return errors.New("invalid telemetry effective profile")
		}
		if entry.Errors > entry.Calls {
			return errors.New("telemetry errors exceed calls")
		}
	}
	return nil
}

func saturatingAdd(left, right uint64) uint64 {
	maximum := ^uint64(0)
	if right > maximum-left {
		return maximum
	}
	return left + right
}
