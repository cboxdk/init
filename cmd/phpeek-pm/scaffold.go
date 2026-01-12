package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gophpeek/phpeek-pm/internal/scaffold"
	"github.com/spf13/cobra"
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [preset]",
	Short: "Generate PHPeek PM configuration files",
	Long: `Scaffold generates PHPeek PM configuration files for common use cases.

Available presets:
  PHP:
    laravel     - Laravel with Horizon, Queue workers, Scheduler
    symfony     - Symfony with Messenger queue workers
    php         - Vanilla PHP with Nginx
    wordpress   - WordPress with WP-CLI cron
    magento     - Magento 2 with queue consumers, cron, indexer
    drupal      - Drupal with Drush cron

  Node.js:
    nextjs      - Next.js standalone with Nginx reverse proxy
    nuxt        - Nuxt 3 with Nitro server, Nginx reverse proxy
    nodejs      - Node.js with workers, Nginx reverse proxy

Use --observability to add tracing, metrics, and API to any preset.

Examples:
  phpeek-pm scaffold laravel
  phpeek-pm scaffold wordpress --observability
  phpeek-pm scaffold nextjs --dockerfile --nginx
  phpeek-pm scaffold magento --docker-compose --observability`,
	Args: cobra.MaximumNArgs(1),
	Run:  runScaffold,
}

var (
	interactive     bool
	outputDir       string
	generateDocker  bool
	generateCompose bool
	generateNginx   bool
	appName         string
	queueWorkers    int
	observability   bool
)

func init() {
	scaffoldCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Interactive mode with prompts")
	scaffoldCmd.Flags().StringVarP(&outputDir, "output", "o", ".", "Output directory for generated files")
	scaffoldCmd.Flags().BoolVar(&generateDocker, "dockerfile", false, "Generate Dockerfile")
	scaffoldCmd.Flags().BoolVar(&generateCompose, "docker-compose", false, "Generate docker-compose.yml")
	scaffoldCmd.Flags().BoolVar(&generateNginx, "nginx", false, "Generate nginx.conf (with upstream load balancing for Node.js)")
	scaffoldCmd.Flags().StringVar(&appName, "app-name", "my-app", "Application name")
	scaffoldCmd.Flags().IntVar(&queueWorkers, "queue-workers", 3, "Number of queue workers")
	scaffoldCmd.Flags().BoolVar(&observability, "observability", false, "Enable observability stack (tracing, metrics, API)")
}

func runScaffold(cmd *cobra.Command, args []string) {
	// Determine preset
	var preset scaffold.Preset
	if len(args) > 0 {
		preset = scaffold.Preset(args[0])
	} else if interactive {
		preset = promptForPreset()
	} else {
		fmt.Fprintln(os.Stderr, "Error: preset required (or use --interactive)")
		fmt.Fprintf(os.Stderr, "\nAvailable presets: %s\n", strings.Join(scaffold.ValidPresets(), ", "))
		os.Exit(1)
	}

	// Validate preset
	validPreset := false
	for _, valid := range scaffold.ValidPresets() {
		if string(preset) == valid {
			validPreset = true
			break
		}
	}
	if !validPreset {
		fmt.Fprintf(os.Stderr, "Error: invalid preset '%s'\n", preset)
		fmt.Fprintf(os.Stderr, "\nAvailable presets: %s\n", strings.Join(scaffold.ValidPresets(), ", "))
		os.Exit(1)
	}

	// Create generator
	gen := scaffold.NewGenerator(preset, outputDir)

	// Interactive configuration
	if interactive {
		configureInteractive(gen)
	} else {
		// Apply CLI flags
		gen.SetAppName(appName)
		gen.SetQueueWorkers(queueWorkers)
	}

	// Apply observability stack if requested
	if observability {
		gen.EnableFeature("tracing", true)
		gen.EnableFeature("metrics", true)
		gen.EnableFeature("api", true)
		gen.SetLogLevel("warn")
	}

	// Display configuration summary
	fmt.Fprintf(os.Stderr, "\n📦 PHPeek PM Scaffold Generator\n\n")
	fmt.Fprintf(os.Stderr, "Preset:        %s\n", preset)
	fmt.Fprintf(os.Stderr, "App Name:      %s\n", gen.GetConfig().AppName)
	fmt.Fprintf(os.Stderr, "Output Dir:    %s\n", outputDir)
	if observability {
		fmt.Fprintf(os.Stderr, "Observability: enabled (tracing + metrics + API)\n")
	}

	// Determine files to generate
	files := []string{"config"}
	if generateCompose {
		files = append(files, "docker-compose")
	}
	if generateDocker {
		files = append(files, "dockerfile")
	}
	if generateNginx {
		files = append(files, "nginx")
	}

	fmt.Fprintf(os.Stderr, "\nGenerating files:\n")
	for _, file := range files {
		fmt.Fprintf(os.Stderr, "  - %s\n", getFilename(file))
	}

	// Confirm if output directory has existing files
	if !confirmOverwrite(outputDir, files) {
		fmt.Fprintln(os.Stderr, "\n❌ Scaffold cancelled")
		os.Exit(1)
	}

	// Generate files
	if err := gen.Generate(files); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Failed to generate files: %v\n", err)
		os.Exit(1)
	}

	// Success message
	fmt.Fprintf(os.Stderr, "\n✅ Scaffold complete!\n\n")
	fmt.Fprintf(os.Stderr, "Generated files in: %s\n", outputDir)
	fmt.Fprintf(os.Stderr, "\nNext steps:\n")
	fmt.Fprintf(os.Stderr, "  1. Review phpeek-pm.yaml\n")
	fmt.Fprintf(os.Stderr, "  2. Customize for your needs\n")
	if generateDocker || generateCompose {
		fmt.Fprintf(os.Stderr, "  3. Build and run: docker-compose up\n")
	} else {
		fmt.Fprintf(os.Stderr, "  3. Run: phpeek-pm serve\n")
	}
}

