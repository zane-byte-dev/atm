package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
)

// The desktop app talks to Go by fork/exec'ing this binary, so every screen it
// draws is a promise about a command path and its flags. Nothing in the Swift
// compiler knows that, and nothing in the Go compiler knows it either: renaming
// `atm todo done` builds clean on both sides and fails at runtime, in a panel,
// on the user's machine.
//
// app/macos/atm-cli-contract.txt is where that promise is written down, and this
// file is what makes it a promise. It checks both directions, because either one
// alone rots: the forward pass proves the CLI still answers what the list says,
// and the reverse pass proves the list still describes what Swift actually calls.

const appContractPath = "app/macos/atm-cli-contract.txt"

type appContract struct {
	// commands maps "todo done" to the flags the app passes with it.
	commands map[string]map[string]bool
	// prefixes are argv heads the app concatenates onto rather than executes, so
	// they appear in the Swift source but are not commands.
	prefixes     map[string]bool
	configKeys   []string
	ignoredFlags map[string]bool
	// ipcVerbs are the `atm _ipc` verbs the app asks for. Checked against the
	// registered handlers rather than against the command tree: the verb is an
	// argument, so cobra resolves `_ipc anything` happily and would catch nothing.
	ipcVerbs []string
}

// repoRoot walks up from the test's working directory to the module root, so the
// test can read files that live outside its own package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

func loadAppContract(t *testing.T) appContract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), appContractPath))
	if err != nil {
		t.Fatalf("read %s: %v", appContractPath, err)
	}
	contract := appContract{
		commands:     map[string]map[string]bool{},
		prefixes:     map[string]bool{},
		ignoredFlags: map[string]bool{},
	}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		switch section {
		case "commands":
			fields := strings.Fields(line)
			path, flags := []string{}, map[string]bool{}
			for _, field := range fields {
				if strings.HasPrefix(field, "--") {
					flags[field] = true
					continue
				}
				path = append(path, field)
			}
			contract.commands[strings.Join(path, " ")] = flags
		case "prefixes":
			contract.prefixes[line] = true
		case "config-keys":
			contract.configKeys = append(contract.configKeys, line)
		case "ignored-flags":
			contract.ignoredFlags[line] = true
		case "ipc-verbs":
			contract.ipcVerbs = append(contract.ipcVerbs, line)
		default:
			t.Fatalf("%s: line outside any section: %q", appContractPath, line)
		}
	}
	if len(contract.commands) == 0 {
		t.Fatalf("%s: no commands parsed", appContractPath)
	}
	return contract
}

// TestAppContractCommandsResolve is the forward pass: everything the app is
// recorded as calling must still exist, still be runnable, and still accept the
// flags the app passes.
func TestAppContractCommandsResolve(t *testing.T) {
	contract := loadAppContract(t)
	for path, flags := range contract.commands {
		tokens := strings.Fields(path)
		command, remaining, err := rootCmd.Find(tokens)
		if err != nil {
			t.Errorf("%s: %v (the app calls this; delete it from %s first)", path, err, appContractPath)
			continue
		}
		if len(remaining) > 0 {
			t.Errorf("%s: resolved only to %q, leftover %v — the command was renamed or removed",
				path, command.CommandPath(), remaining)
			continue
		}
		if want := "atm " + path; command.CommandPath() != want {
			t.Errorf("%s: resolved to %q, want %q", path, command.CommandPath(), want)
			continue
		}
		// A group prints help instead of acting. The app would get exit 0 and an
		// empty payload, which decodes as "no data" rather than an error.
		if !command.Runnable() {
			t.Errorf("%s: no longer runnable — the app would decode its help text", path)
		}
		for flag := range flags {
			name := strings.TrimPrefix(flag, "--")
			if command.Flags().Lookup(name) != nil || command.InheritedFlags().Lookup(name) != nil {
				continue
			}
			t.Errorf("%s: flag %s is gone — the app still passes it", path, flag)
		}
	}
}

