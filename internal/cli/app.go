package cli

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/withnic/kiku/internal/config"
	"github.com/withnic/kiku/internal/engine"
	"github.com/withnic/kiku/internal/fs"
	"github.com/withnic/kiku/internal/parser"
)

//go:embed default.kiku.yaml
var defaultConfig []byte

// Run executes a CLI invocation and returns its process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	code, err := run(args, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return config.ErrorExitCode
	}
	return code
}

func run(args []string, stdout, stderr io.Writer) (int, error) {
	const usage = "usage: kiku <check|fix|init|version> [options] [paths...]"
	if len(args) == 0 {
		return 0, errors.New(usage)
	}
	switch command := args[0]; command {
	case "-h", "--help":
		_, err := fmt.Fprintln(stdout, usage)
		return 0, err
	case "version":
		if len(args) != 1 {
			return 0, errors.New("version takes no arguments")
		}
		_, err := fmt.Fprintln(stdout, "kiku v0.1.0")
		return 0, err
	case "init", "check", "fix":
		return runCommand(command, args[1:], stdout, stderr)
	default:
		return 0, fmt.Errorf("unknown command %q\n%s", command, usage)
	}
}

func runCommand(command string, args []string, stdout, stderr io.Writer) (int, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var configPath, format string
	var diff, printFixed, force bool
	var ignores []string
	if command == "init" {
		flags.BoolVar(&force, "force", false, "replace existing configuration")
	} else {
		flags.StringVar(&configPath, "config", "", "configuration file")
		flags.Func("ignore", "exclude a matching path (repeatable)", func(pattern string) error {
			ignores = append(ignores, pattern)
			return nil
		})
		if command == "check" {
			flags.StringVar(&format, "format", "text", "diagnostic format (text, json, github)")
		} else {
			flags.BoolVar(&diff, "diff", false, "print a unified diff")
			flags.BoolVar(&printFixed, "stdout", false, "print the fixed contents of one file")
		}
	}
	paths, err := parseFlags(flags, args)
	if errors.Is(err, flag.ErrHelp) {
		if _, err := fmt.Fprintf(stdout, "usage: kiku %s [options]", command); err != nil {
			return 0, err
		}
		if command != "init" {
			fmt.Fprint(stdout, " [paths...]")
		}
		fmt.Fprintln(stdout)
		flags.SetOutput(stdout)
		flags.PrintDefaults()
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	if command == "init" {
		if len(paths) != 0 {
			return 0, errors.New("init takes no paths")
		}
		path := filepath.Join(cwd, ".kiku.yaml")
		err := fs.CreateFile(path, defaultConfig)
		if force && errors.Is(err, os.ErrExist) {
			err = fs.AtomicWrite(path, defaultConfig)
		}
		return 0, err
	}
	fix := command == "fix"
	if !fix && format != "text" && format != "json" && format != "github" {
		return 0, fmt.Errorf("unknown output format %q", format)
	}
	if diff && printFixed {
		return 0, errors.New("--diff and --stdout cannot be combined")
	}

	loader, err := config.NewLoader(cwd, configPath, ignores)
	if err != nil {
		return 0, err
	}
	if configPath != "" {
		if _, err := loader.Load(""); err != nil {
			return 0, err
		}
	}
	targets, err := engine.Discover(cwd, paths, loader)
	if err != nil {
		return 0, err
	}
	if printFixed && len(targets) != 1 {
		return 0, errors.New("--stdout requires exactly one target file")
	}
	diagnostics, outputs, runErr := engine.Run(targets, fix, fix && !diff && !printFixed)
	if !fix {
		code := 0
		if len(diagnostics) != 0 {
			code = 1
		}
		return code, errors.Join(runErr, report(stdout, format, diagnostics))
	}
	code := 0
	var remaining []parser.Diagnostic
	for _, diagnostic := range diagnostics {
		if !diagnostic.Fixable {
			remaining = append(remaining, diagnostic)
			if diagnostic.Severity == parser.SeverityError {
				code = 1
			}
		}
	}
	var outputErr error
	for _, output := range outputs {
		if printFixed {
			_, outputErr = stdout.Write(output.Fixed)
		} else if diff {
			outputErr = writeDiff(stdout, output)
		}
		if outputErr != nil {
			break
		}
	}
	return code, errors.Join(runErr, outputErr, report(stderr, "text", remaining))
}

// parseFlags permits options between path arguments and honors the -- terminator.
func parseFlags(flags *flag.FlagSet, args []string) ([]string, error) {
	var options, paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			paths = append(paths, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
			continue
		}
		options = append(options, arg)
		name, _, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		if option := flags.Lookup(name); option != nil && !hasValue && i+1 < len(args) {
			boolean, ok := option.Value.(interface{ IsBoolFlag() bool })
			if !ok || !boolean.IsBoolFlag() {
				i++
				options = append(options, args[i])
			}
		}
	}
	return paths, flags.Parse(options)
}
