package antivpn

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadEnabledEventAddons covers the per-addon enable/disable logic
// driven by /home/container/config/jka-addons.json. The runner only
// launches enabled addons whose "type" is "event"; scheduled addons
// and disabled addons are ignored.
func TestLoadEnabledEventAddons(t *testing.T) {
	tmp := t.TempDir()
	defaults := filepath.Join(tmp, "defaults")
	if err := os.MkdirAll(defaults, 0o755); err != nil {
		t.Fatalf("mkdir defaults: %v", err)
	}
	for _, name := range []string{"live-team-announcer.py", "chatlogger.py", "announcer.py"} {
		if err := os.WriteFile(filepath.Join(defaults, name), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cfgPath := filepath.Join(tmp, "jka-addons.json")
	cfg := `{
"addons": {
  "announcer":           {"enabled": true,  "order": 20, "type": "scheduled", "script": "announcer.py"},
  "live_team_announcer": {"enabled": true,  "order": 30, "type": "event",     "script": "live-team-announcer.py"},
  "chatlogger":          {"enabled": false, "order": 40, "type": "event",     "script": "chatlogger.py"}
}}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	specs, err := loadEnabledEventAddons(cfgPath, defaults)
	if err != nil {
		t.Fatalf("loadEnabledEventAddons: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected exactly one enabled event addon, got %d: %#v", len(specs), specs)
	}
	if specs[0].Name != "live_team_announcer" {
		t.Fatalf("expected live_team_announcer, got %q", specs[0].Name)
	}
	wantPath := filepath.Join(defaults, "live-team-announcer.py")
	if specs[0].Path != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, specs[0].Path)
	}
}

// TestLoadEnabledEventAddonsRejectsTraversal makes sure a malicious or
// careless config that points at ../etc/passwd cannot escape the
// defaults directory.
func TestLoadEnabledEventAddonsRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	defaults := filepath.Join(tmp, "defaults")
	_ = os.MkdirAll(defaults, 0o755)
	cfgPath := filepath.Join(tmp, "jka-addons.json")
	cfg := `{"addons":{"x":{"enabled":true,"type":"event","script":"../escape.py"}}}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEnabledEventAddons(cfgPath, defaults); err == nil {
		t.Fatalf("expected traversal to be rejected")
	}
}

// TestLoadEnabledEventAddonsHonoursOrder verifies that the order key
// is respected.
func TestLoadEnabledEventAddonsHonoursOrder(t *testing.T) {
	tmp := t.TempDir()
	defaults := filepath.Join(tmp, "defaults")
	_ = os.MkdirAll(defaults, 0o755)
	cfgPath := filepath.Join(tmp, "jka-addons.json")
	cfg := `{"addons":{
		"b":{"enabled":true,"type":"event","script":"b.py","order":5},
		"a":{"enabled":true,"type":"event","script":"a.py","order":10}
	}}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := loadEnabledEventAddons(cfgPath, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].Name != "b" || specs[1].Name != "a" {
		t.Fatalf("unexpected order: %#v", specs)
	}
}