func TestAppContractIPCVerbsExist(t *testing.T) {
	contract := loadAppContract(t)
	if len(contract.ipcVerbs) == 0 {
		t.Fatalf("%s: no ipc verbs recorded", appContractPath)
	}
	registeredNames := ipcServer.Names()
	registered := make(map[string]bool, len(registeredNames))
	for _, verb := range registeredNames {
		registered[verb] = true
	}
	declared := make(map[string]bool, len(contract.ipcVerbs))
	for _, verb := range contract.ipcVerbs {
		declared[verb] = true
		if !registered[verb] {
			t.Errorf("the app asks for _ipc verb %q, which no longer has a handler (known: %s)",
				verb, strings.Join(registeredNames, ", "))
		}
	}
	for _, verb := range registeredNames {
		if !declared[verb] {
			t.Errorf("registered _ipc verb %q is absent from %s and therefore has no tracked App consumer",
				verb, appContractPath)
		}
	}
}

// TestAppContractIPCVerbsMatchSwiftMethods is the reverse IPC pass. Typed
// ATMIPCMethod descriptors build their argv dynamically, so the argv scanner
// below cannot see their verbs. Keep the production Swift descriptors and the
// explicit [ipc-verbs] list in exact lockstep, including duplicate detection on
// both sides.
func TestAppContractIPCVerbsMatchSwiftMethods(t *testing.T) {
	contract := loadAppContract(t)
	contractCounts := map[string]int{}
	for _, verb := range contract.ipcVerbs {
		contractCounts[verb]++
	}

	sourceSites := map[string][]swiftIPCMethodSite{}
	for _, site := range scanSwiftIPCMethodSites(t) {
		sourceSites[site.verb] = append(sourceSites[site.verb], site)
	}

	verbs := make([]string, 0, len(contractCounts)+len(sourceSites))
	seen := map[string]bool{}
	for verb := range contractCounts {
		verbs = append(verbs, verb)
		seen[verb] = true
	}
	for verb := range sourceSites {
		if !seen[verb] {
			verbs = append(verbs, verb)
		}
	}
	sort.Strings(verbs)

	for _, verb := range verbs {
		if count := contractCounts[verb]; count > 1 {
			t.Errorf("%s records _ipc verb %q %d times; each verb must appear exactly once", appContractPath, verb, count)
		}
		if sites := sourceSites[verb]; len(sites) > 1 {
			locations := make([]string, 0, len(sites))
			for _, site := range sites {
				locations = append(locations, fmt.Sprintf("%s:%d", site.file, site.line))
			}
			t.Errorf("production Swift declares _ipc verb %q %d times (%s)", verb, len(sites), strings.Join(locations, ", "))
		}
		switch {
		case contractCounts[verb] == 0:
			site := sourceSites[verb][0]
			t.Errorf("%s:%d declares _ipc verb %q, which is not in %s", site.file, site.line, verb, appContractPath)
		case len(sourceSites[verb]) == 0:
			t.Errorf("%s records _ipc verb %q, but production Swift has no typed ATMIPCMethod descriptor for it", appContractPath, verb)
		}
	}
}

func TestAppContractConfigKeysAreSettable(t *testing.T) {
	contract := loadAppContract(t)
	settable := map[string]bool{}
	for _, key := range config.SettableKeys() {
		settable[key] = true
	}
	for _, key := range contract.configKeys {
		if !settable[key] {
			t.Errorf("config key %q is no longer settable — the app's settings screen reads and writes it", key)
		}
	}
}

var (
	// Every top level command, so the scanner only reacts to argv arrays and not
	// to any Swift array that happens to start with a string.
	appContractTopLevel = regexp.MustCompile(`^"(` + strings.Join([]string{
		"agent", "artifact", "backup", "collect", "config", "dashboard", "day",
		"diagnose", "doctor", "guard", "knowledge", "memory", "now", "quota",
		"report", "restore", "session", "stats", "sync", "todo", "version",
		"_ipc",
	}, "|") + `)"$`)
	appContractStringLiteral = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	appContractPureLiteral   = regexp.MustCompile(`^"(?:[^"\\]|\\.)*"$`)
	appContractFlagLiteral   = regexp.MustCompile(`"(--[a-z][a-z0-9-]*)"`)
)

