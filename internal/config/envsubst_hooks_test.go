package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHookEnvOverrides_Basic(t *testing.T) {
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_NAME", "stache-warm")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_COMMAND", "php,please,stache:warm")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_TIMEOUT", "300")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_ALLOW_FAILURE", "true")

	raw := map[string]any{}
	if err := applyEnvOverridesMap(raw); err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	hooks, ok := raw["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks map not created")
	}
	preStart, ok := hooks["pre-start"].([]any)
	if !ok || len(preStart) != 1 {
		t.Fatalf("pre-start = %v, want 1 hook", hooks["pre-start"])
	}

	hook := preStart[0].(map[string]any)
	if hook["name"] != "stache-warm" {
		t.Errorf("name = %v, want stache-warm", hook["name"])
	}
	command, ok := hook["command"].([]any)
	if !ok || len(command) != 3 || command[0] != "php" || command[1] != "please" || command[2] != "stache:warm" {
		t.Errorf("command = %v, want [php please stache:warm]", hook["command"])
	}
	if hook["timeout"] != int64(300) {
		t.Errorf("timeout = %v, want 300", hook["timeout"])
	}
	if hook["continue_on_error"] != true {
		t.Errorf("continue_on_error = %v, want true (via ALLOW_FAILURE)", hook["continue_on_error"])
	}
}

func TestHookEnvOverrides_JSONCommandAndContinueOnError(t *testing.T) {
	t.Setenv("CBOX_INIT_HOOK_POST_START_0_COMMAND", `["php", "artisan", "queue:restart"]`)
	t.Setenv("CBOX_INIT_HOOK_POST_START_0_CONTINUE_ON_ERROR", "true")
	t.Setenv("CBOX_INIT_HOOK_POST_START_0_RETRY", "2")
	t.Setenv("CBOX_INIT_HOOK_POST_START_0_RETRY_DELAY", "5")
	t.Setenv("CBOX_INIT_HOOK_POST_START_0_WORKING_DIR", "/var/www/html")
	t.Setenv("CBOX_INIT_HOOK_POST_START_0_ENV_APP_ENV", "production")

	raw := map[string]any{}
	if err := applyEnvOverridesMap(raw); err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	hooks := raw["hooks"].(map[string]any)
	postStart, ok := hooks["post-start"].([]any)
	if !ok || len(postStart) != 1 {
		t.Fatalf("post-start = %v, want 1 hook", hooks["post-start"])
	}

	hook := postStart[0].(map[string]any)
	command, ok := hook["command"].([]any)
	if !ok || len(command) != 3 || command[2] != "queue:restart" {
		t.Errorf("command = %v, want JSON array parsed", hook["command"])
	}
	if hook["continue_on_error"] != true {
		t.Errorf("continue_on_error = %v, want true", hook["continue_on_error"])
	}
	if hook["retry"] != int64(2) {
		t.Errorf("retry = %v, want 2", hook["retry"])
	}
	if hook["retry_delay"] != int64(5) {
		t.Errorf("retry_delay = %v, want 5", hook["retry_delay"])
	}
	if hook["working_dir"] != "/var/www/html" {
		t.Errorf("working_dir = %v, want /var/www/html", hook["working_dir"])
	}
	env, ok := hook["env"].(map[string]any)
	if !ok || env["APP_ENV"] != "production" {
		t.Errorf("env = %v, want APP_ENV=production", hook["env"])
	}
}

func TestHookEnvOverrides_AppendAfterYAMLAndIndexOrder(t *testing.T) {
	// Deliberately define index 1 and 0; order must follow the index, after YAML hooks.
	t.Setenv("CBOX_INIT_HOOK_PRE_START_1_COMMAND", "php,artisan,event:cache")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_COMMAND", "php,artisan,config:cache")

	raw := map[string]any{
		"hooks": map[string]any{
			"pre-start": []any{
				map[string]any{
					"name":    "migrate",
					"command": []any{"php", "artisan", "migrate", "--force"},
				},
			},
		},
	}
	if err := applyEnvOverridesMap(raw); err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	preStart := raw["hooks"].(map[string]any)["pre-start"].([]any)
	if len(preStart) != 3 {
		t.Fatalf("pre-start has %d hooks, want 3", len(preStart))
	}
	if name := preStart[0].(map[string]any)["name"]; name != "migrate" {
		t.Errorf("first hook = %v, want YAML-defined migrate", name)
	}
	cmd1 := preStart[1].(map[string]any)["command"].([]any)
	if cmd1[2] != "config:cache" {
		t.Errorf("second hook command = %v, want env index 0 (config:cache)", cmd1)
	}
	cmd2 := preStart[2].(map[string]any)["command"].([]any)
	if cmd2[2] != "event:cache" {
		t.Errorf("third hook command = %v, want env index 1 (event:cache)", cmd2)
	}
}

