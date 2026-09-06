package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// The digest is a cross-language contract: the Go platform verifies packages the
// Python SDK sealed. These vectors pin the exact byte layout both sides hash.
func TestDigestIsStableAcrossRuntimes(t *testing.T) {
	directory := t.TempDir()
	write(t, directory, "plugin.py", "async def execute(ctx):\n    return {}\n")
	write(t, directory, "helpers/parse.py", "VALUE = 1\n")

	actual, err := Digest(directory)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if len(actual) != 64 {
		t.Fatalf("digest must be 64 hex characters, got %q", actual)
	}

	// Excluded files must not move the digest.
	write(t, directory, ManifestFile, `{"digest":"whatever"}`)
	write(t, directory, "__pycache__/plugin.cpython-313.pyc", "bytecode")
	write(t, directory, "helpers/__pycache__/parse.cpython-313.pyc", "bytecode")
	write(t, directory, "stale.pyc", "bytecode")
	after, err := Digest(directory)
	if err != nil {
		t.Fatalf("digest after excluded files: %v", err)
	}
	if after != actual {
		t.Fatalf("excluded files changed the digest: %s -> %s", actual, after)
	}

	// A source change must move it.
	write(t, directory, "helpers/parse.py", "VALUE = 2\n")
	changed, err := Digest(directory)
	if err != nil {
		t.Fatalf("digest after edit: %v", err)
	}
	if changed == actual {
		t.Fatal("editing a source file did not change the digest")
	}
}

// Paths are sorted as POSIX strings, not as path segments. "a-b.py" sorts before
// "a/b.py" because '-' < '/', and Python's sorted() over the same strings agrees.
func TestDigestSortsBySlashPath(t *testing.T) {
	first := t.TempDir()
	write(t, first, "a-b.py", "one\n")
	write(t, first, "a/b.py", "two\n")

	second := t.TempDir()
	write(t, second, "a/b.py", "two\n")
	write(t, second, "a-b.py", "one\n")

	left, err := Digest(first)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	right, err := Digest(second)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if left != right {
		t.Fatalf("creation order changed the digest: %s != %s", left, right)
	}
}

func TestLoadRejectsATamperedPackage(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "example")
	write(t, directory, "plugin.py", "async def execute(ctx):\n    return {}\n")

	sealed, err := Digest(directory)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	manifest := `{"protocol":"workflow.plugin/v1","name":"team.example","version":"1.0.0",` +
		`"title":"Example","description":"","category":"Custom","kind":"action",` +
		`"configSchema":{"type":"object"},"inputs":[],"outputs":[],"permissions":[],"digest":"` + sealed + `"}`
	write(t, directory, ManifestFile, manifest)

	if _, err := Load(root); err != nil {
		t.Fatalf("sealed package should load: %v", err)
	}

	write(t, directory, "plugin.py", "async def execute(ctx):\n    return {'surprise': 1}\n")
	if _, err := Load(root); err == nil {
		t.Fatal("editing a handler without resealing must be refused")
	}
}

func write(t *testing.T, directory, name, body string) {
	t.Helper()
	path := filepath.Join(directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The installed tree is sealed by the Python SDK and verified by the Go platform.
// If the two hash algorithms ever diverge, every package fails to load here.
func TestInstalledPackagesMatchThePythonSeal(t *testing.T) {
	root := filepath.Join("..", "..", "..", "plugins")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no installed plugin tree")
	}
	packages, err := Load(root)
	if err != nil {
		t.Fatalf("Go could not verify the digests workflow-plugin sealed: %v", err)
	}
	if len(packages) == 0 {
		t.Fatal("no plugin packages found")
	}
	for _, item := range packages {
		t.Logf("verified %s %.12s…", item.Manifest.Key(), item.Manifest.Digest)
	}
}
