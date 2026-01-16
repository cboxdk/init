package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExpandEnv expands environment variables in config content
// Supports ${VAR:-default} and ${VAR} syntax
func ExpandEnv(content string) string {
	// Pattern: ${VAR:-default} or ${VAR}
	pattern := regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

	return pattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		defaultValue := ""
		if len(parts) >= 3 {
			defaultValue = parts[2]
		}

		// Get from environment or use default
		if value := os.Getenv(varName); value != "" {
			return value
		}

		return defaultValue
	})
}

// LoadWithEnvExpansion loads config file, expands env vars, and applies ENV overrides
func LoadWithEnvExpansion(path string) (*Config, error) {
	rawConfig := map[string]interface{}{}

	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "ℹ️  No config file found at %s, using environment variables only\n", path)
	} else {
		expanded := ExpandEnv(string(content))
		if err := yaml.Unmarshal([]byte(expanded), &rawConfig); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	if err := applyEnvOverridesMap(rawConfig); err != nil {
		return nil, err
	}

	mergedBytes, err := yaml.Marshal(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize merged config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(mergedBytes, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse merged config: %w", err)
	}

	if cfg.Processes == nil {
		cfg.Processes = make(map[string]*Process)
	}

	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// fieldNode represents a YAML field tree for env overrides
type fieldNode struct {
	key      string
	children map[string]*fieldNode
}

func buildFieldNode(t reflect.Type) *fieldNode {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	node := &fieldNode{
		children: map[string]*fieldNode{},
	}

	if t.Kind() != reflect.Struct {
		return node
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		tag = strings.Split(tag, ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		child := buildFieldNode(field.Type)
		child.key = tag
		node.children[normalizeKey(tag)] = child
	}

	return node
}

var (
	globalFieldTree  = buildFieldNode(reflect.TypeOf(GlobalConfig{}))
	processFieldTree = buildFieldNode(reflect.TypeOf(Process{}))
)

func normalizeKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(key, "-", "_"))
}

func matchFieldPath(node *fieldNode, tokens []string) ([]string, bool) {
	if len(tokens) == 0 {
		return []string{}, true
	}

	for i := len(tokens); i >= 1; i-- {
		candidate := strings.Join(tokens[:i], "_")
		child, ok := node.children[candidate]
		if !ok {
			continue
		}
		rest, ok := matchFieldPath(child, tokens[i:])
		if ok {
			return append([]string{child.key}, rest...), true
		}
	}

	return nil, false
}

func ensureMap(m map[string]interface{}, key string) map[string]interface{} {
	val, ok := m[key]
	if ok {
		if cast, ok := val.(map[string]interface{}); ok {
			return cast
		}
	}
	newMap := map[string]interface{}{}
	m[key] = newMap
	return newMap
}

func setNestedValue(root map[string]interface{}, path []string, value interface{}) {
	if len(path) == 0 {
		return
	}
	current := root
	for i := 0; i < len(path)-1; i++ {
		current = ensureMap(current, path[i])
	}
	current[path[len(path)-1]] = value
}

func parseEnvValue(raw string) interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "{") {
		var data interface{}
		if err := json.Unmarshal([]byte(raw), &data); err == nil {
			return data
		}
	}

	if v, err := strconv.ParseBool(raw); err == nil {
		return v
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}

	return raw
}

func ensureGlobalMap(raw map[string]interface{}) map[string]interface{} {
	return ensureMap(raw, "global")
}

func ensureProcessMap(raw map[string]interface{}) map[string]interface{} {
	return ensureMap(raw, "processes")
}

func applyEnvOverridesMap(raw map[string]interface{}) error {
	globalMap := ensureGlobalMap(raw)
	processesMap := ensureProcessMap(raw)

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]

		switch {
		case strings.HasPrefix(key, "CBOX_INIT_GLOBAL_"):
			segment := strings.TrimPrefix(key, "CBOX_INIT_GLOBAL_")
			path := buildPathFromKey(segment, globalFieldTree)
			if len(path) == 0 {
				continue
			}
			setNestedValue(globalMap, path, parseEnvValue(value))
		case strings.HasPrefix(key, "CBOX_INIT_PROCESS_"):
			segment := strings.TrimPrefix(key, "CBOX_INIT_PROCESS_")
			applyProcessEnvOverride(processesMap, segment, value)
		}
	}

	return nil
}

func buildPathFromKey(segment string, tree *fieldNode) []string {
	tokens := strings.Split(strings.ToLower(segment), "_")
	path, ok := matchFieldPath(tree, tokens)
	if !ok {
		return nil
	}
	return path
}

func applyProcessEnvOverride(processes map[string]interface{}, segment string, value string) {
	if segment == "" {
		return
	}

	if idx := strings.Index(segment, "_ENV_"); idx != -1 {
		procEncoded := segment[:idx]
		envKey := segment[idx+len("_ENV_"):]
		if envKey == "" {
			return
		}
		name := decodeProcessName(processes, procEncoded)
		if name == "" {
			name = normalizeProcessName(procEncoded)
		}
		procMap := ensureMap(processes, name)
		envMap := ensureMap(procMap, "env")
		envMap[envKey] = value
		return
	}

	tokens := strings.Split(segment, "_")
	if len(tokens) < 2 {
		return
	}

	lowerTokens := make([]string, len(tokens))
	for i, token := range tokens {
		lowerTokens[i] = strings.ToLower(token)
	}

	for split := len(tokens) - 1; split >= 1; split-- {
		fieldTokens := lowerTokens[split:]
		path, ok := matchFieldPath(processFieldTree, fieldTokens)
		if !ok {
			continue
		}
		procEncoded := strings.Join(tokens[:split], "_")
		name := decodeProcessName(processes, procEncoded)
		if name == "" {
			name = normalizeProcessName(procEncoded)
		}
		procMap := ensureMap(processes, name)
		setNestedValue(procMap, path, parseEnvValue(value))
		return
	}
}

func decodeProcessName(processes map[string]interface{}, encoded string) string {
	target := strings.ToUpper(encoded)
	for name := range processes {
		existing := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		if existing == target {
			return name
		}
	}
	return ""
}

func normalizeProcessName(encoded string) string {
	return strings.ToLower(strings.ReplaceAll(encoded, "_", "-"))
}
