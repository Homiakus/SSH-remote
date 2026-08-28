package system

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  map[string]string
}

type Result struct {
	Stdout string
	Stderr string
}

type Runner interface {
	Run(context.Context, Command) (Result, error)
	LookPath(string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	execCmd := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	if cmd.Dir != "" {
		execCmd.Dir = cmd.Dir
	}
	if len(cmd.Env) > 0 {
		env := execCmd.Environ()
		for key, value := range cmd.Env {
			env = append(env, key+"="+value)
		}
		execCmd.Env = env
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	if err := execCmd.Run(); err != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("%s %s: %w: %s",
			cmd.Name, strings.Join(cmd.Args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return Result{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (ExecRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

type FakeRunner struct {
	Calls           []Command
	Handler         func(Command) (Result, error)
	HandlerContext  func(context.Context, Command) (Result, error)
	Paths           map[string]string
	LookPathHandler func(string) (string, error)
}

func (f *FakeRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	f.Calls = append(f.Calls, cmd)
	if f.HandlerContext != nil {
		return f.HandlerContext(ctx, cmd)
	}
	if f.Handler != nil {
		return f.Handler(cmd)
	}
	return Result{}, nil
}

func (f *FakeRunner) LookPath(name string) (string, error) {
	if f.LookPathHandler != nil {
		return f.LookPathHandler(name)
	}
	if f.Paths != nil {
		if value, ok := f.Paths[name]; ok {
			return value, nil
		}
	}
	return "/usr/bin/" + name, nil
}
