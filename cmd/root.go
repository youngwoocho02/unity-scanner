package cmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

var Version = "dev"

func Execute(args []string) error {
	if len(args) == 0 {
		printHelp(os.Stdout)
		return nil
	}

	switch args[0] {
	case "help", "--help", "-h":
		if len(args) > 1 {
			printTopicHelp(os.Stdout, args[1])
		} else {
			printHelp(os.Stdout)
		}
		return nil
	case "version", "--version", "-v":
		fmt.Fprintf(os.Stdout, "unity-scanner %s\n", Version)
		return nil
	case "update":
		return updateCmd(args[1:])
	case "list", "ls":
		return runWithUpdateNotice(listCmd(args[1:]))
	case "read", "cat":
		return runWithUpdateNotice(readCmd(args[1:]))
	case "search", "find":
		return runWithUpdateNotice(searchCmd(args[1:]))
	case "refs":
		return runWithUpdateNotice(refsCmd(args[1:]))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runWithUpdateNotice(err error) error {
	if err == nil {
		printUpdateNotice()
	}
	return err
}

type commonOptions struct {
	project string
}

func addCommonFlags(fs *flag.FlagSet, opts *commonOptions) {
	fs.StringVar(&opts.project, "project", "", "Unity project path")
	fs.StringVar(&opts.project, "p", "", "Unity project path")
}

func parse(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(reorderFlagArgs(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flag.ErrHelp
		}
		return err
	}
	return nil
}

func reorderFlagArgs(fs *flag.FlagSet, args []string) []string {
	flagArgs := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		flagArgs = append(flagArgs, arg)
		name := strings.TrimLeft(arg, "-")
		if before, _, ok := strings.Cut(name, "="); ok {
			name = before
		}
		f := fs.Lookup(name)
		if f == nil || isBoolFlag(f) || strings.Contains(arg, "=") {
			continue
		}
		if i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return append(flagArgs, positionals...)
}

func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface {
		IsBoolFlag() bool
	}
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

func commaList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsFold(s, needle string) bool {
	if needle == "" {
		return true
	}
	if strings.Contains(s, needle) {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(needle))
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `unity-scanner scans Unity project assets without opening Unity.

Usage:
  unity-scanner <command> [options] [path]

Commands:
  list     compressed ls for Unity assets
  read     readable summary for .prefab/.unity/.asset YAML
  search   structured name/component/guid search
  refs     find Unity YAML references to an asset GUID
  update   update to the latest GitHub release

Common:
  -p, --project <path>   Unity project path

Examples:
  unity-scanner list -p /projects/MyUnityProject Assets --depth 2
  unity-scanner read -p . Assets/Scenes/Main.unity --component GameManager
  unity-scanner search -p . --name Station --type prefab,scene
  unity-scanner refs -p . Assets/Scripts/Foo.cs
  unity-scanner update --check
`)
}

func printTopicHelp(w io.Writer, topic string) {
	switch topic {
	case "list", "ls":
		fmt.Fprint(w, `Usage:
  unity-scanner list [options] [path]

Options:
  --depth <n>       directory summary depth, default 2
  --kind <list>     comma-separated kinds: prefab,scene,asset,cs,mat
  --meta            include .meta files in body
  --flat            omit directory summary, print grouped names only
  --limit <n>       max groups, default 80
`)
	case "read", "cat":
		fmt.Fprint(w, `Usage:
  unity-scanner read [options] <asset>

Options:
  --depth <n>          hierarchy depth, default 2
  --path <name/path>   only show matching object branch
  --component <name>   show fields for matching component
  --field-limit <n>    max fields per component, default 20
  --limit <n>          max GameObjects/component matches, default 60
  --full-tree          show every visible tree row without render-only folding
`)
	case "search", "find":
		fmt.Fprint(w, `Usage:
  unity-scanner search [options] [path]

Options:
  --name <text>        match file or GameObject name
  --component <text>   match component/script name
  --guid <guid>        match raw Unity GUID reference
  --ref <guid>         alias of --guid
  --type <list>        prefab,scene,asset,cs,mat
  --compact           one-line grouped result
  --limit <n>          max result files, default 80
`)
	case "refs":
		fmt.Fprint(w, `Usage:
  unity-scanner refs [options] <asset-or-guid> [scan-path]

Options:
  --type <list>        prefab,scene,asset,mat,controller
  --detail             print detailed matches instead of compact groups
  --limit <n>          max result files, default 80
`)
	case "update":
		fmt.Fprint(w, `Usage:
  unity-scanner update [options]

Update the CLI binary to the latest release from GitHub.

Options:
  --check              Check for updates without installing

Examples:
  unity-scanner update
  unity-scanner update --check
`)
	default:
		printHelp(w)
	}
}
