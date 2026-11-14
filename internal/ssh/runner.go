package ssh

import (
	"errors"
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"elyms/internal/config"
)

// FinishedMsg is delivered to the program when the ssh session ends.
type FinishedMsg struct {
	Alias string
	Err   error
}

// Describe turns the end of a session into a status line. ssh exits 255 when
// the connection itself failed; any other exit code is the remote command's,
// not an ssh failure. The reason for a 255 was printed on the normal screen,
// which the TUI covers, so the hint says how to see it.
func Describe(msg FinishedMsg) (text string, isErr bool) {
	if msg.Err == nil {
		return "Session with " + msg.Alias + " ended", false
	}
	var exit *exec.ExitError
	if errors.As(msg.Err, &exit) {
		if exit.ExitCode() == 255 {
			return fmt.Sprintf("Could not connect to %s. Run `ssh %s` in a shell to see why.", msg.Alias, msg.Alias), true
		}
		return fmt.Sprintf("Session with %s ended (exit %d)", msg.Alias, exit.ExitCode()), false
	}
	return fmt.Sprintf("ssh %s: %v", msg.Alias, msg.Err), true
}

func Available() bool {
	_, err := exec.LookPath("ssh")
	return err == nil
}

// Command builds `ssh -- <alias>`. The "--" plus config.ValidateTarget keep
// an alias from ever being read as an ssh option.
func Command(alias string) (*exec.Cmd, error) {
	if err := config.ValidateTarget(alias); err != nil {
		return nil, err
	}
	path, err := exec.LookPath("ssh")
	if err != nil {
		return nil, fmt.Errorf("ssh executable not found on PATH (enable the Windows OpenSSH Client): %w", err)
	}
	return exec.Command(path, "--", alias), nil
}

// Connect suspends the TUI, hands the terminal to ssh, and returns a
// FinishedMsg when it exits.
func Connect(alias string) tea.Cmd {
	cmd, err := Command(alias)
	if err != nil {
		return func() tea.Msg { return FinishedMsg{Alias: alias, Err: err} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return FinishedMsg{Alias: alias, Err: err}
	})
}
