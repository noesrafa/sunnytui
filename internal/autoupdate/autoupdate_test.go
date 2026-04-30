package autoupdate

import (
	"path/filepath"
	"testing"
)

func TestInstalledViaBrew(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/sunnytui/0.13.6/bin/sunnytui", true},
		{"/usr/local/Cellar/sunnytui/0.13.6/bin/sunnytui", true},
		{"/Users/me/custom-brew/Cellar/sunnytui/0.13.6/bin/sunnytui", true},
		{"/Users/mac/sunnytui/bin/sunnytui", false},
		{"/usr/local/bin/sunnytui", false}, // bare symlink, evalSymlinks resolves it
		{"/tmp/go-build-123/sunnytui", false},
		{"", false},
	}
	for _, c := range cases {
		if got := installedViaBrew(c.path); got != c.want {
			t.Errorf("installedViaBrew(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRecentlyChecked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stamp")

	if recentlyChecked(path) {
		t.Fatal("missing file should report not recent")
	}
	writeTimestamp(path)
	if !recentlyChecked(path) {
		t.Fatal("just-written stamp should report recent")
	}
}
