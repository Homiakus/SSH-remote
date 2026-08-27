package ssh

import "testing"

func TestParseRemotePlatformOutput(t *testing.T) {
	goos, arch, err := parseRemotePlatformOutput("Linux\nx86_64\n")
	if err != nil {
		t.Fatalf("parseRemotePlatformOutput() error = %v", err)
	}
	if goos != "Linux" || arch != "x86_64" {
		t.Fatalf("parsed values = %q/%q, want Linux/x86_64", goos, arch)
	}
}

func TestNormalizeRemotePlatform(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		arch    string
		want    RemotePlatform
		wantErr bool
	}{
		{name: "amd64", goos: "Linux", arch: "x86_64", want: RemotePlatform{GOOS: "linux", GOARCH: "amd64"}},
		{name: "arm64", goos: "linux", arch: "aarch64", want: RemotePlatform{GOOS: "linux", GOARCH: "arm64"}},
		{name: "arm", goos: "linux", arch: "armv7l", want: RemotePlatform{GOOS: "linux", GOARCH: "arm"}},
		{name: "386", goos: "linux", arch: "i686", want: RemotePlatform{GOOS: "linux", GOARCH: "386"}},
		{name: "unsupported os", goos: "Darwin", arch: "arm64", wantErr: true},
		{name: "unsupported arch", goos: "linux", arch: "sparc64", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRemotePlatform(tt.goos, tt.arch)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRemotePlatform() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRemotePlatform() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
