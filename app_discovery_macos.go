package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type macInventoryRunner func(context.Context, string, ...string) ([]byte, error)

type macAppScanner struct {
	ctx       context.Context
	run       macInventoryRunner
	locations applicationLocations
	bundles   map[string]bool
	packages  map[string]bool
}

// Inventory only: never execute discovered apps, CLI wrappers, npm, or a login
// shell. Finder's PATH need not contain the user's Homebrew/npm/native CLI bins.
func discoverMacApplications(ctx context.Context, home, path string, prefixes []string, run macInventoryRunner) (applicationLocations, error) {
	s := macAppScanner{ctx: ctx, run: run, locations: applicationLocations{Executables: map[string][]string{}}, bundles: map[string]bool{}, packages: map[string]bool{}}
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications"), filepath.Join(home, "Downloads")} {
		for _, name := range []string{"Codex.app", "ChatGPT.app", "Claude.app"} {
			s.bundle(filepath.Join(dir, name))
		}
	}
	// Bundle identity survives renaming or moving an app. Spotlight can be
	// disabled; the standard locations and running-process fallback still work.
	data, _ := run(ctx, "/usr/bin/mdfind", "-0", `(kMDItemCFBundleIdentifier == 'com.openai.codex') || (kMDItemCFBundleIdentifier == 'com.anthropic.claudefordesktop')`)
	for _, path := range strings.Split(string(data), "\x00") {
		if filepath.IsAbs(path) {
			s.bundle(path)
		}
	}
	bins := filepath.SplitList(path)
	for _, prefix := range append([]string{home, filepath.Join(home, ".local"), filepath.Join(home, ".codex"), filepath.Join(home, ".npm-global"), filepath.Join(home, ".npm"), filepath.Join(home, ".bun"), filepath.Join(home, ".volta")}, prefixes...) {
		bins = append(bins, filepath.Join(prefix, "bin"))
		s.nodeModules(filepath.Join(prefix, "lib", "node_modules"))
	}
	for _, pattern := range []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin"),
	} {
		matches, _ := filepath.Glob(pattern)
		bins = append(bins, matches...)
	}
	seen := map[string]bool{}
	for _, dir := range bins {
		if !filepath.IsAbs(dir) || seen[dir] {
			continue
		}
		seen[dir] = true
		for _, name := range []string{"codex", "codex-aarch64-apple-darwin", "codex-x86_64-apple-darwin", "claude"} {
			s.cli(filepath.Join(dir, name))
		}
		s.nodeModules(filepath.Join(filepath.Dir(dir), "lib", "node_modules"))
	}
	s.nodeModules(filepath.Join(home, ".bun", "install", "global", "node_modules"))
	// Standalone GitHub downloads retain their architecture-qualified name.
	// Limit this scan to Codex/Claude entries, not the user's whole home tree.
	entries, _ := os.ReadDir(filepath.Join(home, "Downloads"))
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if (strings.HasPrefix(name, "codex") || strings.HasPrefix(name, "claude")) && !strings.HasSuffix(name, ".app") {
			s.walk(filepath.Join(home, "Downloads", entry.Name()), 3, func(path string) { s.cli(path) })
		}
	}
	// comm reports executable paths without collecting prompts, credentials, or
	// other process arguments. -ww prevents truncation of paths containing spaces.
	data, _ = run(ctx, "/bin/ps", "-axww", "-o", "comm=")
	known := map[string]bool{}
	for _, paths := range s.locations.Executables {
		for _, path := range paths {
			known[path] = true
		}
	}
	for _, path := range strings.Split(string(data), "\n") {
		path = strings.TrimSpace(path)
		resolved, _ := filepath.EvalSymlinks(path)
		if !filepath.IsAbs(path) || (!known[resolved] && !macAppExecutable(filepath.Base(path))) {
			continue
		}
		s.locations.RunningPaths = append(s.locations.RunningPaths, path)
		if bundle := macBundleRoot(path); bundle != "" {
			s.bundle(bundle)
		} else {
			s.cli(path)
		}
	}
	if ctx.Err() != nil {
		return s.locations, fmt.Errorf("应用扫描超时或已取消，已保留本次识别结果：%w", ctx.Err())
	}
	return s.locations, nil
}

