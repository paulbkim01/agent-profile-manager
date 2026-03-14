package generate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/paulbkim/agent-profile-manager/internal/config"
)

func setup(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		APMDir:       filepath.Join(tmp, "apm"),
		ClaudeDir:    filepath.Join(tmp, ".claude"),
		CommonDir:    filepath.Join(tmp, "apm", "common"),
		ProfilesDir:  filepath.Join(tmp, "apm", "profiles"),
		GeneratedDir: filepath.Join(tmp, "apm", "generated"),
		ConfigPath:   filepath.Join(tmp, "apm", "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// Mock ~/.claude/
	claude := cfg.ClaudeDir
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatalf("creating claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"effortLevel":"high"}`), 0o644); err != nil {
		t.Fatalf("writing claude settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claude, "history.jsonl"), []byte(""), 0o644); err != nil {
		t.Fatalf("writing history.jsonl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.local.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("writing settings.local.json: %v", err)
	}
	for _, dir := range []string{"skills", "plugins", "sessions", "projects"} {
		if err := os.MkdirAll(filepath.Join(claude, dir), 0o755); err != nil {
			t.Fatalf("creating claude %s dir: %v", dir, err)
		}
	}

	// Common settings
	if err := os.WriteFile(filepath.Join(cfg.CommonDir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Read","Write"]}}`), 0o644); err != nil {
		t.Fatalf("writing common settings.json: %v", err)
	}

	// Profile
	profDir := filepath.Join(cfg.ProfilesDir, "work")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("creating profile dir: %v", err)
	}
	for _, sub := range []string{"skills", "commands", "agents"} {
		if err := os.MkdirAll(filepath.Join(profDir, sub), 0o755); err != nil {
			t.Fatalf("creating profile %s dir: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(profDir, "settings.json"),
		[]byte(`{"model":"opus","permissions":{"allow":["Grep"]}}`), 0o644); err != nil {
		t.Fatalf("writing profile settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "profile.yaml"),
		[]byte("name: work\n"), 0o644); err != nil {
		t.Fatalf("writing profile.yaml: %v", err)
	}

	return cfg
}

func TestGenerate(t *testing.T) {
	cfg := setup(t)

	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Check merged settings.json exists
	data, err := os.ReadFile(filepath.Join(genDir, "settings.json"))
	if err != nil {
		t.Fatal("settings.json not found in generated dir")
	}
	// Should contain model from profile
	if len(data) == 0 {
		t.Error("settings.json is empty")
	}

	// Verify deep merge: should have model from profile and permissions union
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing merged settings.json: %v", err)
	}
	if settings["model"] != "opus" {
		t.Errorf("expected model=opus, got %v", settings["model"])
	}
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatal("expected permissions to be an object")
	}
	allow, ok := perms["allow"].([]any)
	if !ok {
		t.Fatal("expected permissions.allow to be an array")
	}
	// Should be union of common (Read, Write) and profile (Grep)
	if len(allow) != 3 {
		t.Errorf("expected 3 permissions, got %d: %v", len(allow), allow)
	}

	// Check shared items are symlinked
	for _, shared := range []string{"history.jsonl", "settings.local.json", "plugins", "sessions", "projects"} {
		link := filepath.Join(genDir, shared)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Errorf("expected symlink for %s: %v", shared, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("expected %s to be a symlink", shared)
		}
	}

	// Check managed dirs exist as real directories (not symlinks)
	for _, dir := range []string{"skills", "commands", "agents"} {
		fi, err := os.Lstat(filepath.Join(genDir, dir))
		if err != nil {
			t.Errorf("expected %s dir: %v", dir, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s should be a real directory, not a symlink", dir)
		}
	}

	// Check .apm-meta.json exists
	if _, err := os.Stat(filepath.Join(genDir, ".apm-meta.json")); err != nil {
		t.Error("expected .apm-meta.json")
	}
}

func TestGenerateRebuilds(t *testing.T) {
	cfg := setup(t)

	// Generate twice -- should clean and rebuild
	if err := Profile(cfg, "work"); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if err := Profile(cfg, "work"); err != nil {
		t.Fatalf("second generate: %v", err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json missing after rebuild")
	}
}

func TestGenerateMissingClaude(t *testing.T) {
	cfg := setup(t)
	if err := os.RemoveAll(cfg.ClaudeDir); err != nil {
		t.Fatalf("removing claude dir: %v", err)
	}

	// Should not fail, just skip symlinks
	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	// Verify settings.json was still generated
	genDir := cfg.GeneratedProfileDir("work")
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json missing even though merge should still work")
	}
}

func TestGenerateMissingProfile(t *testing.T) {
	cfg := setup(t)

	err := Profile(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestGenerateMergeDirOverride(t *testing.T) {
	cfg := setup(t)

	// Add a skill to common
	commonSkill := filepath.Join(cfg.CommonDir, "skills", "shared-skill.md")
	if err := os.WriteFile(commonSkill, []byte("shared skill"), 0o644); err != nil {
		t.Fatalf("writing common skill: %v", err)
	}

	// Add a skill with the same name to profile (should override)
	profSkill := filepath.Join(cfg.ProfileDir("work"), "skills", "shared-skill.md")
	if err := os.WriteFile(profSkill, []byte("profile skill"), 0o644); err != nil {
		t.Fatalf("writing profile skill: %v", err)
	}

	// Add a common-only skill
	commonOnlySkill := filepath.Join(cfg.CommonDir, "skills", "common-only.md")
	if err := os.WriteFile(commonOnlySkill, []byte("common only"), 0o644); err != nil {
		t.Fatalf("writing common-only skill: %v", err)
	}

	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	skillsDir := filepath.Join(genDir, "skills")

	// shared-skill.md should be a symlink to profile version
	link, err := os.Readlink(filepath.Join(skillsDir, "shared-skill.md"))
	if err != nil {
		t.Fatalf("reading symlink for shared-skill.md: %v", err)
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("reading linked file: %v", err)
	}
	if string(data) != "profile skill" {
		t.Errorf("expected profile version of shared-skill.md, got %q", string(data))
	}

	// common-only.md should be a symlink to common version
	link, err = os.Readlink(filepath.Join(skillsDir, "common-only.md"))
	if err != nil {
		t.Fatalf("reading symlink for common-only.md: %v", err)
	}
	data, err = os.ReadFile(link)
	if err != nil {
		t.Fatalf("reading linked file: %v", err)
	}
	if string(data) != "common only" {
		t.Errorf("expected common version of common-only.md, got %q", string(data))
	}
}

func TestGenerateSkipsBackupFiles(t *testing.T) {
	cfg := setup(t)

	// Create backup files in ~/.claude/
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "settings.json.work"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("writing .work file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "settings.json.backup"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("writing .backup file: %v", err)
	}

	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Backup files should NOT be symlinked
	if _, err := os.Lstat(filepath.Join(genDir, "settings.json.work")); err == nil {
		t.Error(".work file should not be symlinked into generated dir")
	}
	if _, err := os.Lstat(filepath.Join(genDir, "settings.json.backup")); err == nil {
		t.Error(".backup file should not be symlinked into generated dir")
	}
}

func TestGenerateMetaHash(t *testing.T) {
	cfg := setup(t)

	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	metaPath := filepath.Join(genDir, ".apm-meta.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading .apm-meta.json: %v", err)
	}

	var meta map[string]string
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parsing .apm-meta.json: %v", err)
	}

	hash, ok := meta["settings_hash"]
	if !ok {
		t.Fatal("expected settings_hash key in .apm-meta.json")
	}
	if len(hash) != 64 {
		t.Errorf("expected SHA-256 hash (64 hex chars), got length %d", len(hash))
	}
}
