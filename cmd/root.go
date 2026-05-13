package cmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
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
		return listCmd(args[1:])
	case "read", "cat":
		return readCmd(args[1:])
	case "search", "find":
		return searchCmd(args[1:])
	case "refs":
		return refsCmd(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type commonOptions struct {
	project   string
	lineWidth int
	profile   bool
	workers   int
}

func addCommonFlags(fs *flag.FlagSet, opts *commonOptions) {
	opts.lineWidth = 1200
	fs.StringVar(&opts.project, "project", "", "Unity project path")
	fs.StringVar(&opts.project, "p", "", "Unity project path")
	fs.IntVar(&opts.lineWidth, "line-width", opts.lineWidth, "max output line width, 0 disables truncation")
	fs.BoolVar(&opts.profile, "profile", false, "print command timing profile")
	fs.IntVar(&opts.workers, "workers", opts.workers, "parallel worker count, default CPU count")
}

func lineLimit(opts commonOptions) int {
	return opts.lineWidth
}

func printLineLimited(limit int, line string) {
	if limit > 0 && len(line) > limit {
		if limit <= 3 {
			line = line[:limit]
		} else {
			line = line[:limit-3] + "..."
		}
	}
	fmt.Println(line)
}

func printfLineLimited(limit int, format string, args ...any) {
	printLineLimited(limit, fmt.Sprintf(format, args...))
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

type commandProfileStep struct {
	name    string
	elapsed time.Duration
}

type commandProfile struct {
	enabled bool
	start   time.Time
	last    time.Time
	steps   []commandProfileStep
}

func newCommandProfile(enabled bool) *commandProfile {
	now := time.Now()
	return &commandProfile{enabled: enabled, start: now, last: now}
}

func (p *commandProfile) mark(name string) {
	if !p.enabled {
		return
	}
	now := time.Now()
	p.steps = append(p.steps, commandProfileStep{name: name, elapsed: now.Sub(p.last)})
	p.last = now
}

func (p *commandProfile) print() {
	if !p.enabled || len(p.steps) == 0 {
		return
	}
	total := time.Since(p.start)
	fmt.Println()
	fmt.Println("PROFILE")
	for _, step := range p.steps {
		fmt.Printf("  %-22s %s\n", step.name, formatDuration(step.elapsed))
	}
	fmt.Printf("  %-22s %s\n", "total", formatDuration(total))
}

func formatDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `unity-scanner scans Unity project assets without opening Unity.

Usage:
  unity-scanner <command> [options] [path]
  unity-scanner help [command]

Commands:
  list     compressed ls for Unity assets (alias: ls)
  read     readable summary for .prefab/.unity/.asset YAML (alias: cat)
  search   structured name/component/guid search (alias: find)
  refs     find Unity YAML references to an asset GUID
  update   update to the latest GitHub release
  help     show general help or command help
  version  print version

Root options:
  -h, --help             Show help
  -v, --version          Print version

Project commands:
  -p, --project <path>   Unity project path
  --line-width <n>       Max output line width, default 1200, 0 disables truncation
  --profile              Print command timing profile
  --workers <n>          Parallel worker count, default CPU count

Examples:
  unity-scanner help read
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

Aliases:
  ls

Common:
  -p, --project <path>   Unity project path
  -h, --help             Show help
  --line-width <n>       Max output line width, default 1200, 0 disables truncation
  --profile              Print command timing profile
  --workers <n>          Parallel worker count, default CPU count

Options:
  --depth <n>       directory summary depth, default unlimited
  --kind <list>     comma-separated kinds: prefab,scene,asset,cs,mat
  --meta            include .meta files in body
  --flat            omit directory summary, print grouped names only
  --limit <n>       max groups, default unlimited

Examples:
  unity-scanner list -p . Assets --depth 2
  unity-scanner ls -p . Assets/Prefabs --kind prefab --flat
`)
	case "read", "cat":
		fmt.Fprint(w, `Usage:
  unity-scanner read [options] <asset>

Aliases:
  cat

Common:
  -p, --project <path>   Unity project path
  -h, --help             Show help
  --line-width <n>       Max output line width, default 1200, 0 disables truncation
  --profile              Print command timing profile
  --workers <n>          Parallel worker count, default CPU count

Options:
  --depth <n>          hierarchy depth, default unlimited
  --path <name/path>   only show matching object branch
  --component <name>   show fields for matching component
  --field-limit <n>    max fields per component, default unlimited
  --limit <n>          max GameObjects/component matches, default unlimited
  --full-tree          show every visible tree row without render-only folding
  --override <text>    only show prefab overrides matching text
  --override-limit <n> max prefab overrides shown, default 40, 0 unlimited
  --no-resolve         skip script, GUID, and source prefab path resolution

Examples:
  unity-scanner read -p . Assets/Scenes/Main.unity --depth 3
  unity-scanner cat -p . Assets/Prefabs/Hero.prefab --component MeshRenderer
`)
	case "search", "find":
		fmt.Fprint(w, `Usage:
  unity-scanner search [options] [path]

Aliases:
  find

Common:
  -p, --project <path>   Unity project path
  -h, --help             Show help
  --line-width <n>       Max output line width, default 1200, 0 disables truncation
  --profile              Print command timing profile
  --workers <n>          Parallel worker count, default CPU count

Options:
  --name <text>        match file or GameObject name
  --component <text>   match component/script name
  --script-path <path> match MonoBehaviour scripts under asset path
  --guid <guid>        match raw Unity GUID reference
  --ref <guid>         alias of --guid
  --type <list>        prefab,scene,asset,cs,mat
  --compact           one-line grouped result
  --warnings <mode>    warning output: summary or detail, default summary
  --limit <n>          max result files, default unlimited
  --object-limit <n>   max objects shown per result file, default 12

Examples:
  unity-scanner search -p . --name Station --type prefab,scene
  unity-scanner find -p . Assets --component GameManager --compact
`)
	case "refs":
		fmt.Fprint(w, `Usage:
  unity-scanner refs [options] <asset-or-guid> [scan-path]

Common:
  -p, --project <path>   Unity project path
  -h, --help             Show help
  --line-width <n>       Max output line width, default 1200, 0 disables truncation
  --profile              Print command timing profile
  --workers <n>          Parallel worker count, default CPU count

Options:
  --type <list>        prefab,scene,asset,mat,controller
  --detail             print detailed matches instead of compact groups
  --warnings <mode>    warning output: summary or detail, default summary
  --limit <n>          max result files, default unlimited

Examples:
  unity-scanner refs -p . Assets/Scripts/Foo.cs
  unity-scanner refs -p . 0123456789abcdef0123456789abcdef Assets/Prefabs
`)
	case "update":
		fmt.Fprint(w, `Usage:
  unity-scanner update [options]

Update the CLI binary to the latest release from GitHub.

Options:
  -h, --help            Show help
  --check              Check for updates without installing

Examples:
  unity-scanner update
  unity-scanner update --check
`)
	default:
		printHelp(w)
	}
}
