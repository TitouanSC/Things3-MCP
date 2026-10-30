package local

import (
	"context"
	"fmt"
	"os/exec"
)

// Runner executes an external command. It is injected so tests can record
// commands (or simulate Things) instead of driving the real app.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs commands with os/exec, never through a shell: arguments
// are passed as-is, so their content cannot be interpreted as shell syntax.
type ExecRunner struct{}

// Run implements Runner. The error deliberately omits the arguments and the
// command output, which may contain the auth token or task content.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// trashScript moves a to-do or project to the Things trash. In the Things
// AppleScript dictionary, "delete" moves the item to the Trash (it remains
// restorable); "empty trash" is never used. The ID arrives through argv
// rather than being spliced into the script, which rules out AppleScript
// injection.
const trashScript = `on run argv
	set theID to item 1 of argv
	tell application id "com.culturedcode.ThingsMac"
		if (item 2 of argv) is "project" then
			delete project id theID
		else
			delete to do id theID
		end if
	end tell
end run`
