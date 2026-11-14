package ssh

import (
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCommandPassesAliasAfterOptionTerminator(t *testing.T) {
	cmd, err := Command("web")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmd.Args[1:], []string{"--", "web"}; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestCommandRejectsUnsafeAliases(t *testing.T) {
	for _, a := range []string{"", "-oProxyCommand=calc", "a b", "web*", "!x"} {
		if _, err := Command(a); err == nil {
			t.Errorf("alias %q should be rejected", a)
		}
	}
}

func TestCommandFailsClearlyWhenSSHMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Command("web")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ssh") {
		t.Errorf("want an error mentioning ssh, got %v", err)
	}
}

func TestAvailableIsFalseWithEmptyPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Errorf("Available must be false with empty PATH")
	}
}

func finish(t *testing.T, err error) FinishedMsg {
	t.Helper()
	return FinishedMsg{Alias: "web", Err: err}
}

func exitErr(t *testing.T, code int) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", fmt.Sprintf("exit %d", code))
	} else {
		cmd = exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
	}
	err := cmd.Run()
	if err == nil {
		t.Fatalf("exit %d produced no error", code)
	}
	return err
}

func TestDescribeCleanExitIsNotAnError(t *testing.T) {
	text, isErr := Describe(finish(t, nil))
	if isErr || !strings.Contains(text, "web") {
		t.Errorf("got %q err=%v", text, isErr)
	}
}

func TestDescribeExit255MeansConnectionFailedAndHintsHowToSeeWhy(t *testing.T) {
	text, isErr := Describe(finish(t, exitErr(t, 255)))
	if !isErr || !strings.Contains(text, "web") || !strings.Contains(text, "ssh web") {
		t.Errorf("got %q err=%v", text, isErr)
	}
}

func TestDescribeRemoteNonZeroExitIsNotShownAsFailure(t *testing.T) {
	text, isErr := Describe(finish(t, exitErr(t, 3)))
	if isErr || !strings.Contains(text, "3") {
		t.Errorf("a remote command's exit code is not an ssh failure: %q err=%v", text, isErr)
	}
}

func TestDescribeOtherErrorsAreShownAsErrors(t *testing.T) {
	text, isErr := Describe(finish(t, errors.New("boom")))
	if !isErr || !strings.Contains(text, "boom") {
		t.Errorf("got %q err=%v", text, isErr)
	}
}

func TestCommandStillAllowsExistingAliasesWithColonOrDot(t *testing.T) {
	for _, a := range []string{"prod:22", "host.example.com", "10.0.0.1"} {
		if _, err := Command(a); err != nil {
			t.Errorf("alias %q must stay connectable: %v", a, err)
		}
	}
}