// swiftArgvSite is one argv array literal found in the Swift sources.
type swiftArgvSite struct {
	// paths holds one entry per literal branch: a ternary in the command position
	// (`read ? "read" : "unread"`) is two calls, not one.
	paths [][]string
	flags []string
	file  string
}

// swiftIPCMethodSite is one typed ATMIPCMethod constructor in production Swift.
type swiftIPCMethodSite struct {
	verb string
	file string
	line int
}

// scanSwiftIPCMethodSites scans only app production sources. Tests often define
// example methods for decoder coverage and must not expand the App protocol.
func scanSwiftIPCMethodSites(t *testing.T) []swiftIPCMethodSite {
	t.Helper()
	root := repoRoot(t)
	sourceDir := filepath.Join(root, "app", "macos", "Sources")
	var sites []swiftIPCMethodSite
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != sourceDir && info.Name() == "Tests" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".swift") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found, err := parseSwiftIPCMethodSites(string(raw), rel)
		if err != nil {
			return err
		}
		sites = append(sites, found...)
		return nil
	})
	if err != nil {
		t.Fatalf("scan typed IPC methods under %s: %v", sourceDir, err)
	}
	if len(sites) == 0 {
		t.Fatalf("no typed ATMIPCMethod descriptors found under %s — the scanner is broken, not the app", sourceDir)
	}
	return sites
}

// parseSwiftIPCMethodSites recognizes ATMIPCMethod<...>("verb") constructors
// without relying on a single-line regex. Generic arguments may be nested and
// both the type list and constructor call may span lines. Type annotations and
// the ATMIPCMethod type declaration itself are ignored because no opening
// parenthesis follows their closing generic bracket.
func parseSwiftIPCMethodSites(source, file string) ([]swiftIPCMethodSite, error) {
	const methodName = "ATMIPCMethod"
	var sites []swiftIPCMethodSite
	for index := 0; index < len(source); {
		if next, ok, err := swiftCommentOrStringEnd(source, index); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, swiftLine(source, index), err)
		} else if ok {
			index = next
			continue
		}
		if !strings.HasPrefix(source[index:], methodName) ||
			(index > 0 && isSwiftIdentifierByte(source[index-1])) ||
			(index+len(methodName) < len(source) && isSwiftIdentifierByte(source[index+len(methodName)])) {
			index++
			continue
		}

		genericStart, err := skipSwiftTrivia(source, index+len(methodName))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, swiftLine(source, index), err)
		}
		if genericStart >= len(source) || source[genericStart] != '<' {
			index += len(methodName)
			continue
		}
		genericEnd, err := swiftGenericEnd(source, genericStart)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, swiftLine(source, index), err)
		}
		callStart, err := skipSwiftTrivia(source, genericEnd)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, swiftLine(source, index), err)
		}
		if callStart >= len(source) || source[callStart] != '(' {
			index = genericEnd
			continue
		}
		argumentStart, err := skipSwiftTrivia(source, callStart+1)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, swiftLine(source, index), err)
		}
		verb, end, ok, err := swiftStringLiteral(source, argumentStart)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, swiftLine(source, index), err)
		}
		if !ok {
			return nil, fmt.Errorf("%s:%d: typed ATMIPCMethod must use a string-literal verb so %s can track it", file, swiftLine(source, index), appContractPath)
		}
		if verb == "" || strings.Contains(verb, "\\") {
			return nil, fmt.Errorf("%s:%d: _ipc verb must be a non-empty, unescaped string literal", file, swiftLine(source, argumentStart))
		}
		sites = append(sites, swiftIPCMethodSite{verb: verb, file: file, line: swiftLine(source, index)})
		index = end
	}
	return sites, nil
}

