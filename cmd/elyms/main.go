package main

import (
	"fmt"
	"os"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/config"
	"elyms/internal/ssh"
	"elyms/internal/ui"
	"elyms/internal/workspace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "elyms:", err)
		os.Exit(1)
	}
}

func run() error {
	sshDir, err := config.GetSSHDir()
	if err != nil {
		return fmt.Errorf("cannot locate home directory: %w", err)
	}
	cfgPath, err := config.GetConfigPath()
	if err != nil {
		return err
	}
	// Workspace metadata lives beside the app's data, never in the SSH
	// directory: a failure to resolve it only disables workspaces.
	wsPath, err := workspace.DefaultPath()
	if err != nil {
		wsPath = ""
	}

	model, err := ui.New(ui.Options{
		ConfigPath:    cfgPath,
		SSHDir:        sshDir,
		WorkspacePath: wsPath,
		Connect:       ssh.Connect,
		Copy:          clipboard.WriteAll,
		SSHAvailable:  ssh.Available(),
	})
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", cfgPath, err)
	}

	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}
