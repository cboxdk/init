package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
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
		// Reject unknown keys (typos like `comand:` for `command:` or
		// `health_chek:` for `health_check:`) with the file's own line numbers,
		// before the env-merge below flattens line information. Without this a
		// misspelled key was silently dropped — a mistyped `health_check` turned
		// off health checking with no warning. KnownFields only rejects keys that
		// are not struct fields; arbitrary process names (map keys) are unaffected.
		strictDec := yaml.NewDecoder(strings.NewReader(expanded))
		strictDec.KnownFields(true)
		var strictCheck Config
		if err := strictDec.Decode(&strictCheck); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("invalid configuration (%s): %w", path, err)
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
	if _, err := cfg.ValidateComprehensive(); err != nil {
		return nil, fmt.Errorf("comprehensive config validation failed: %w", err)
	}

	return &cfg, nil
}

// fieldNode represents a YAML field tree for env overrides
type fieldNode struct {
	key      string
	children map[string]*fieldNode
}

func buildFieldNode(t reflect.Type) *fieldNode {
	for t.Kind() == reflect.Pointer {
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
	hookFieldTree    = buildFieldNode(reflect.TypeOf(Hook{}))
)

// hookEnvTypes maps the env-var segment for each hook list to its YAML key,
// in the order hook lists appear in HooksConfig.
var hookEnvTypes = []struct {
	envPrefix string
	yamlKey   string
}{
	{"PRE_START_", "pre-start"},
	{"POST_START_", "post-start"},
	{"PRE_STOP_", "pre-stop"},
	{"POST_STOP_", "post-stop"},
}

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
	hookCollector := map[string]map[int]map[string]interface{}{}

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
		case strings.HasPrefix(key, "CBOX_INIT_HOOK_"):
			segment := strings.TrimPrefix(key, "CBOX_INIT_HOOK_")
			collectHookEnvOverride(hookCollector, segment, value)
		case strings.HasPrefix(key, "CBOX_INIT_PROCESS_"):
			segment := strings.TrimPrefix(key, "CBOX_INIT_PROCESS_")
			applyProcessEnvOverride(processesMap, segment, value)
		}
	}

	applyHookEnvOverrides(raw, hookCollector)

	return nil
}

// collectHookEnvOverride parses one CBOX_INIT_HOOK_<TYPE>_<N>_<FIELD> variable
// into the collector, keyed by hook-list YAML key and index. Unknown types,
// indexes, or fields are ignored, matching the behavior of the other prefixes.
func collectHookEnvOverride(collector map[string]map[int]map[string]interface{}, segment string, value string) {
	var yamlKey string
	for _, ht := range hookEnvTypes {
		if strings.HasPrefix(segment, ht.envPrefix) {
			yamlKey = ht.yamlKey
			segment = strings.TrimPrefix(segment, ht.envPrefix)
			break
		}
	}
	if yamlKey == "" {
		return
	}

	sep := strings.Index(segment, "_")
	if sep <= 0 {
		return
	}
	index, err := strconv.Atoi(segment[:sep])
	if err != nil || index < 0 {
		return
	}
	fieldSegment := segment[sep+1:]
	if fieldSegment == "" {
		return
	}

	// Hook environment variables keep their key verbatim, like process ENV_.
	if envKey := strings.TrimPrefix(fieldSegment, "ENV_"); envKey != fieldSegment && envKey != "" {
		envMap := ensureMap(hookEnvEntry(collector, yamlKey, index), "env")
		envMap[envKey] = value
		return
	}

	// ALLOW_FAILURE is the documented env spelling for continue_on_error.
	normalized := normalizeKey(fieldSegment)
	if normalized == "allow_failure" {
		normalized = "continue_on_error"
	}

	path, ok := matchFieldPath(hookFieldTree, strings.Split(normalized, "_"))
	if !ok || len(path) == 0 {
		return
	}

	hookMap := hookEnvEntry(collector, yamlKey, index)
	if path[0] == "command" {
		hookMap["command"] = parseCommandValue(value)
		return
	}
	setNestedValue(hookMap, path, parseEnvValue(value))
}

// hookEnvEntry returns (creating as needed) the collector entry for one hook.
func hookEnvEntry(collector map[string]map[int]map[string]interface{}, yamlKey string, index int) map[string]interface{} {
	byIndex, ok := collector[yamlKey]
	if !ok {
		byIndex = map[int]map[string]interface{}{}
		collector[yamlKey] = byIndex
	}
	hookMap, ok := byIndex[index]
	if !ok {
		hookMap = map[string]interface{}{}
		byIndex[index] = hookMap
	}
	return hookMap
}

// applyHookEnvOverrides appends env-defined hooks to the raw config, after any
// YAML-defined hooks, ordered by their env index within each hook list.
func applyHookEnvOverrides(raw map[string]interface{}, collector map[string]map[int]map[string]interface{}) {
	if len(collector) == 0 {
		return
	}

	hooksMap := ensureMap(raw, "hooks")
	for _, ht := range hookEnvTypes {
		byIndex, ok := collector[ht.yamlKey]
		if !ok {
			continue
		}

		indexes := make([]int, 0, len(byIndex))
		for index := range byIndex {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)

		list, _ := hooksMap[ht.yamlKey].([]interface{})
		for _, index := range indexes {
			list = append(list, byIndex[index])
		}
		hooksMap[ht.yamlKey] = list
	}
}

// parseCommandValue parses a command list from an env value: a JSON array
// (["php","artisan","migrate"]) or a comma-separated list (php,artisan,migrate).
func parseCommandValue(raw string) interface{} {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		var arr []interface{}
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			return arr
		}
	}

	parts := strings.Split(raw, ",")
	command := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			command = append(command, part)
		}
	}
	return command
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
		if len(path) == 1 && path[0] == "command" {
			setNestedValue(procMap, path, parseCommandValue(value))
		} else {
			setNestedValue(procMap, path, parseEnvValue(value))
		}
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
