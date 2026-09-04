package launchservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zane-byte-dev/atm/internal/executionlock"
)

const workspaceServiceOwner = "atm-serve/v1"

type workspaceServiceOptions struct {
	Home, DataDir, Executable, Path, GOOS string
	UID, Port                             int
	CheckAssets                           func() error
	Run                                   func(context.Context, ...string) ([]byte, error)
	WorkspaceRunning                      func(context.Context, string) (bool, error)
}

type workspaceServicePlan struct {
	Label     string   `json:"label"`
	PlistPath string   `json:"plist_path"`
	DataDir   string   `json:"data_dir"`
	Arguments []string `json:"arguments"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	Domain    string   `json:"domain"`
	Path      string   `json:"-"`
}

func makeWorkspaceServicePlan(opts workspaceServiceOptions, install bool) (workspaceServicePlan, error) {
	if opts.GOOS != "darwin" {
		return workspaceServicePlan{}, errors.New("workspace login services are supported on macOS only")
	}
	if install && (opts.Port < 1 || opts.Port > 65535) {
		return workspaceServicePlan{}, errors.New("a login service requires a fixed port between 1 and 65535")
	}
	home, err := canonicalServicePath(opts.Home)
	if err != nil {
		return workspaceServicePlan{}, err
	}
	dataDir, err := canonicalServicePath(opts.DataDir)
	if err != nil {
		return workspaceServicePlan{}, err
	}
	executable, err := canonicalServicePath(opts.Executable)
	if err != nil {
		return workspaceServicePlan{}, err
	}
	if install {
		info, err := os.Stat(executable)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return workspaceServicePlan{}, errors.New("the current CLI must be a regular executable file at an absolute path")
		}
		if opts.CheckAssets == nil {
			return workspaceServicePlan{}, errors.New("Web asset validation is required")
		}
		if err := opts.CheckAssets(); err != nil {
			return workspaceServicePlan{}, err
		}
	}
	hash := sha256.Sum256([]byte(dataDir))
	label := "com.atm.workspace." + hex.EncodeToString(hash[:8])
	plan := workspaceServicePlan{Label: label, PlistPath: filepath.Join(home, "Library", "LaunchAgents", label+".plist"), DataDir: dataDir,
		Arguments: []string{executable, "serve", "--background", "--data-dir", dataDir, "--port", strconv.Itoa(opts.Port)},
		Stdout:    filepath.Join(dataDir, "runtime", "serve.stdout.log"), Stderr: filepath.Join(dataDir, "runtime", "serve.stderr.log"), Domain: fmt.Sprintf("gui/%d", opts.UID), Path: workspaceServicePath(opts.Path, home)}
	return plan, nil
}

// Canonicalize the existing prefix without creating a data directory during a
// --print run. This also handles macOS /var and /tmp aliases consistently.
func canonicalServicePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("service paths must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	prefix := absolute
	tail := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(prefix)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return "", err
		}
		tail = append(tail, filepath.Base(prefix))
		prefix = parent
	}
}

func workspaceServicePath(value, home string) string {
	result := []string{}
	seen := map[string]bool{}
	for _, entry := range append(filepath.SplitList(value), filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin") {
		if !filepath.IsAbs(entry) {
			continue
		}
		entry = filepath.Clean(entry)
		if !seen[entry] {
			result = append(result, entry)
			seen[entry] = true
		}
	}
	return strings.Join(result, string(os.PathListSeparator))
}

func renderWorkspaceService(plan workspaceServicePlan) ([]byte, error) {
	var body bytes.Buffer
	body.WriteString(xml.Header + "<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n")
	text := func(tag, value string) {
		body.WriteString("<" + tag + ">")
		_ = xml.EscapeText(&body, []byte(value))
		body.WriteString("</" + tag + ">\n")
	}
	pair := func(key, value string) { text("key", key); text("string", value) }
	pair("Label", plan.Label)
	pair("ATMManagedBy", workspaceServiceOwner)
	pair("ATMDataDirectory", plan.DataDir)
	text("key", "ProgramArguments")
	body.WriteString("<array>\n")
	for _, arg := range plan.Arguments {
		text("string", arg)
	}
	body.WriteString("</array>\n")
	pair("WorkingDirectory", plan.DataDir)
	pair("StandardOutPath", plan.Stdout)
	pair("StandardErrorPath", plan.Stderr)
	text("key", "EnvironmentVariables")
	body.WriteString("<dict>\n")
	pair("PATH", plan.Path)
	body.WriteString("</dict>\n")
	body.WriteString("<key>RunAtLoad</key><true/>\n<key>KeepAlive</key><true/>\n<key>ThrottleInterval</key><integer>10</integer>\n<key>ExitTimeOut</key><integer>45</integer>\n<key>Umask</key><integer>63</integer>\n<key>ProcessType</key><string>Background</string>\n</dict>\n</plist>\n")
	return body.Bytes(), nil
}

type serviceXMLNode struct {
	XMLName  xml.Name
	Text     string           `xml:",chardata"`
	Children []serviceXMLNode `xml:",any"`
}

func ownedWorkspaceService(data []byte, plan workspaceServicePlan) bool {
	if len(data) > 64<<10 {
		return false
	}
	var root serviceXMLNode
	if xml.Unmarshal(data, &root) != nil || root.XMLName.Local != "plist" || len(root.Children) != 1 || root.Children[0].XMLName.Local != "dict" {
		return false
	}
	entries := root.Children[0].Children
	if len(entries)%2 != 0 {
		return false
	}
	values := map[string]serviceXMLNode{}
	for i := 0; i < len(entries); i += 2 {
		if entries[i].XMLName.Local != "key" {
			return false
		}
		key := entries[i].Text
		if _, exists := values[key]; exists {
			return false
		}
		values[key] = entries[i+1]
	}
	for key, want := range map[string]string{"Label": plan.Label, "ATMManagedBy": workspaceServiceOwner, "ATMDataDirectory": plan.DataDir} {
		if values[key].XMLName.Local != "string" || values[key].Text != want {
			return false
		}
	}
	return true
}

func readManagedWorkspaceService(plan workspaceServicePlan) ([]byte, error) {
	if err := checkServiceDirectories(filepath.Dir(plan.PlistPath), false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(plan.PlistPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("refusing a symlink or non-regular LaunchAgent")
	}
	data, err := os.ReadFile(plan.PlistPath)
	if err != nil {
		return nil, err
	}
	if !ownedWorkspaceService(data, plan) {
		return nil, errors.New("refusing to replace or remove an unrelated LaunchAgent")
	}
	return data, nil
}

func checkServiceDirectories(path string, create bool) error {
	parent := filepath.Dir(path)
	if parent != path {
		if err := checkServiceDirectories(parent, create); err != nil {
			return err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil
		}
		return os.Mkdir(path, 0700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("service directory must not be a symlink or non-directory: %s", path)
	}
	return nil
}

func workspaceServiceLoaded(ctx context.Context, opts workspaceServiceOptions, plan workspaceServicePlan) (bool, error) {
	output, err := opts.Run(ctx, "print", plan.Domain+"/"+plan.Label)
	if err == nil {
		return true, nil
	}
	if strings.Contains(string(output), "Could not find service") || strings.Contains(string(output), "Could not find specified service") {
		return false, nil
	}
	return false, fmt.Errorf("inspect ATM LaunchAgent: %w (%s)", err, strings.TrimSpace(string(output)))
}

func runWorkspaceLaunchctl(ctx context.Context, opts workspaceServiceOptions, args ...string) error {
	output, err := opts.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("launchctl %s failed: %w (%s)", args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

func lockWorkspaceService(ctx context.Context, plan workspaceServicePlan) (*executionlock.Lock, error) {
	directory := filepath.Join(plan.DataDir, "runtime", "locks")
	if err := checkServiceDirectories(directory, false); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(filepath.Join(directory, "workspace-service.lock")); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("service lock must not be a symlink or non-regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return executionlock.Acquire(ctx, plan.DataDir, "workspace-service")
}

func installWorkspaceService(ctx context.Context, opts workspaceServiceOptions, plan workspaceServicePlan, plist []byte) error {
	// Inspect paths before lock acquisition or any file creation.
	if err := checkServiceDirectories(filepath.Dir(plan.Stdout), false); err != nil {
		return err
	}
	previous, err := readManagedWorkspaceService(plan)
	if err != nil {
		return err
	}
	lock, err := lockWorkspaceService(ctx, plan)
	if err != nil {
		return err
	}
	defer lock.Close()
	previous, err = readManagedWorkspaceService(plan)
	if err != nil {
		return err
	}
	loaded, err := workspaceServiceLoaded(ctx, opts, plan)
	if err != nil {
		return err
	}
	if previous == nil && loaded {
		return errors.New("an existing launchd job already uses this label; refusing to replace an unverified service")
	}
	if !loaded && opts.WorkspaceRunning != nil {
		running, err := opts.WorkspaceRunning(ctx, plan.DataDir)
		if err != nil {
			return err
		}
		if running {
			return errors.New("an unmanaged workspace is already running; stop it with atm serve stop before installing the login service")
		}
	}
	if err := checkServiceDirectories(filepath.Dir(plan.PlistPath), true); err != nil {
		return err
	}
	if err := checkServiceDirectories(filepath.Dir(plan.Stdout), true); err != nil {
		return err
	}
	for _, path := range []string{plan.Stdout, plan.Stderr} {
		if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
			return errors.New("service log must be a regular file")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = file.Chmod(0600)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	changed := !bytes.Equal(previous, plist)
	if loaded && !changed {
		return os.Chmod(plan.PlistPath, 0600)
	}
	if loaded && changed {
		if err := runWorkspaceLaunchctl(ctx, opts, "bootout", plan.Domain+"/"+plan.Label); err != nil {
			return err
		}
		loaded = false
	}
	if changed {
		if err := writeWorkspaceServicePlist(plan, plist, previous == nil); err != nil {
			return err
		}
	} else if err := os.Chmod(plan.PlistPath, 0600); err != nil {
		return err
	}
	if !loaded {
		if err := runWorkspaceLaunchctl(ctx, opts, "bootstrap", plan.Domain, plan.PlistPath); err != nil {
			return err
		}
	}
	// No -k: an identical install leaves the running process intact.
	return runWorkspaceLaunchctl(ctx, opts, "kickstart", plan.Domain+"/"+plan.Label)
}

func writeWorkspaceServicePlist(plan workspaceServicePlan, data []byte, exclusive bool) error {
	file, err := os.CreateTemp(filepath.Dir(plan.PlistPath), ".atm-service-*.plist")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if exclusive {
		return os.Link(file.Name(), plan.PlistPath)
	}
	if _, err := readManagedWorkspaceService(plan); err != nil {
		return err
	}
	return os.Rename(file.Name(), plan.PlistPath)
}

func uninstallWorkspaceService(ctx context.Context, opts workspaceServiceOptions, plan workspaceServicePlan) error {
	previous, err := readManagedWorkspaceService(plan)
	if err != nil || previous == nil {
		return err
	}
	if err := checkServiceDirectories(filepath.Dir(plan.Stdout), false); err != nil {
		return err
	}
	lock, err := lockWorkspaceService(ctx, plan)
	if err != nil {
		return err
	}
	defer lock.Close()
	previous, err = readManagedWorkspaceService(plan)
	if err != nil || previous == nil {
		return err
	}
	loaded, err := workspaceServiceLoaded(ctx, opts, plan)
	if err != nil {
		return err
	}
	if loaded {
		if err := runWorkspaceLaunchctl(ctx, opts, "bootout", plan.Domain+"/"+plan.Label); err != nil {
			return err
		}
	}
	// Only the verified plist is removed. The Go owner's durable marker must
	// survive so a legacy App cannot silently reclaim workers or Agent hooks.
	if _, err := readManagedWorkspaceService(plan); err != nil {
		return err
	}
	return os.Remove(plan.PlistPath)
}

func stopWorkspaceService(ctx context.Context, opts workspaceServiceOptions, plan workspaceServicePlan) (bool, error) {
	previous, err := readManagedWorkspaceService(plan)
	if err != nil || previous == nil {
		return false, err
	}
	lock, err := lockWorkspaceService(ctx, plan)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	previous, err = readManagedWorkspaceService(plan)
	if err != nil || previous == nil {
		return false, err
	}
	loaded, err := workspaceServiceLoaded(ctx, opts, plan)
	if err != nil || !loaded {
		return false, err
	}
	return true, runWorkspaceLaunchctl(ctx, opts, "bootout", plan.Domain+"/"+plan.Label)
}

// Options and Plan are the composition contract used by command adapters.
type Options = workspaceServiceOptions
type Plan = workspaceServicePlan

const Owner = workspaceServiceOwner

func Canonical(path string) (string, error) { return canonicalServicePath(path) }
func MakePlan(opts Options, install bool) (Plan, error) {
	return makeWorkspaceServicePlan(opts, install)
}
func Render(plan Plan) ([]byte, error) { return renderWorkspaceService(plan) }
func Owned(data []byte, plan Plan) bool { return ownedWorkspaceService(data, plan) }
func Install(ctx context.Context, opts Options, plan Plan, plist []byte) error {
	return installWorkspaceService(ctx, opts, plan, plist)
}
func Uninstall(ctx context.Context, opts Options, plan Plan) error {
	return uninstallWorkspaceService(ctx, opts, plan)
}
func Stop(ctx context.Context, opts Options, plan Plan) (bool, error) {
	return stopWorkspaceService(ctx, opts, plan)
}
