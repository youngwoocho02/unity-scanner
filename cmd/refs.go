package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/youngwoocho02/unity-scanner/internal/unityasset"
)

var guidLiteralRE = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

type refsOptions struct {
	commonOptions
	types  string
	detail bool
	limit  int
}

func refsCmd(args []string) error {
	opts := refsOptions{limit: 80}
	fs := flag.NewFlagSet("refs", flag.ContinueOnError)
	addCommonFlags(fs, &opts.commonOptions)
	fs.StringVar(&opts.types, "type", "", "comma-separated asset kinds")
	fs.BoolVar(&opts.detail, "detail", false, "print detailed matches")
	fs.IntVar(&opts.limit, "limit", opts.limit, "max result files")
	if err := parse(fs, args); err != nil {
		if err == flag.ErrHelp {
			printTopicHelp(os.Stdout, "refs")
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("refs requires an asset path or GUID")
	}

	project, err := unityasset.OpenProject(opts.project)
	if err != nil {
		return err
	}

	target := fs.Arg(0)
	guid, label, err := resolveRefGUID(project, target)
	if err != nil {
		return err
	}

	scanPath := "Assets"
	if fs.NArg() > 1 {
		scanPath = fs.Arg(1)
	}
	kinds := unityasset.ParseKindSet(opts.types)
	kinds = defaultSearchKinds(kinds, searchOptions{guid: guid})
	result, err := unityasset.Scan(project, scanPath, unityasset.ScanOptions{Kinds: kinds})
	if err != nil {
		return err
	}

	searchOpts := searchOptions{
		guid:    guid,
		types:   opts.types,
		compact: !opts.detail,
		limit:   opts.limit,
	}
	_, searchOpts.rootPath, _ = project.Resolve(scanPath)
	matches, warnings := runSearch(project, result.Files, unityasset.ScriptIndex{}, searchOpts)

	fmt.Printf("REF     %s\n", label)
	fmt.Printf("GUID    %s\n\n", guid)
	printSearch(matches, result.KindCount, searchOpts, warnings)
	return nil
}

func resolveRefGUID(project unityasset.Project, target string) (string, string, error) {
	if guidLiteralRE.MatchString(target) {
		return strings.ToLower(target), "guid", nil
	}

	abs, assetPath, err := project.Resolve(target)
	if err != nil {
		return "", "", err
	}

	metaPath := abs + ".meta"
	if strings.EqualFold(filepath.Ext(abs), ".meta") {
		metaPath = abs
		assetPath = strings.TrimSuffix(assetPath, ".meta")
	}
	guid := unityasset.ReadMetaGUID(metaPath)
	if guid == "" {
		return "", "", fmt.Errorf("meta GUID not found: %s", filepath.ToSlash(metaPath))
	}
	return strings.ToLower(guid), assetPath, nil
}
