package unityasset

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type FileEntry struct {
	Abs       string
	AssetPath string
	Dir       string
	Name      string
	Ext       string
	Kind      string
	IsMeta    bool
}

type ScanOptions struct {
	Kinds       map[string]bool
	IncludeMeta bool
	Workers     int
}

type ScanResult struct {
	Files     []FileEntry
	MetaCount int
	KindCount map[string]int
}

func Scan(p Project, input string, opts ScanOptions) (ScanResult, error) {
	abs, _, err := p.Resolve(input)
	if err != nil {
		return ScanResult{}, err
	}

	result := ScanResult{KindCount: map[string]int{}}
	info, err := os.Stat(abs)
	if err != nil {
		return ScanResult{}, err
	}

	visit := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != abs {
				return filepath.SkipDir
			}
			return nil
		}

		entry := makeEntry(p, path)
		if entry.IsMeta {
			result.MetaCount++
			if !opts.IncludeMeta {
				return nil
			}
		}
		if len(opts.Kinds) > 0 && !opts.Kinds[entry.Kind] {
			return nil
		}
		result.KindCount[entry.Kind]++
		result.Files = append(result.Files, entry)
		return nil
	}

	if !info.IsDir() {
		entry := makeEntry(p, abs)
		if entry.IsMeta {
			result.MetaCount = 1
		}
		if acceptEntry(entry, opts) {
			result.KindCount[entry.Kind]++
			result.Files = append(result.Files, entry)
		}
		return result, nil
	}

	workers := scanWorkers(opts.Workers)
	if workers > 1 {
		result, err = scanDirectoryParallel(p, abs, opts, workers)
	} else {
		err = filepath.WalkDir(abs, visit)
	}
	if err != nil {
		return ScanResult{}, err
	}

	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].AssetPath < result.Files[j].AssetPath
	})
	return result, nil
}

func scanWorkers(requested int) int {
	if requested > 0 {
		return requested
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		return 1
	}
	if workers > 32 {
		return 32
	}
	return workers
}

func scanDirectoryParallel(p Project, root string, opts ScanOptions, workers int) (ScanResult, error) {
	result := ScanResult{KindCount: map[string]int{}}
	jobs := make(chan string, workers*4)
	var dirWG sync.WaitGroup
	var workerWG sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	addDir := func(path string) {
		dirWG.Add(1)
		go func() {
			jobs <- path
		}()
	}

	processFile := func(path string) {
		entry := makeEntry(p, path)
		mu.Lock()
		defer mu.Unlock()
		addScanEntry(&result, entry, opts)
	}

	for i := 0; i < workers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for dir := range jobs {
				entries, err := os.ReadDir(dir)
				if err != nil {
					recordErr(err)
					dirWG.Done()
					continue
				}
				for _, entry := range entries {
					path := filepath.Join(dir, entry.Name())
					if entry.IsDir() {
						if shouldSkipDir(entry.Name()) && path != root {
							continue
						}
						addDir(path)
						continue
					}
					processFile(path)
				}
				dirWG.Done()
			}
		}()
	}

	addDir(root)
	dirWG.Wait()
	close(jobs)
	workerWG.Wait()

	if firstErr != nil {
		return ScanResult{}, firstErr
	}
	return result, nil
}

func addScanEntry(result *ScanResult, entry FileEntry, opts ScanOptions) {
	if entry.IsMeta {
		result.MetaCount++
		if !opts.IncludeMeta {
			return
		}
	}
	if len(opts.Kinds) > 0 && !opts.Kinds[entry.Kind] {
		return
	}
	result.KindCount[entry.Kind]++
	result.Files = append(result.Files, entry)
}

func acceptEntry(entry FileEntry, opts ScanOptions) bool {
	if entry.IsMeta && !opts.IncludeMeta {
		return false
	}
	if len(opts.Kinds) > 0 && !opts.Kinds[entry.Kind] {
		return false
	}
	return true
}

func makeEntry(p Project, abs string) FileEntry {
	assetPath := p.AssetPath(abs)
	ext := filepath.Ext(abs)
	name := strings.TrimSuffix(filepath.Base(abs), ext)
	kind := KindForPath(abs)
	return FileEntry{
		Abs:       abs,
		AssetPath: assetPath,
		Dir:       filepath.ToSlash(filepath.Dir(assetPath)),
		Name:      name,
		Ext:       ext,
		Kind:      kind,
		IsMeta:    kind == "meta",
	}
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".vs", "Library", "Logs", "obj", "Obj", "Temp", "Build", "Builds", "UserSettings":
		return true
	default:
		return false
	}
}
