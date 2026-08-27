package nginx

import (
	"strings"
	"testing"
)

func TestRenderProxyTLSConfig(t *testing.T) {
	t.Parallel()

	content, err := RenderSite(SiteConfig{
		Name:        "demo",
		Domain:      "demo.test",
		Mode:        ModeProxy,
		Root:        "/srv/demo/current",
		ProxyPass:   "http://127.0.0.1:3000",
		EnableTLS:   true,
		TLSCertPath: "/etc/letsencrypt/live/demo/fullchain.pem",
		TLSKeyPath:  "/etc/letsencrypt/live/demo/privkey.pem",
		Webroot:     "/srv/demo/shared/acme",
	})
	if err != nil {
		t.Fatalf("render config: %v", err)
	}

	for _, fragment := range []string{
		"proxy_pass http://127.0.0.1:3000;",
		"ssl_certificate /etc/letsencrypt/live/demo/fullchain.pem;",
		"location /.well-known/acme-challenge/",
		"server_name demo.test;",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expected config to contain %q\n%s", fragment, content)
		}
	}
}

func TestRenderSiteRejectsDirectiveInjection(t *testing.T) {
	t.Parallel()

	_, err := RenderSite(SiteConfig{
		Name:   "demo",
		Domain: "demo.test;\nserver_name evil.test",
		Mode:   ModeStatic,
		Root:   "/srv/demo/current",
	})
	if err == nil {
		t.Fatal("expected unsafe domain error")
	}
}

func TestRenderSiteQuotesWhitespacePaths(t *testing.T) {
	t.Parallel()

	content, err := RenderSite(SiteConfig{
		Name:   "demo",
		Domain: "demo.test",
		Mode:   ModeStatic,
		Root:   "/srv/demo with space/current",
	})
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if !strings.Contains(content, `root "/srv/demo with space/current";`) {
		t.Fatalf("expected quoted root path:\n%s", content)
	}
}
