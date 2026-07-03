package cmd

import (
	"fmt"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current status of Omnideck",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, _ []string) error {
	if err := requireConfigMulti(); err != nil {
		return err
	}
	cfg := LoadedConfig

	// Detect engine.
	var eng engine.Engine
	switch cfg.Engine {
	case "docker":
		eng = &engine.DockerEngine{}
	case "podman":
		eng = &engine.PodmanEngine{}
	default:
		var err error
		eng, err = engine.Detect()
		if err != nil {
			return err
		}
	}

	// Gather status concurrently.
	type result struct {
		containerStatus   string
		containerErr      error
		ollamaOK          bool
		ollamaHost        string
		homeVolumeExists  bool
		stateVolumeExists bool
	}

	var res result
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		s, err := eng.ContainerStatus(cfg.ContainerName)
		res.containerStatus = s
		res.containerErr = err
	}()
	go func() {
		defer wg.Done()
		res.ollamaOK, res.ollamaHost = checks.CheckOllama()
	}()
	go func() {
		defer wg.Done()
		ok, _ := eng.VolumeExists(cfg.HomeVolumeName())
		res.homeVolumeExists = ok
		ok, _ = eng.VolumeExists(cfg.StateVolumeName())
		res.stateVolumeExists = ok
	}()

	wg.Wait()

	// Render table.
	label := lipgloss.NewStyle().Width(16).Foreground(lipgloss.AdaptiveColor{Light: "#555", Dark: "#999"})
	value := lipgloss.NewStyle()
	sep := styles.Dim.Render("─────────────────────────────")

	containerDot := styles.RedBullet()
	running := false
	if res.containerStatus == "running" {
		containerDot = styles.GreenBullet()
		running = true
	}

	ollamaStatus := styles.Error.Render("✗ not reachable")
	if res.ollamaOK {
		ollamaStatus = styles.Success.Render("✓ reachable at " + res.ollamaHost)
	}

	homeVolumeStatus := styles.Error.Render("✗ missing")
	if res.homeVolumeExists {
		homeVolumeStatus = styles.Success.Render("✓ exists")
	}
	stateVolumeStatus := styles.Error.Render("✗ missing")
	if res.stateVolumeExists {
		stateVolumeStatus = styles.Success.Render("✓ exists")
	}

	fmt.Println()
	fmt.Println(styles.Title.Render("  Omnideck Status"))
	fmt.Println("  " + sep)
	fmt.Printf("  %s %s %s\n", label.Render("Container"), value.Render(cfg.ContainerName), containerDot+" "+res.containerStatus)
	fmt.Printf("  %s %s\n", label.Render("Image"), value.Render(cfg.Image))
	fmt.Printf("  %s %s\n", label.Render("Engine"), value.Render(cfg.Engine))
	fmt.Printf("  %s %s  %s\n", label.Render("Home volume"), value.Render(cfg.HomeVolumeName()), homeVolumeStatus)
	fmt.Printf("  %s %s  %s\n", label.Render("State volume"), value.Render(cfg.StateVolumeName()), stateVolumeStatus)
	fmt.Printf("  %s %s\n", label.Render("Ollama"), ollamaStatus)
	fmt.Printf("  %s %s\n", label.Render("Web UI"), value.Render("http://localhost:"+cfg.WebUIPortOrDefault()))
	fmt.Println()

	if !running {
		// Exit 1 (Unix convention for stopped services) without printing
		// "Error: ..." — the status table above already shows the state.
		return errAborted
	}
	return nil
}