func TestHookEnvOverrides_IgnoresInvalidKeys(t *testing.T) {
	t.Setenv("CBOX_INIT_HOOK_PRE_START_X_COMMAND", "echo,ignored") // non-numeric index
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_BOGUS_FIELD", "ignored")  // unknown field
	t.Setenv("CBOX_INIT_HOOK_MID_FLIGHT_0_COMMAND", "echo,nope")   // unknown hook type
	t.Setenv("CBOX_INIT_HOOK_NAME", "set-by-executor")             // executor-injected var

	raw := map[string]any{}
	if err := applyEnvOverridesMap(raw); err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	if hooks, ok := raw["hooks"].(map[string]any); ok {
		if preStart, ok := hooks["pre-start"].([]any); ok && len(preStart) > 0 {
			t.Errorf("pre-start = %v, want no hooks from invalid keys", preStart)
		}
	}
}

func TestHookEnvOverrides_FullLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "cbox-init.yaml")
	yamlContent := `
version: "1.0"
processes:
  app:
    enabled: true
    command: ["sleep", "1"]
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_COMMAND", "php,please,stache:warm")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_TIMEOUT", "300")
	t.Setenv("CBOX_INIT_HOOK_PRE_START_0_ALLOW_FAILURE", "true")

	cfg, err := LoadWithEnvExpansion(configPath)
	if err != nil {
		t.Fatalf("LoadWithEnvExpansion() error = %v", err)
	}

	if len(cfg.Hooks.PreStart) != 1 {
		t.Fatalf("PreStart has %d hooks, want 1", len(cfg.Hooks.PreStart))
	}
	hook := cfg.Hooks.PreStart[0]
	if hook.Name != "pre-start-0" {
		t.Errorf("Name = %q, want default pre-start-0", hook.Name)
	}
	if len(hook.Command) != 3 || hook.Command[0] != "php" {
		t.Errorf("Command = %v, want [php please stache:warm]", hook.Command)
	}
	if hook.Timeout != 300 {
		t.Errorf("Timeout = %d, want 300", hook.Timeout)
	}
	if !hook.ContinueOnError {
		t.Error("ContinueOnError = false, want true (via ALLOW_FAILURE)")
	}
}

func TestProcessEnvOverride_CommaSeparatedCommand(t *testing.T) {
	t.Setenv("CBOX_INIT_PROCESS_WORKER_COMMAND", "php, artisan, queue:work")

	raw := map[string]any{}
	if err := applyEnvOverridesMap(raw); err != nil {
		t.Fatalf("applyEnvOverridesMap() error = %v", err)
	}

	worker := raw["processes"].(map[string]any)["worker"].(map[string]any)
	command, ok := worker["command"].([]any)
	if !ok || len(command) != 3 || command[0] != "php" || command[2] != "queue:work" {
		t.Errorf("command = %v, want [php artisan queue:work]", worker["command"])
	}
}

func TestValidateHooks(t *testing.T) {
	base := func() *Config {
		cfg := &Config{
			Processes: map[string]*Process{
				"app": {Enabled: true, Command: []string{"sleep", "1"}},
			},
		}
		cfg.SetDefaults()
		return cfg
	}

	t.Run("missing command", func(t *testing.T) {
		cfg := base()
		cfg.Hooks.PreStart = []Hook{{Name: "broken"}}
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() = nil, want error for hook without command")
		}
	})

	t.Run("negative timeout", func(t *testing.T) {
		cfg := base()
		cfg.Hooks.PostStop = []Hook{{Name: "neg", Command: []string{"true"}, Timeout: -1}}
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() = nil, want error for negative timeout")
		}
	})

	t.Run("valid hooks", func(t *testing.T) {
		cfg := base()
		cfg.Hooks.PreStart = []Hook{{Command: []string{"true"}, Timeout: 30, ContinueOnError: true}}
		cfg.SetDefaults()
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
		if cfg.Hooks.PreStart[0].Name != "pre-start-0" {
			t.Errorf("Name = %q, want default pre-start-0", cfg.Hooks.PreStart[0].Name)
		}
	})
}
