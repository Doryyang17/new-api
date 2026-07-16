package model

import "strings"

// NormalizeUsageModelName returns the model identity persisted by usage logs.
func NormalizeUsageModelName(modelName string) string {
	if strings.HasPrefix(modelName, "gpt-4-gizmo") {
		return "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(modelName, "gpt-4o-gizmo") {
		return "gpt-4o-gizmo-*"
	}
	return modelName
}

// UsageModelMatches reports whether an observed model belongs to a configured limit.
func UsageModelMatches(configuredModel string, observedModel string) bool {
	configuredCandidates := expandUsageScopeModels([]string{configuredModel})
	observedCandidates := expandUsageScopeModels([]string{observedModel})
	for _, configured := range configuredCandidates {
		prefix := strings.TrimSuffix(configured, "*")
		for _, observed := range observedCandidates {
			if configured == observed {
				return true
			}
			if strings.HasSuffix(configured, "*") && strings.HasPrefix(observed, prefix) {
				return true
			}
		}
	}
	return false
}

func expandUsageScopeModels(models []string) []string {
	expanded := make([]string, 0, len(models)*2)
	seen := make(map[string]struct{}, len(models)*2)
	for _, modelName := range models {
		for _, candidate := range []string{modelName, NormalizeUsageModelName(modelName)} {
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			expanded = append(expanded, candidate)
		}
	}
	return expanded
}

func usageModelLikePattern(modelName string, clickHouse bool) string {
	prefix := strings.TrimSuffix(modelName, "*")
	if clickHouse {
		prefix = strings.ReplaceAll(prefix, `\`, `\\`)
		prefix = strings.ReplaceAll(prefix, `%`, `\%`)
		prefix = strings.ReplaceAll(prefix, `_`, `\_`)
		return prefix + "%"
	}
	prefix = strings.ReplaceAll(prefix, "!", "!!")
	prefix = strings.ReplaceAll(prefix, "%", "!%")
	prefix = strings.ReplaceAll(prefix, "_", "!_")
	return prefix + "%"
}
