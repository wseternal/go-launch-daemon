package supervisor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"syscall"

	"launchd/internal/registry"
)

// spawnConfig holds the configuration for spawning a process.
type spawnConfig struct {
	APIAddr string
	LogDir  string
}

// spawn starts a process for the given service spec.
// It returns the exec.Cmd and the session token injected into the environment.
func spawn(spec registry.Spec, cfg spawnConfig) (*exec.Cmd, string, error) {
	if len(spec.Argv) == 0 {
		return nil, "", fmt.Errorf("empty argv for service %q", spec.Name)
	}

	token := generateSessionToken()

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)

	// Build environment: inherit parent env + injected vars.
	env := make([]string, 0, len(spec.Env)+4)
	env = append(env, spec.Env...)
	env = append(env,
		"LAUNCHD_API_ADDR="+cfg.APIAddr,
		"LAUNCHD_SERVICE_NAME="+spec.Name,
		"LAUNCHD_SESSION_TOKEN="+token,
	)
	cmd.Env = env

	// Process group isolation.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	return cmd, token, nil
}

// generateSessionToken creates a 32-byte hex session token.
func generateSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}
