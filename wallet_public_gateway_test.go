package main

import (
	"os"
	"strings"
	"testing"
)

func TestWalletPublicGatewayIgnoresUnsafeSavedRPC(t *testing.T) {
	js, err := os.ReadFile("ui/msc_wallet.js")
	if err != nil {
		t.Fatalf("read wallet js: %v", err)
	}
	body := string(js)
	required := []string{
		"const isPublicGatewayPage = () =>",
		"const shouldKeepSavedRPCForCurrentPage = (rpc) =>",
		`if (isHTTPSPage() && url.protocol !== "https:") return false;`,
		"if (isPublicGatewayPage()) return window.location.origin;",
	}
	for _, want := range required {
		if !strings.Contains(body, want) {
			t.Fatalf("wallet public gateway guard missing %q", want)
		}
	}

	html, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatalf("read wallet html: %v", err)
	}
	if !strings.Contains(string(html), "msc_wallet.js?v=20260527a") {
		t.Fatalf("wallet js cache buster was not updated")
	}
}