func promptForPreset() scaffold.Preset {
	fmt.Fprintln(os.Stderr, "\n📦 Select a preset:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  PHP:")
	fmt.Fprintln(os.Stderr, "    1. laravel    - Laravel with Horizon, Queue, Scheduler")
	fmt.Fprintln(os.Stderr, "    2. symfony    - Symfony with Messenger")
	fmt.Fprintln(os.Stderr, "    3. php        - Vanilla PHP with Nginx")
	fmt.Fprintln(os.Stderr, "    4. wordpress  - WordPress with WP-CLI cron")
	fmt.Fprintln(os.Stderr, "    5. magento    - Magento 2 with queue, cron, indexer")
	fmt.Fprintln(os.Stderr, "    6. drupal     - Drupal with Drush cron")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Node.js:")
	fmt.Fprintln(os.Stderr, "    7. nextjs     - Next.js standalone")
	fmt.Fprintln(os.Stderr, "    8. nuxt       - Nuxt 3 with Nitro")
	fmt.Fprintln(os.Stderr, "    9. nodejs     - Node.js with workers")

	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "\nChoice (1-9): ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		return scaffold.PresetLaravel
	case "2":
		return scaffold.PresetSymfony
	case "3":
		return scaffold.PresetPHP
	case "4":
		return scaffold.PresetWordPress
	case "5":
		return scaffold.PresetMagento
	case "6":
		return scaffold.PresetDrupal
	case "7":
		return scaffold.PresetNextJS
	case "8":
		return scaffold.PresetNuxt
	case "9":
		return scaffold.PresetNodeJS
	default:
		fmt.Fprintln(os.Stderr, "\nInvalid choice, using laravel")
		return scaffold.PresetLaravel
	}
}

func configureInteractive(gen *scaffold.Generator) {
	reader := bufio.NewReader(os.Stdin)

	// App name
	fmt.Fprint(os.Stderr, "\nApplication name [my-app]: ")
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		gen.SetAppName(strings.TrimSpace(input))
	}

	// Log level
	fmt.Fprint(os.Stderr, "Log level (debug/info/warn/error) [info]: ")
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		gen.SetLogLevel(strings.TrimSpace(input))
	}

	cfg := gen.GetConfig()

	// Framework-specific questions
	if cfg.Framework == "laravel" {
		fmt.Fprint(os.Stderr, "\nNumber of queue workers [3]: ")
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			if count, err := strconv.Atoi(strings.TrimSpace(input)); err == nil {
				gen.SetQueueWorkers(count)
			}
		}

		fmt.Fprint(os.Stderr, "Queue connection (redis/database/sqs) [redis]: ")
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			gen.SetQueueConnection(strings.TrimSpace(input))
		}
	}

	// Features
	fmt.Fprintln(os.Stderr, "\n🔧 Features (y/n):")

	// Observability stack (convenience option)
	if promptYesNo("Enable full observability stack? (tracing + metrics + API)", false) {
		gen.EnableFeature("tracing", true)
		gen.EnableFeature("metrics", true)
		gen.EnableFeature("api", true)
		gen.SetLogLevel("warn")
		observability = true
	} else {
		// Individual feature toggles
		if promptYesNo("Enable Prometheus metrics?", cfg.EnableMetrics) {
			gen.EnableFeature("metrics", true)
		}

		if promptYesNo("Enable Management API?", cfg.EnableAPI) {
			gen.EnableFeature("api", true)
		}

		if promptYesNo("Enable distributed tracing?", cfg.EnableTracing) {
			gen.EnableFeature("tracing", true)
		}
	}

	// Docker files
	fmt.Fprintln(os.Stderr, "\n🐳 Docker files:")
	generateCompose = promptYesNo("Generate docker-compose.yml?", observability) // Default yes if observability enabled
	generateDocker = promptYesNo("Generate Dockerfile?", false)
	generateNginx = promptYesNo("Generate nginx.conf (with load balancing)?", cfg.EnableNginx)
}

func promptYesNo(prompt string, defaultVal bool) bool {
	reader := bufio.NewReader(os.Stdin)
	defaultStr := "n"
	if defaultVal {
		defaultStr = "y"
	}

	fmt.Fprintf(os.Stderr, "%s [%s]: ", prompt, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))

	if input == "" {
		return defaultVal
	}
	return input == "y" || input == "yes"
}

func confirmOverwrite(dir string, files []string) bool {
	existing := []string{}
	for _, file := range files {
		path := filepath.Join(dir, getFilename(file))
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, getFilename(file))
		}
	}

	if len(existing) == 0 {
		return true
	}

	fmt.Fprintf(os.Stderr, "\n⚠️  The following files already exist:\n")
	for _, file := range existing {
		fmt.Fprintf(os.Stderr, "  - %s\n", file)
	}

	return promptYesNo("\nOverwrite existing files?", false)
}

func getFilename(file string) string {
	switch file {
	case "config":
		return "phpeek-pm.yaml"
	case "docker-compose":
		return "docker-compose.yml"
	case "dockerfile":
		return "Dockerfile"
	case "nginx":
		return "nginx.conf"
	default:
		return file
	}
}
