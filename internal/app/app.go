// Package app wires CLI options to sources and the read-only TUI.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hcie123/guard9s/internal/demo"
	"github.com/hcie123/guard9s/internal/model"
	"github.com/hcie123/guard9s/internal/report"
	"github.com/hcie123/guard9s/internal/ui"
	"github.com/spf13/cobra"
)

type demoSource struct{ scenario, namespace string }

func (d demoSource) Snapshot(context.Context) (model.Snapshot, error) {
	s := demo.Snapshot(d.scenario)
	s.Namespace = d.namespace
	return s, nil
}

// liveSource is wired in live.go. Keeping demo independent prevents credentials
// or network initialization on the offline path.
func NewCommand(version string) *cobra.Command {
	var demoMode bool
	var kubeconfig, contextName, namespace, scenario, node, output, redact string
	var timeout time.Duration
	cmd := &cobra.Command{Use: "guard9s", Short: "Read-only Kubernetes SRE diagnosis and maintenance TUI", Version: version, SilenceErrors: true, SilenceUsage: true, Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&demoMode, "demo", false, "Use synthetic offline data; never load kubeconfig")
	cmd.Flags().StringVar(&scenario, "demo-scenario", "mixed", "Demo scenario: mixed, healthy or scheduling")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to trusted kubeconfig (defaults to standard client-go loading rules)")
	cmd.Flags().StringVar(&contextName, "context", "", "Kubeconfig context")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "all", "Display namespace; analysis always reads all namespaces")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Initial cache synchronization timeout")
	cmd.Flags().StringVar(&node, "node", "", "Target node (required for report export)")
	cmd.Flags().StringVar(&output, "output", "tui", "Output format: tui, json or markdown; reports go to stdout")
	cmd.Flags().StringVar(&redact, "redact", "none", "Report redaction: none or basic (bare --redact means basic)")
	cmd.Flags().Lookup("redact").NoOptDefVal = "basic"
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if redact != "none" && redact != "basic" {
			return fmt.Errorf("--redact must be none or basic")
		}
		if output == "tui" && redact != "none" {
			return fmt.Errorf("--redact requires --output json or markdown")
		}
		if output != "tui" && output != "json" && output != "markdown" {
			return fmt.Errorf("--output must be tui, json or markdown")
		}
		if output != "tui" && node == "" {
			return fmt.Errorf("--node is required for report export")
		}
		if output == "tui" && node != "" {
			return fmt.Errorf("--node requires --output json or markdown")
		}
		if namespace == "" {
			namespace = "all"
		}
		if timeout <= 0 {
			return fmt.Errorf("--timeout must be positive")
		}
		if scenario != "mixed" && scenario != "healthy" && scenario != "scheduling" {
			return fmt.Errorf("--demo-scenario must be mixed, healthy or scheduling")
		}
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		var source ui.Source
		if demoMode {
			source = demoSource{scenario: scenario, namespace: namespace}
		} else {
			var err error
			source, err = liveSource(ctx, kubeconfig, contextName, namespace, timeout)
			if err != nil {
				return err
			}
		}
		if closer, ok := source.(interface{ Close() }); ok {
			defer closer.Close()
		}
		snapshot, err := source.Snapshot(ctx)
		if err != nil {
			return err
		}
		if output != "tui" {
			r, err := report.Build(ctx, snapshot, node, version, report.Options{Redact: redact})
			if err != nil {
				return err
			}
			if output == "json" {
				return report.WriteJSON(cmd.OutOrStdout(), r)
			}
			return report.WriteMarkdown(cmd.OutOrStdout(), r)
		}
		return ui.New(snapshot).Run(ctx, source)
	}
	return cmd
}
