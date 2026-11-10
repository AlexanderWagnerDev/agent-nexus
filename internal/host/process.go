package host

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// detectActiveElsewhere performs a best-effort process scan. Agent CLIs do
// not expose a portable "which session is open in a terminal" API, so this is
// intentionally heuristic: a command line containing the agent executable and
// the native session ID counts as active outside AgentNexus.
func detectActiveElsewhere(ctx context.Context, sessions []protocol.Session) map[string]bool {
	if len(sessions) == 0 {
		return nil
	}
	lines := processCommandLines(ctx)
	if len(lines) == 0 {
		return nil
	}
	return matchActiveSessions(lines, sessions)
}

func matchActiveSessions(lines []string, sessions []protocol.Session) map[string]bool {
	active := map[string]bool{}
	for _, s := range sessions {
		if s.NativeID == "" {
			continue
		}
		needle := strings.ToLower(s.NativeID)
		agent := strings.ToLower(s.Agent)
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, needle) && strings.Contains(lower, agent) {
				active[s.Agent+":"+s.NativeID] = true
				break
			}
		}
	}
	return active
}

func processCommandLines(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"Get-CimInstance Win32_Process | ForEach-Object { $_.CommandLine }")
	} else {
		cmd = exec.CommandContext(ctx, "ps", "-eo", "args=")
	}
	hideProcessWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return strings.Split(string(out), "\n")
}
