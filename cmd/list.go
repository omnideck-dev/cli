package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"instances"},
	Short:   "List Omnideck installations",
	RunE:    runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(_ *cobra.Command, _ []string) error {
	instances, err := config.ListInstances()
	if err != nil {
		return fmt.Errorf("reading saved Omnideck installations: %w", err)
	}
	instances = withLoadedInstance(instances, LoadedConfig, ConfigPath)
	if len(instances) == 0 {
		fmt.Println("No Omnideck installations are set up yet.")
		fmt.Println("Run `omnideck setup` to create the first one.")
		return nil
	}

	var eng engine.Engine
	if candidate, detectErr := engineFromConfig(configuredEngineName(LoadedConfig, instances)); detectErr == nil {
		eng = candidate
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tBROWSER ADDRESS")
	for _, instance := range instances {
		status := "runtime not ready"
		if eng != nil && instance.Config != nil {
			status, err = eng.ContainerStatus(instance.Config.ContainerName)
			if err != nil || status == "" {
				status = "container not found"
			}
		}
		port := ""
		if instance.Config != nil {
			port = "http://localhost:" + instance.Config.WebUIPortOrDefault()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", instance.Name, status, port)
	}
	return w.Flush()
}
