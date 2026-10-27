package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryFoldersOnByDefault(t *testing.T) {
	var c Config
	if !c.HistoryFoldersOn() {
		t.Error("history folders should be on when unset")
	}
	if err := json.Unmarshal([]byte(`{"historyFolders":false}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.HistoryFoldersOn() {
		t.Error("historyFolders: false did not turn it off")
	}
}

func TestAddFolder(t *testing.T) {
	c := Default()
	dir := t.TempDir()
	app := filepath.Join(dir, "My App")

	f, isNew := c.AddFolder(app, "")
	if !isNew || f.ID != "my-app" || f.Path != app || f.Server.Capacity != DefaultCapacity {
		t.Fatalf("added %+v %v", f, isNew)
	}
	// The same folder again, written differently, is not added twice.
	again, isNew := c.AddFolder(strings.ToUpper(app)+string(filepath.Separator), "")
	if isNew || again.ID != "my-app" || len(c.Folders) != 1 {
		t.Errorf("again = %+v %v, folders %d", again, isNew, len(c.Folders))
	}
	// Another folder with the same name gets its own id.
	other, _ := c.AddFolder(filepath.Join(dir, "x", "My App"), "")
	if other.ID != "my-app-2" {
		t.Errorf("other id = %q", other.ID)
	}
}
