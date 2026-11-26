// Command fileforge-cli runs FileForge tools and recipes headlessly.
//
// It composes the same registry, job engine, tooling service and recipe
// service the desktop app uses, but never imports or boots Wails. Output is
// human-readable by default; --json prints exactly one object on stdout.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"fileforge-desktop/internal/models"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitValidation
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return exitSuccess
	case "tools":
		return runTools(ctx, args[1:], stdout, stderr)
	case "recipes":
		return runRecipes(ctx, args[1:], stdout, stderr)
	case "run":
		return runJob(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitValidation
	}
}

// runTools handles `tools list [--json]`.
func runTools(_ context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(stderr, "usage: fileforge-cli tools list [--json]")
		return exitValidation
	}

	asJSON, err := parseListFlags("tools list", args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, "usage: fileforge-cli tools list [--json]")
			return exitSuccess
		}
		fmt.Fprintln(stderr, err)
		return exitValidation
	}

	s := newStack()
	defer s.close()

	response := s.tooling.ListTools()
	if asJSON {
		writeJSON(stdout, response)
		return exitSuccess
	}

	for _, entry := range response.Tools {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", entry.Manifest.ToolID, entry.Manifest.Name, entry.State.Status)
	}

	return exitSuccess
}

// runRecipes handles `recipes list [--json]`.
func runRecipes(_ context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(stderr, "usage: fileforge-cli recipes list [--json]")
		return exitValidation
	}

	asJSON, err := parseListFlags("recipes list", args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, "usage: fileforge-cli recipes list [--json]")
			return exitSuccess
		}
		fmt.Fprintln(stderr, err)
		return exitValidation
	}

	s := newStack()
	defer s.close()

	list := s.recipes.List()
	if asJSON {
		writeJSON(stdout, models.ListRecipesResponse{
			Success: true,
			Message: "recipes listed",
			Recipes: list,
		})
		return exitSuccess
	}

	for _, recipe := range list {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", recipe.ID, recipe.Name, recipe.Category)
	}

	return exitSuccess
}

// parseListFlags parses the flags shared by both list commands. The flag set
// writes nothing itself so the caller prints every error once.
func parseListFlags(name string, args []string) (asJSON bool, err error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	jsonFlag := flags.Bool("json", false, "print one machine-readable JSON object on stdout")
	if err := flags.Parse(args); err != nil {
		return false, err
	}
	if flags.NArg() > 0 {
		return false, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}

	return *jsonFlag, nil
}

// runJob handles `run tool ...` and `run recipe ...`.
func runJob(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: fileforge-cli run <tool|recipe> <id> --input PATH [--input PATH...] [--out DIR] [--option key=value...] [--json]")
		return exitValidation
	}

	kind := args[0]
	if kind != "tool" && kind != "recipe" {
		fmt.Fprintf(stderr, "unknown run target %q: expected tool or recipe\n", kind)
		return exitValidation
	}
	if len(args) < 2 {
		fmt.Fprintf(stderr, "usage: fileforge-cli run %s <id> --input PATH\n", kind)
		return exitValidation
	}
	if args[1] == "-h" || args[1] == "--help" {
		printUsage(stdout)
		return exitSuccess
	}

	id := args[1]
	runOpts, err := parseRunFlags("run "+kind, args[2:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return exitSuccess
		}
		fmt.Fprintln(stderr, err)
		return exitValidation
	}

	s := newStack()
	defer s.close()

	if kind == "tool" {
		return runTool(ctx, s, id, runOpts, stdout, stderr)
	}

	return runRecipe(ctx, s, id, runOpts, stdout, stderr)
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `fileforge-cli runs FileForge tools and recipes without the desktop app.

Usage:
  fileforge-cli tools list [--json]
  fileforge-cli recipes list [--json]
  fileforge-cli run tool <toolID> --input PATH [--input PATH...] [--out DIR] [--option key=value...] [--json]
  fileforge-cli run recipe <recipeID> --input PATH [--input PATH...] [--out DIR] [--option key=value...] [--json]
  fileforge-cli help

Options:
  --input PATH          input file; repeat for every input
  --out DIR             output directory (defaults to the input directory)
  --option key=value    tool or recipe option; repeat for every option
  --json                print exactly one JSON object on stdout

Every run is planned first. A failed plan prints the canonical code and
message and exits 2 without starting a job.

Exit codes:
  0  success
  1  internal error
  2  validation or usage error
  3  the job finished failed or partial
`)
}
