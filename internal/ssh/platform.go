package ssh

import (
	"fmt"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

type RemotePlatform struct {
	GOOS   string
	GOARCH string
}

// DetectRemotePlatform определяет целевую платформу сервера для кросс-компиляции.
func DetectRemotePlatform(client *gossh.Client) (RemotePlatform, error) {
	output, err := ExecuteCommand(client, "uname -s && uname -m")
	if err != nil {
		return RemotePlatform{}, fmt.Errorf("не удалось определить платформу сервера: %w", err)
	}

	goos, arch, err := parseRemotePlatformOutput(output)
	if err != nil {
		return RemotePlatform{}, err
	}

	return normalizeRemotePlatform(goos, arch)
}

func parseRemotePlatformOutput(output string) (string, string, error) {
	lines := strings.Fields(strings.TrimSpace(output))
	if len(lines) < 2 {
		return "", "", fmt.Errorf("не удалось разобрать ответ uname: %q", strings.TrimSpace(output))
	}

	return lines[0], lines[1], nil
}

func normalizeRemotePlatform(goos, arch string) (RemotePlatform, error) {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "linux":
	default:
		return RemotePlatform{}, fmt.Errorf("поддерживаются только Linux-серверы, получено: %s", strings.TrimSpace(goos))
	}

	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "amd64":
		return RemotePlatform{GOOS: "linux", GOARCH: "amd64"}, nil
	case "aarch64", "arm64":
		return RemotePlatform{GOOS: "linux", GOARCH: "arm64"}, nil
	case "armv7l", "armv6l", "arm":
		return RemotePlatform{GOOS: "linux", GOARCH: "arm"}, nil
	case "i386", "i686", "386":
		return RemotePlatform{GOOS: "linux", GOARCH: "386"}, nil
	default:
		return RemotePlatform{}, fmt.Errorf("неподдерживаемая архитектура сервера: %s", strings.TrimSpace(arch))
	}
}