func swiftGenericEnd(source string, start int) (int, error) {
	depth := 0
	for index := start; index < len(source); {
		if next, ok, err := swiftCommentOrStringEnd(source, index); err != nil {
			return 0, err
		} else if ok {
			index = next
			continue
		}
		switch source[index] {
		case '<':
			depth++
		case '>':
			// A function type may occur inside a generic argument.
			if index == 0 || source[index-1] != '-' {
				depth--
				if depth == 0 {
					return index + 1, nil
				}
			}
		}
		index++
	}
	return 0, fmt.Errorf("unterminated ATMIPCMethod generic argument list")
}

func skipSwiftTrivia(source string, start int) (int, error) {
	index := start
	for {
		for index < len(source) && strings.ContainsRune(" \t\r\n", rune(source[index])) {
			index++
		}
		if index >= len(source) {
			return index, nil
		}
		next, ok, err := swiftCommentEnd(source, index)
		if err != nil {
			return 0, err
		}
		if !ok {
			return index, nil
		}
		index = next
	}
}

func swiftCommentOrStringEnd(source string, start int) (int, bool, error) {
	if end, ok, err := swiftCommentEnd(source, start); ok || err != nil {
		return end, ok, err
	}
	_, end, ok, err := swiftStringLiteral(source, start)
	return end, ok, err
}

func swiftCommentEnd(source string, start int) (int, bool, error) {
	if !strings.HasPrefix(source[start:], "//") && !strings.HasPrefix(source[start:], "/*") {
		return start, false, nil
	}
	if strings.HasPrefix(source[start:], "//") {
		if end := strings.IndexByte(source[start:], '\n'); end >= 0 {
			return start + end + 1, true, nil
		}
		return len(source), true, nil
	}
	depth := 1
	for index := start + 2; index < len(source); {
		switch {
		case strings.HasPrefix(source[index:], "/*"):
			depth++
			index += 2
		case strings.HasPrefix(source[index:], "*/"):
			depth--
			index += 2
			if depth == 0 {
				return index, true, nil
			}
		default:
			index++
		}
	}
	return 0, true, fmt.Errorf("unterminated block comment")
}

// swiftStringLiteral returns the literal body without interpreting escapes.
// It supports Swift raw strings and multiline strings so examples containing
// ATMIPCMethod text do not become false production descriptors.
func swiftStringLiteral(source string, start int) (string, int, bool, error) {
	index := start
	for index < len(source) && source[index] == '#' {
		index++
	}
	hashes := source[start:index]
	if index >= len(source) || source[index] != '"' {
		return "", start, false, nil
	}
	quotes := "\""
	if strings.HasPrefix(source[index:], "\"\"\"") {
		quotes = "\"\"\""
	}
	contentStart := index + len(quotes)
	terminator := quotes + hashes
	for cursor := contentStart; cursor < len(source); {
		if strings.HasPrefix(source[cursor:], terminator) {
			return source[contentStart:cursor], cursor + len(terminator), true, nil
		}
		if hashes == "" && quotes == "\"" && source[cursor] == '\\' {
			cursor += 2
			continue
		}
		cursor++
	}
	return "", 0, true, fmt.Errorf("unterminated string literal")
}

func isSwiftIdentifierByte(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
}

func swiftLine(source string, index int) int {
	return 1 + strings.Count(source[:index], "\n")
}

