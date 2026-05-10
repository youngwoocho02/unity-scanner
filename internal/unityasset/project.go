package unityasset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Project struct {
	Root   string
	Assets string
}

func OpenProject(path string) (Project, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Project{}, err
		}
		path = cwd
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, err
	}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	for {
		assets := filepath.Join(abs, "Assets")
		if info, statErr := os.Stat(assets); statErr == nil && info.IsDir() {
			return Project{Root: abs, Assets: assets}, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}

	return Project{}, fmt.Errorf("Unity project not found from %q: missing Assets directory", path)
}

func (p Project) Resolve(input string) (string, string, error) {
	if input == "" {
		input = "Assets"
	}

	var abs string
	if filepath.IsAbs(input) {
		abs = filepath.Clean(input)
	} else {
		clean := filepath.Clean(input)
		if clean == "." {
			abs = p.Assets
		} else {
			abs = filepath.Join(p.Root, clean)
		}
	}

	rel, err := filepath.Rel(p.Root, abs)
	if err != nil {
		return "", "", err
	}
	if rel == "." {
		rel = ""
	}
	if strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("path is outside project: %s", input)
	}

	return abs, filepath.ToSlash(rel), nil
}

func (p Project) AssetPath(abs string) string {
	rel, err := filepath.Rel(p.Root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}
