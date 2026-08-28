package system

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFakeRunnerContextHandlerCapturesOutputAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := Command{Name: "fixture", Args: []string{"a"}, Dir: "/tmp", Env: map[string]string{"A": "B"}}
	fake := &FakeRunner{
		HandlerContext: func(gotCtx context.Context, got Command) (Result, error) {
			if !reflect.DeepEqual(got, cmd) {
				t.Fatalf("command = %#v, want %#v", got, cmd)
			}
			if !errors.Is(gotCtx.Err(), context.Canceled) {
				t.Fatalf("context error = %v, want canceled", gotCtx.Err())
			}
			return Result{Stdout: "out", Stderr: "err"}, context.Canceled
		},
	}

	result, err := fake.Run(ctx, cmd)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want canceled", err)
	}
	if result.Stdout != "out" || result.Stderr != "err" {
		t.Fatalf("result = %#v", result)
	}
	if len(fake.Calls) != 1 || !reflect.DeepEqual(fake.Calls[0], cmd) {
		t.Fatalf("calls = %#v", fake.Calls)
	}
}

func TestFakeRunnerLegacyHandlerAndDefaults(t *testing.T) {
	injected := errors.New("legacy")
	fake := &FakeRunner{Handler: func(cmd Command) (Result, error) {
		if cmd.Name != "legacy" {
			t.Fatalf("command = %q", cmd.Name)
		}
		return Result{Stdout: "legacy-out"}, injected
	}}
	result, err := fake.Run(context.Background(), Command{Name: "legacy"})
	if !errors.Is(err, injected) || result.Stdout != "legacy-out" {
		t.Fatalf("legacy result = %#v, %v", result, err)
	}

	plain := &FakeRunner{}
	if result, err := plain.Run(context.Background(), Command{Name: "noop"}); err != nil || result != (Result{}) {
		t.Fatalf("default result = %#v, %v", result, err)
	}
}

func TestFakeRunnerLookPathModes(t *testing.T) {
	injected := errors.New("missing")
	withHandler := &FakeRunner{LookPathHandler: func(name string) (string, error) {
		if name != "tool" {
			t.Fatalf("name = %q", name)
		}
		return "", injected
	}}
	if _, err := withHandler.LookPath("tool"); !errors.Is(err, injected) {
		t.Fatalf("handler LookPath error = %v", err)
	}

	withMap := &FakeRunner{Paths: map[string]string{"tool": "/custom/tool"}}
	if got, err := withMap.LookPath("tool"); err != nil || got != "/custom/tool" {
		t.Fatalf("mapped LookPath = %q, %v", got, err)
	}
	if got, err := withMap.LookPath("other"); err != nil || got != "/usr/bin/other" {
		t.Fatalf("fallback LookPath = %q, %v", got, err)
	}
	if got, err := (&FakeRunner{}).LookPath("plain"); err != nil || got != "/usr/bin/plain" {
		t.Fatalf("default LookPath = %q, %v", got, err)
	}
}
