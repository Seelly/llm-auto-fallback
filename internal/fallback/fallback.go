package fallback

import (
	"sort"
	"strings"

	"github.com/seelly/llm-auto-fallback/internal/config"
	"github.com/seelly/llm-auto-fallback/internal/prober"
)

// Engine makes fallback decisions based on model availability.
type Engine struct {
	cfg    *config.Config
	prober *prober.Prober
}

func New(cfg *config.Config, p *prober.Prober) *Engine {
	return &Engine{cfg: cfg, prober: p}
}

// Prober returns the underlying prober instance.
func (e *Engine) Prober() *prober.Prober {
	return e.prober
}

// Resolve returns the best available model given a preferred model.
// Returns the resolved model name and the provider that hosts it.
// If no model is available, returns ("", "").
func (e *Engine) Resolve(preferred string) (model string, provider string) {
	// Step 1: if preferred is available, use it directly
	if e.prober.IsAvailable(preferred) {
		prov, _ := e.prober.ProviderFor(preferred)
		return preferred, prov
	}

	// Step 2: check custom fallback chain
	if model, provider := e.resolveCustom(preferred); model != "" {
		return model, provider
	}

	// Step 3: name similarity matching (same family, version downgrade)
	if model, provider := e.resolveSimilar(preferred); model != "" {
		return model, provider
	}

	// Step 4: cross-family fallback via global priority
	if model, provider := e.resolveGlobal(); model != "" {
		return model, provider
	}

	return "", ""
}

func (e *Engine) resolveCustom(preferred string) (string, string) {
	custom := e.cfg.Fallback.Custom
	if len(custom) == 0 {
		return "", ""
	}

	// Find preferred in the custom list
	idx := -1
	for i, m := range custom {
		if m == preferred {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", ""
	}

	// Walk forward from that position
	for i := idx + 1; i < len(custom); i++ {
		if e.prober.IsAvailable(custom[i]) {
			prov, _ := e.prober.ProviderFor(custom[i])
			return custom[i], prov
		}
	}
	return "", ""
}

func (e *Engine) resolveSimilar(preferred string) (string, string) {
	family := extractFamily(preferred)
	if family == "" {
		return "", ""
	}

	// Collect all models in the same family
	allModels := e.prober.GetAllModels()
	var candidates []string
	for m := range allModels {
		if extractFamily(m) == family && m != preferred {
			candidates = append(candidates, m)
		}
	}

	// Sort by version descending (higher version first = closer to preferred)
	sort.Slice(candidates, func(i, j int) bool {
		return compareVersions(candidates[i], candidates[j]) > 0
	})

	for _, m := range candidates {
		if e.prober.IsAvailable(m) {
			prov, _ := e.prober.ProviderFor(m)
			return m, prov
		}
	}
	return "", ""
}

func (e *Engine) resolveGlobal() (string, string) {
	available := e.prober.GetAvailableModels()
	if len(available) == 0 {
		return "", ""
	}

	// Build priority map
	priority := make(map[string]int)
	for i, name := range e.cfg.Fallback.GlobalPriority {
		priority[name] = i
	}

	// Sort available models by global priority, then by name
	sort.Slice(available, func(i, j int) bool {
		pi := priority[available[i].Provider]
		pj := priority[available[j].Provider]
		if pi != pj {
			return pi < pj
		}
		return available[i].Model < available[j].Model
	})

	return available[0].Model, available[0].Provider
}

// extractFamily extracts the model family prefix.
// e.g. "claude-opus-4.6" -> "claude", "deepseek-v4" -> "deepseek", "qwen3-max" -> "qwen"
func extractFamily(model string) string {
	model = strings.ToLower(model)
	// Try to split on common separators and find the family prefix
	// Strategy: look for the first digit in the name, everything before it is the family
	for i, c := range model {
		if c >= '0' && c <= '9' {
			family := strings.TrimRight(model[:i], "-_.")
			if family != "" {
				return family
			}
		}
	}
	// Fallback: use the first segment before a dash
	if idx := strings.Index(model, "-"); idx > 0 {
		return model[:idx]
	}
	return model
}

// compareVersions compares two model version strings.
// Returns positive if a > b, negative if a < b, 0 if equal.
func compareVersions(a, b string) int {
	// Extract version numbers from model names
	// e.g. "claude-opus-4.6" -> [4, 6], "claude-sonnet-4.5" -> [4, 5]
	numsA := extractNumbers(a)
	numsB := extractNumbers(b)

	for i := 0; i < len(numsA) && i < len(numsB); i++ {
		if numsA[i] != numsB[i] {
			return numsA[i] - numsB[i]
		}
	}
	return len(numsA) - len(numsB)
}

func extractNumbers(s string) []int {
	var nums []int
	current := 0
	inNum := false

	for _, c := range s {
		if c >= '0' && c <= '9' {
			current = current*10 + int(c-'0')
			inNum = true
		} else {
			if inNum {
				nums = append(nums, current)
				current = 0
				inNum = false
			}
		}
	}
	if inNum {
		nums = append(nums, current)
	}
	return nums
}
