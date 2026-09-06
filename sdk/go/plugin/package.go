package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ManifestFile is the generated contract each installed package carries.
const ManifestFile = "manifest.json"

// Package is one installed plugin artifact on disk.
type Package struct {
	Directory string
	Manifest  Manifest
}

// Digest hashes a package's source files.
//
// The algorithm is mirrored by workflow_sdk.registry.digest so the Go platform
// and the Python worker derive the same value from the same directory: files are
// visited in ascending POSIX relative-path order, and each contributes its path,
// its byte length, and its contents. The manifest is excluded because it carries
// the digest; its contract is compared separately when a version is registered.
func Digest(directory string) (string, error) {
	var names []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "__pycache__" {
				return fs.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ManifestFile || strings.HasSuffix(relative, ".pyc") {
			return nil
		}
		names = append(names, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write([]byte(strconv.Itoa(len(data))))
		hash.Write([]byte{0})
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Load reads every installed package under root and verifies each one against
// its sealed digest. A package whose files no longer match the manifest that
// describes them stops the platform here rather than failing mid-run.
func Load(root string) ([]Package, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", ManifestFile))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	packages := make([]Package, 0, len(paths))
	seen := map[string]string{}
	for _, path := range paths {
		directory := filepath.Dir(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var manifest Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := manifest.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		actual, err := Digest(directory)
		if err != nil {
			return nil, err
		}
		if actual != manifest.Digest {
			return nil, fmt.Errorf(
				"%s@%s: package contents do not match the sealed digest (manifest %.12s…, files %.12s…); run \"workflow-plugin build\"",
				manifest.Name, manifest.Version, manifest.Digest, actual)
		}
		key := manifest.Key()
		if previous, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("%s is installed twice: %s and %s", key, previous, directory)
		}
		seen[key] = directory
		packages = append(packages, Package{Directory: directory, Manifest: manifest})
	}
	return packages, nil
}

// Key identifies one immutable plugin release.
func (m Manifest) Key() string { return m.Name + "@" + m.Version }

// Contract is everything a saved workflow revision depends on: the ports, the
// schemas, and the identity, but not the artifact digest. Two releases with the
// same contract are interchangeable for graph validation.
func (m Manifest) Contract() string {
	m.Digest = ""
	raw, _ := json.Marshal(m)
	return string(raw)
}
