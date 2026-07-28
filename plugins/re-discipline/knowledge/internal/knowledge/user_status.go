package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UserStatusBlock is the plain-language rendering of knowledge and memory
// state. Its strings are printed to the user verbatim by hosts and skills,
// so the translation from machinery state to plain English happens here,
// deterministically, and nowhere else. Machinery vocabulary must never
// appear in these strings; the system block carries the technical state.
type UserStatusBlock struct {
	Knowledge string   `json:"knowledge"`
	Memory    string   `json:"memory"`
	Attention []string `json:"attention"`
}

func systemObject(system map[string]any, key string) map[string]any {
	value, ok := system[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return value
}

func systemBool(container map[string]any, key string) bool {
	value, ok := container[key].(bool)
	return ok && value
}

func systemString(system map[string]any, key string) string {
	value, _ := system[key].(string)
	return value
}

// BuildUserStatus translates a system status payload into the user block.
// Verdict priority: damaged configuration, knowledge disabled, damaged
// index, then working. A merely-unrefreshed index is "working": it repairs
// itself on the next search or ensure run and is never the user's problem.
// Benchmark staleness and evidence-pin drift are deliberately absent here;
// the measurement skills handle them at their own gate time.
func BuildUserStatus(system map[string]any) UserStatusBlock {
	user := UserStatusBlock{Attention: []string{}}

	configuration := systemObject(system, "configuration")
	memoryMode, _ := configuration["memoryMode"].(string)
	if memoryMode == "" {
		memoryMode = "unknown"
	}
	pending := -1
	switch value := system["memoryProposalsPending"].(type) {
	case int:
		pending = value
	case float64:
		pending = int(value)
	}
	switch {
	case pending == 0:
		user.Memory = memoryMode + "; no proposals waiting"
	case pending > 0:
		user.Memory = fmt.Sprintf("%s; %d proposals awaiting review", memoryMode, pending)
	default:
		user.Memory = memoryMode
	}

	if !systemBool(configuration, "valid") {
		user.Knowledge = "needs attention: the knowledge system is paused"
		user.Attention = append(user.Attention,
			"The knowledge system's internal configuration file is damaged, so search is paused. "+
				"Say 'repair the knowledge configuration' and I will fix it.")
		return user
	}

	if knowledgeBlock, ok := system["knowledge"].(map[string]any); ok {
		if enabled, present := knowledgeBlock["enabled"].(bool); present && !enabled {
			user.Knowledge = "off"
			return user
		}
	}

	index := systemObject(system, "index")
	switch {
	case systemBool(index, "present") && systemBool(index, "integrity"):
		user.Knowledge = "working"
	case systemBool(index, "present"):
		user.Knowledge = "needs attention: the search index is damaged"
		user.Attention = append(user.Attention,
			"The search index is damaged and will be rebuilt on the next search. "+
				"Nothing is lost; say 'rebuild the knowledge index' to do it now.")
	default:
		user.Knowledge = "working (building on first use)"
	}

	// A project profile that is requested but not effective (any fallback
	// reason) is a calibration outcome waiting on a human. An accepted
	// project profile also keeps the project: prefix but runs with no
	// fallback, and must not nag. The in-process payload carries a *string;
	// a decoded JSON payload carries string or nil.
	fallback := ""
	switch value := system["fallbackReason"].(type) {
	case string:
		fallback = value
	case *string:
		if value != nil {
			fallback = *value
		}
	}
	if strings.HasPrefix(systemString(system, "requestedProfile"), "project:") &&
		fallback != "" {
		user.Attention = append(user.Attention,
			"A tuned search profile from a previous calibration is awaiting your decision - "+
				"accept or reject it with decide-retrieval-profile.")
	}

	return user
}

// countPendingProposals counts proposal files awaiting review. INDEX files
// and directories are not proposals. A read error returns -1 so the user
// block degrades to the bare memory mode instead of inventing a count.
func countPendingProposals(projectRoot string) int {
	entries, err := os.ReadDir(filepath.Join(projectRoot, ".re-discipline", "memory", "proposals"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		return -1
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(name, "INDEX.md") || !strings.HasSuffix(name, ".md") {
			continue
		}
		count++
	}
	return count
}
