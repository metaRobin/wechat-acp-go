package acp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

// AgentProcess represents a running ACP agent subprocess.
type AgentProcess struct {
	Cmd        *exec.Cmd
	Connection *sdk.ClientSideConnection
	SessionID  sdk.SessionId
	OnExit     func() // called when process exits
}

// SpawnOpts configures agent subprocess startup.
type SpawnOpts struct {
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
	Client  sdk.Client
	Logger  *slog.Logger
}

// Spawn starts an ACP agent subprocess and initializes the protocol.
func Spawn(ctx context.Context, opts SpawnOpts) (*AgentProcess, error) {
	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)
	cmd.Dir = opts.Cwd
	cmd.Stderr = os.Stderr
	cmd.Env = mergeEnv(os.Environ(), opts.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	// Create ACP connection over stdio
	conn := sdk.NewClientSideConnection(opts.Client, stdin, stdout)
	conn.SetLogger(opts.Logger)

	// Initialize protocol
	_, err = conn.Initialize(ctx, sdk.InitializeRequest{
		ProtocolVersion: sdk.ProtocolVersionNumber,
		ClientInfo: &sdk.Implementation{
			Name:    "wechat-router-go",
			Version: "0.1.0",
		},
		ClientCapabilities: sdk.ClientCapabilities{
			Fs: sdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
		},
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Create session
	sessResp, err := conn.NewSession(ctx, sdk.NewSessionRequest{
		Cwd:        opts.Cwd,
		McpServers: []sdk.McpServer{},
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("new session: %w", err)
	}

	ap := &AgentProcess{
		Cmd:        cmd,
		Connection: conn,
		SessionID:  sessResp.SessionId,
	}

	// Watch for process exit
	go func() {
		_ = cmd.Wait()
		if ap.OnExit != nil {
			ap.OnExit()
		}
	}()

	return ap, nil
}

// Kill terminates the agent process gracefully (SIGTERM, then SIGKILL after 5s).
func (ap *AgentProcess) Kill() {
	if ap.Cmd.Process == nil {
		return
	}
	_ = ap.Cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		time.Sleep(5 * time.Second)
		if ap.Cmd.ProcessState == nil || !ap.Cmd.ProcessState.Exited() {
			_ = ap.Cmd.Process.Kill()
		}
	}()
}

func mergeEnv(base []string, extra map[string]string) []string {
	env := make([]string, len(base), len(base)+len(extra))
	copy(env, base)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func ptr[T any](v T) *T { return &v }
