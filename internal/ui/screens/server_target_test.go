package screens

import (
	"reflect"
	"testing"

	"sshpilot/internal/config"
)

func TestFormatServerTarget(t *testing.T) {
	tests := []struct {
		name   string
		server config.ServerConfig
		want   string
	}{
		{
			name:   "empty port omits suffix",
			server: config.ServerConfig{User: "root", Host: "185.72.144.39"},
			want:   "root@185.72.144.39",
		},
		{
			name:   "explicit port 22 stays visible",
			server: config.ServerConfig{User: "root", Host: "185.72.144.39", Port: "22"},
			want:   "root@185.72.144.39:22",
		},
		{
			name:   "custom port stays visible",
			server: config.ServerConfig{User: "root", Host: "185.72.144.39", Port: "2222"},
			want:   "root@185.72.144.39:2222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatServerTarget(tt.server); got != tt.want {
				t.Fatalf("formatServerTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSSHArgs(t *testing.T) {
	tests := []struct {
		name   string
		server config.ServerConfig
		want   []string
	}{
		{
			name:   "empty port uses default ssh behavior",
			server: config.ServerConfig{User: "root", Host: "185.72.144.39"},
			want:   []string{"root@185.72.144.39"},
		},
		{
			name:   "explicit port 22 is passed to ssh",
			server: config.ServerConfig{User: "root", Host: "185.72.144.39", Port: "22"},
			want:   []string{"-p", "22", "root@185.72.144.39"},
		},
		{
			name:   "custom port is passed to ssh",
			server: config.ServerConfig{User: "root", Host: "185.72.144.39", Port: "2222"},
			want:   []string{"-p", "2222", "root@185.72.144.39"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSSHArgs(tt.server); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildSSHArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