func TestParseSwiftIPCMethodSites(t *testing.T) {
	source := `
// ATMIPCMethod<Ignored, Ignored>("comment.example")
let text = #"ATMIPCMethod<Ignored, Ignored>("string.example")"#

static let snapshot = ATMIPCMethod<
    Envelope<Request<Foo>>,
    Result<[String: Value]>
>(
    "day.snapshot",
    timeout: 60
)

func call<Request, Response>(_ method: ATMIPCMethod<Request, Response>) {}

static let show = ATMIPCMethod<
    (Request) -> Response,
    Dictionary<String, Array<Result>>
>("day.show")
`
	sites, err := parseSwiftIPCMethodSites(source, "Example.swift")
	if err != nil {
		t.Fatalf("parse typed methods: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("got %d typed methods (%+v), want 2", len(sites), sites)
	}
	if sites[0].verb != "day.snapshot" || sites[1].verb != "day.show" {
		t.Fatalf("verbs = %q, %q; want day.snapshot, day.show", sites[0].verb, sites[1].verb)
	}

	_, err = parseSwiftIPCMethodSites(`let dynamic = ATMIPCMethod<Request, Response>(verb)`, "Dynamic.swift")
	if err == nil || !strings.Contains(err.Error(), "string-literal verb") {
		t.Fatalf("dynamic verb error = %v, want static-contract diagnostic", err)
	}
}

// splitSwiftElements splits an array body on its top level commas, stepping over
// string literals and nested calls so that `String(limit)` and a Chinese string
// containing a comma both stay in one piece.
func splitSwiftElements(body string) []string {
	var elements []string
	var current strings.Builder
	depth, inString, escaped := 0, false, false
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inString {
			current.WriteByte(ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				elements = append(elements, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
		}
		current.WriteByte(ch)
	}
	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		elements = append(elements, trimmed)
	}
	return elements
}

// parseSwiftArgv reads the leading string literals and the flags out of one argv
// array body. Where the command path ends is decided later by
// truncateToCommand: only the command tree knows that "grok_live_quota" after
// `config get` is an argument and not a subcommand.
func parseSwiftArgv(body string) ([][]string, []string) {
	branches := [][]string{{}}
	var flags []string
	pathOpen := true
	for _, element := range splitSwiftElements(body) {
		literals := appContractStringLiteral.FindAllStringSubmatch(element, -1)
		pure := appContractPureLiteral.MatchString(element)
		if pure && strings.HasPrefix(literals[0][1], "--") {
			flags = append(flags, literals[0][1])
			continue
		}
		if pure {
			if pathOpen {
				for i := range branches {
					branches[i] = append(branches[i], literals[0][1])
				}
			}
			continue
		}
		// Not a plain literal: a variable, an interpolation, or a ternary. Only a
		// ternary between two plain values can extend the path, and anything else
		// ends it — but flags spelled inside it still count.
		values := make([]string, 0, len(literals))
		for _, literal := range literals {
			if strings.HasPrefix(literal[1], "--") {
				flags = append(flags, literal[1])
				continue
			}
			values = append(values, literal[1])
		}
		isTernary := strings.Contains(element, "?") && strings.Contains(element, ":")
		if pathOpen && isTernary && len(values) >= 2 && len(values) == len(literals) {
			expanded := make([][]string, 0, len(branches)*len(values))
			for _, branch := range branches {
				for _, value := range values {
					next := append(append([]string{}, branch...), value)
					expanded = append(expanded, next)
				}
			}
			branches = expanded
			continue
		}
		pathOpen = false
	}
	return branches, flags
}

// truncateToCommand cuts a run of literals down to the part cobra would resolve
// as a command path. Only the command tree can draw that line: after `config
// get` the next literal is a settings key, after `guard approve` it is a value,
// and neither is a subcommand.
func truncateToCommand(tokens []string) []string {
	command := rootCmd
	resolved := make([]string, 0, len(tokens))
	for _, token := range tokens {
		child := childCommand(command, token)
		if child == nil {
			break
		}
		command = child
		resolved = append(resolved, token)
	}
	return resolved
}

func childCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == name {
				return child
			}
		}
	}
	return nil
}

