package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Config holds values parsed from command-line flags.
type Config struct {
	showVersion bool
	benchmark   bool
	ctagsBin    string
	tagfilePath string
	languages   string
	ctagArgs    string
}

var version = "self compiled" // Populated with -X main.version

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr, checkCtagsInstallation))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, checkCtags func(string) error) int {
	config, err := parseFlags(args, stdout)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	if config.showVersion {
		fmt.Fprintf(stdout, "CTags Language Server %s\n", version)
		return 0
	}

	if err := checkCtags(config.ctagsBin); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	server := &Server{
		cache: FileCache{
			content: make(map[string][]string),
		},
		ctagsBin:    config.ctagsBin,
		tagfilePath: config.tagfilePath,
		languages:   config.languages,
		output:      stdout,
		ctagArgs:    strings.Split(config.ctagArgs, " "),
	}

	if config.benchmark {
		if err := runBenchmark(server); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if err := serve(stdin, server); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	return 0
}

func serve(r io.Reader, server *Server) error {
	reader := bufio.NewReader(r)
	for {
		req, err := readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			server.sendError(nil, -32600, "Malformed request", err.Error())
			continue
		}

		go handleRequest(server, req)
	}
}

func parseFlags(args []string, output io.Writer) (*Config, error) {
	config := &Config{}

	flagset := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flagset.SetOutput(output)
	flagset.Usage = func() {
		flagUsage(output, args[0])
	}
	flagset.BoolVar(&config.showVersion, "version", false, "")
	flagset.BoolVar(&config.benchmark, "benchmark", false, "")
	flagset.StringVar(&config.ctagsBin, "ctags-bin", "ctags", "")
	flagset.StringVar(&config.tagfilePath, "tagfile", "", "")
	flagset.StringVar(&config.languages, "languages", "", "")
	flagset.StringVar(&config.ctagArgs, "ctags-args", "", "")

	if err := flagset.Parse(args[1:]); err != nil {
		return nil, err
	}

	return config, nil
}

func flagUsage(w io.Writer, program string) {
	fmt.Fprintf(w, `CTags Language Server
Provides LSP functionality based on ctags.

Usage:
  %s [options]

Options:
  --help               Show this help message
  --version            Show version information
  --ctags-bin <name>   Use custom ctags binary name (default: "ctags")
  --tagfile <path>     Use custom tagfile (default: tries "tags", ".tags" and ".git/tags")
  --languages <value>  Pass through language filter list to ctags
  --ctags-args <value> Pass through ctags arg
`, program)
}

func getInstallInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return "You can install Universal Ctags with: brew install universal-ctags"
	case "linux":
		return "You can install Universal Ctags with:\n" +
			"- Ubuntu/Debian: sudo apt-get install universal-ctags\n" +
			"- Fedora: sudo dnf install ctags\n" +
			"- Arch Linux: sudo pacman -S ctags"
	case "windows":
		return "You can install Universal Ctags with:\n" +
			"- Chocolatey: choco install universal-ctags\n" +
			"- Scoop: scoop install universal-ctags\n" +
			"Or download from: https://github.com/universal-ctags/ctags-win32/releases"
	default:
		return "Please visit https://github.com/universal-ctags/ctags for installation instructions"
	}
}

func checkCtagsInstallation(ctagsBin string) error {
	cmd := exec.Command(ctagsBin, "--version", "--output-format=json")
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), "Universal Ctags") {
		return fmt.Errorf("%s command not found or incorrect version. Universal Ctags with JSON support is required.\n%s", ctagsBin, getInstallInstructions())
	}

	return nil
}

func runBenchmark(server *Server) error {
	mockID := json.RawMessage(`1`)
	mockParams := InitializeParams{RootURI: ""}
	mockParamsBytes, err := json.Marshal(mockParams)
	if err != nil {
		return fmt.Errorf("marshal initialize params: %w", err)
	}

	mockReq := RPCRequest{
		Jsonrpc: "2.0",
		ID:      &mockID,
		Method:  "initialize",
		Params:  mockParamsBytes,
	}

	handleInitialize(server, mockReq)
	return nil
}