func (s *macAppScanner) bundle(root string) {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if s.ctx.Err() != nil || s.bundles[root] {
		return
	}
	s.bundles[root] = true
	plist := filepath.Join(root, "Contents", "Info.plist")
	if info, err := os.Stat(plist); err != nil || !info.Mode().IsRegular() {
		return
	}
	data, err := s.run(s.ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", plist)
	if err != nil {
		return
	}
	var metadata struct {
		ID         string `json:"CFBundleIdentifier"`
		Executable string `json:"CFBundleExecutable"`
	}
	if json.Unmarshal(data, &metadata) != nil {
		return
	}
	id := ""
	switch metadata.ID {
	case "com.openai.codex":
		id = "codex-desktop"
	case "com.anthropic.claudefordesktop":
		id = "claude-desktop"
	default:
		return
	}
	// Add the declared main binary before bounded resource scans. Large bundles
	// must not lose their entry point after walking thousands of framework files.
	if metadata.Executable != "" && filepath.Base(metadata.Executable) == metadata.Executable {
		s.native(id, filepath.Join(root, "Contents", "MacOS", metadata.Executable))
	}
	for _, dir := range []string{filepath.Join(root, "Contents", "MacOS"), filepath.Join(root, "Contents", "Resources")} {
		s.walk(dir, 8, func(path string) {
			if macExecutableForApp(id, filepath.Base(path)) {
				s.native(id, path)
			}
		})
	}
	helpers, _ := filepath.Glob(filepath.Join(root, "Contents", "Frameworks", "*.app", "Contents", "MacOS", "*"))
	for _, path := range helpers {
		if macExecutableForApp(id, filepath.Base(path)) {
			s.native(id, path)
		}
	}
}

func (s *macAppScanner) cli(path string) {
	if s.ctx.Err() != nil || macBundleRoot(path) != "" {
		return
	}
	id := macCLIIdentity(filepath.Base(path))
	if id == "" {
		return
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return
	}
	// npm entry points are JS launchers. Find their own native dependency;
	// never bind node/bun/python or execute the launcher to discover it.
	if macSharedRuntime(filepath.Base(resolved)) {
		return
	}
	s.native(id, resolved)
	for dir, depth := filepath.Dir(resolved), 0; depth < 6; dir, depth = filepath.Dir(dir), depth+1 {
		if filepath.Base(dir) == "node_modules" {
			s.nodeModules(dir)
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
}

func (s *macAppScanner) nodeModules(root string) {
	if s.ctx.Err() != nil || s.packages[root] {
		return
	}
	s.packages[root] = true
	for _, pkg := range []struct{ pattern, id string }{
		{filepath.Join("@openai", "codex"), "codex-cli"},
		{filepath.Join("@openai", "codex-darwin-*"), "codex-cli"},
		{filepath.Join("@anthropic-ai", "claude-code"), "claude-cli"},
	} {
		matches, _ := filepath.Glob(filepath.Join(root, pkg.pattern))
		for _, path := range matches {
			s.walk(path, 12, func(file string) {
				if macCLIIdentity(filepath.Base(file)) == pkg.id {
					s.native(pkg.id, file)
				}
			})
		}
	}
}

func (s *macAppScanner) native(id, path string) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || macSharedRuntime(filepath.Base(resolved)) || !macNativeExecutable(resolved) {
		return
	}
	s.locations.Executables[id] = append(s.locations.Executables[id], resolved)
}

func (s *macAppScanner) walk(root string, depth int, visit func(string)) {
	if target, err := filepath.EvalSymlinks(root); err == nil {
		root = target
	}
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if s.ctx.Err() != nil || count >= 6000 {
			return fs.SkipAll
		}
		if err != nil {
			return nil
		}
		count++
		if entry.IsDir() {
			rel, _ := filepath.Rel(root, path)
			if strings.Count(rel, string(filepath.Separator)) > depth {
				return fs.SkipDir
			}
			return nil
		}
		visit(path)
		return nil
	})
}

func macBundleRoot(path string) string {
	root := ""
	for dir := filepath.Dir(path); filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if strings.HasSuffix(strings.ToLower(dir), ".app") {
			root = dir
		}
	}
	return root
}

func macCLIIdentity(name string) string {
	switch strings.ToLower(name) {
	case "codex", "codex-aarch64-apple-darwin", "codex-x86_64-apple-darwin":
		return "codex-cli"
	case "claude":
		return "claude-cli"
	}
	return ""
}

func macExecutableForApp(id, name string) bool {
	return appExecutable(id, name) || (id == "codex-desktop" && strings.HasPrefix(strings.ToLower(name), "chatgpt helper"))
}

func macAppExecutable(name string) bool {
	return macCLIIdentity(name) != "" || macExecutableForApp("codex-desktop", name) || macExecutableForApp("claude-desktop", name)
}

func macSharedRuntime(name string) bool {
	switch strings.ToLower(name) {
	case "node", "nodejs", "bun", "sh", "bash", "zsh", "python", "python3":
		return true
	}
	return false
}

func macNativeExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	switch binary.BigEndian.Uint32(magic[:]) {
	case 0xfeedface, 0xcefaedfe, 0xfeedfacf, 0xcffaedfe, 0xcafebabe, 0xbebafeca, 0xcafebabf, 0xbfbafeca:
		return true
	}
	return false
}
