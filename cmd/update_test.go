package cmd

import "testing"

func TestFindAssetMatchesCurrentPlatform(t *testing.T) {
	assets := []ghAsset{
		{Name: "unity-scanner-linux-amd64", BrowserDownloadURL: "linux"},
		{Name: "unity-scanner-windows-amd64.exe", BrowserDownloadURL: "windows"},
	}

	asset := findAsset(assets)
	if asset == nil {
		t.Fatal("expected current platform asset")
	}
	if asset.BrowserDownloadURL == "" {
		t.Fatalf("expected download URL, got %+v", asset)
	}
}
