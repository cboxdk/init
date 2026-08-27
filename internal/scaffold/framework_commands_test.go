package scaffold

import (
	"strings"
	"testing"
)

// TestPresetsUseTheirOwnFrameworkCommands: the Symfony preset shared Laravel's
// queue block, so it generated `php artisan queue:work` with restart: always.
// artisan does not exist in a Symfony app, so three workers crash-looped from
// first boot — and check-config passes it, because it validates structure, not
// command semantics.
func TestPresetsUseTheirOwnFrameworkCommands(t *testing.T) {
	tests := []struct {
		preset      Preset
		mustHave    []string
		mustNotHave []string
	}{
		{
			preset:      PresetSymfony,
			mustHave:    []string{"bin/console", "messenger:consume"},
			mustNotHave: []string{"artisan"},
		},
		{
			preset:      PresetLaravel,
			mustHave:    []string{"artisan"},
			mustNotHave: []string{"bin/console", "bin/magento", "drush"},
		},
		{
			preset:      PresetMagento,
			mustHave:    []string{"bin/magento"},
			mustNotHave: []string{"artisan", "bin/console"},
		},
		{
			preset:      PresetWordPress,
			mustNotHave: []string{"artisan", "bin/console", "bin/magento"},
		},
		{
			preset:      PresetDrupal,
			mustNotHave: []string{"artisan", "bin/console", "bin/magento"},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.preset), func(t *testing.T) {
			content, err := GenerateConfig(DefaultConfig(tt.preset))
			if err != nil {
				t.Fatalf("GenerateConfig: %v", err)
			}
			for _, want := range tt.mustHave {
				if !strings.Contains(content, want) {
					t.Errorf("%s config does not mention %q", tt.preset, want)
				}
			}
			for _, unwanted := range tt.mustNotHave {
				if strings.Contains(content, unwanted) {
					t.Errorf("%s config runs %q, which belongs to another framework; "+
						"under restart: always this crash-loops from first boot",
						tt.preset, unwanted)
				}
			}
		})
	}
}

// TestNginxDocRootMatchesFramework: root was hardcoded to WorkDir/public, so
// every real request 404'd on WordPress, Drupal and Magento — hidden by the
// nginx /health stub, which answers with a literal `return 200`, so cbox-init
// reported the process healthy forever.
func TestNginxDocRootMatchesFramework(t *testing.T) {
	tests := map[Preset]string{
		PresetWordPress: "root /var/www/html;",
		PresetDrupal:    "root /var/www/html/web;",
		PresetMagento:   "root /var/www/html/pub;",
		PresetLaravel:   "root /var/www/html/public;",
		PresetSymfony:   "root /var/www/html/public;",
	}

	for preset, wantRoot := range tests {
		t.Run(string(preset), func(t *testing.T) {
			cfg := DefaultConfig(preset)
			content, err := GenerateNginxConfig(cfg)
			if err != nil {
				t.Fatalf("GenerateNginxConfig: %v", err)
			}
			if !strings.Contains(content, wantRoot) {
				t.Errorf("%s nginx.conf lacks %q; every request would 404", preset, wantRoot)
			}
		})
	}
}

// TestGeneratedConfigIsInstalledWhereCboxInitLooks: the discovery order is
// $CBOX_INIT_CONFIG, /etc/cbox-init/cbox-init.yaml, ./cbox-init.yaml. Installing
// to /etc/cbox-init/config.yaml meant the scaffolded config was silently ignored
// and the base image's default ran instead.
func TestGeneratedConfigIsInstalledWhereCboxInitLooks(t *testing.T) {
	for _, preset := range []Preset{PresetLaravel, PresetNodeJS} {
		t.Run(string(preset), func(t *testing.T) {
			cfg := DefaultConfig(preset)

			compose, err := GenerateDockerCompose(cfg)
			if err != nil {
				t.Fatalf("GenerateDockerCompose: %v", err)
			}
			dockerfile, err := GenerateDockerfile(cfg)
			if err != nil {
				t.Fatalf("GenerateDockerfile: %v", err)
			}

			for name, content := range map[string]string{"compose": compose, "Dockerfile": dockerfile} {
				if strings.Contains(content, "/etc/cbox-init/config.yaml") {
					t.Errorf("%s installs the config at /etc/cbox-init/config.yaml, "+
						"which is not in the discovery order", name)
				}
			}
		})
	}
}

// TestGeneratedComposeHasNoBakedInPasswords: the compose template shipped
// MYSQL_ROOT_PASSWORD=secret and GF_SECURITY_ADMIN_PASSWORD=admin.
func TestGeneratedComposeHasNoBakedInPasswords(t *testing.T) {
	cfg := DefaultConfig(PresetLaravel)
	cfg.EnableMetrics = true
	content, err := GenerateDockerCompose(cfg)
	if err != nil {
		t.Fatalf("GenerateDockerCompose: %v", err)
	}

	for _, baked := range []string{"PASSWORD=secret", "PASSWORD=admin"} {
		if strings.Contains(content, baked) {
			t.Errorf("compose file ships a literal %q", baked)
		}
	}
}