// scanSwiftArgvSites finds every argv array literal in the app sources.
func scanSwiftArgvSites(t *testing.T) []swiftArgvSite {
	t.Helper()
	sourceDir := filepath.Join(repoRoot(t), "app", "macos", "Sources")
	var sites []swiftArgvSite
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".swift") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(raw)
		for index := 0; index < len(source); index++ {
			if source[index] != '[' {
				continue
			}
			body, end := swiftBracketBody(source, index)
			if end < 0 {
				continue
			}
			elements := splitSwiftElements(body)
			if len(elements) == 0 || !appContractTopLevel.MatchString(elements[0]) {
				continue
			}
			paths, flags := parseSwiftArgv(body)
			sites = append(sites, swiftArgvSite{paths: paths, flags: flags, file: filepath.Base(path)})
			index = end
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", sourceDir, err)
	}
	if len(sites) == 0 {
		t.Fatalf("no argv arrays found under %s — the scanner is broken, not the app", sourceDir)
	}
	return sites
}

// swiftBracketBody returns the contents of the bracket opening at start, and the
// index of its closing bracket, ignoring brackets inside string literals.
func swiftBracketBody(source string, start int) (string, int) {
	depth, inString, escaped := 0, false, false
	for i := start; i < len(source); i++ {
		ch := source[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return source[start+1 : i], i
			}
		case '\n':
			// An unclosed bracket within a few lines is not an argv array; bail
			// rather than swallowing the rest of the file.
			if depth == 0 {
				return "", -1
			}
		}
	}
	return "", -1
}

// TestAppContractCoversSwiftCallSites is the reverse pass: a new call site in
// Swift that the list does not mention fails here, so the list cannot silently
// fall behind the app.
func TestAppContractCoversSwiftCallSites(t *testing.T) {
	contract := loadAppContract(t)
	recordedVerbs := map[string]bool{}
	for _, verb := range contract.ipcVerbs {
		recordedVerbs[verb] = true
	}
	for _, site := range scanSwiftArgvSites(t) {
		for _, path := range site.paths {
			// `_ipc <verb>` carries its target in an argument, so it is checked
			// against the recorded verbs instead of the command tree.
			if len(path) > 0 && path[0] == ipcCmd.Name() {
				if len(path) < 2 {
					t.Errorf("%s calls %s with no verb", site.file, ipcCmd.Name())
					continue
				}
				if !recordedVerbs[path[1]] {
					t.Errorf("%s asks for _ipc verb %q, which is not in %s", site.file, path[1], appContractPath)
				}
				continue
			}
			path = truncateToCommand(path)
			if len(path) == 0 {
				continue
			}
			key := strings.Join(path, " ")
			if contract.prefixes[key] {
				continue
			}
			if _, ok := contract.commands[key]; !ok {
				t.Errorf("%s calls %q, which is not in %s — add it (or add it under [prefixes] if the app concatenates onto it)",
					site.file, "atm "+key, appContractPath)
			}
		}
	}
}

// TestAppContractCoversSwiftFlags catches a flag added in Swift without being
// recorded. Attribution to a specific command is deliberately not attempted:
// several flags are appended by shared helpers far from their argv array, so the
// check is that the flag is known at all.
func TestAppContractCoversSwiftFlags(t *testing.T) {
	contract := loadAppContract(t)
	known := map[string]bool{}
	for _, flags := range contract.commands {
		for flag := range flags {
			known[flag] = true
		}
	}
	sourceDir := filepath.Join(repoRoot(t), "app", "macos", "Sources")
	missing := map[string]string{}
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".swift") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range appContractFlagLiteral.FindAllStringSubmatch(string(raw), -1) {
			flag := match[1]
			if known[flag] || contract.ignoredFlags[flag] {
				continue
			}
			if _, seen := missing[flag]; !seen {
				missing[flag] = filepath.Base(path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", sourceDir, err)
	}
	names := make([]string, 0, len(missing))
	for flag := range missing {
		names = append(names, flag)
	}
	sort.Strings(names)
	for _, flag := range names {
		t.Errorf("%s uses %s, which %s does not record (add it to the command's line, or to [ignored-flags] if it is not an atm flag)",
			missing[flag], flag, appContractPath)
	}
}
