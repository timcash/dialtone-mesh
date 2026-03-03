package meshv1

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	configv1 "dialtone/dev/plugins/config/src_v1/go"
	logs "dialtone/dev/plugins/logs/src_v1/go"
	sshv1 "dialtone/dev/plugins/ssh/src_v1/go"
)

const defaultVersion = "v1"

type Paths struct {
	Runtime    configv1.Runtime
	Version    string
	VersionDir string
	LibudxDir  string
	SourceC    string
	BinDir     string
	BinAMD64   string
	BinARM64   string
}

func Run(args []string) error {
	version, command, rest, warnedOldOrder, err := parseArgs(args)
	if err != nil {
		printUsage()
		return err
	}
	if warnedOldOrder {
		logs.Warn("old mesh CLI order is deprecated. Use: ./dialtone.sh mesh v1 <command> [args]")
	}
	if version != defaultVersion {
		return fmt.Errorf("unsupported mesh version %s (expected %s)", version, defaultVersion)
	}

	paths, err := resolvePaths("")
	if err != nil {
		return err
	}
	_ = configv1.LoadEnvFile(paths.Runtime)
	_ = configv1.ApplyRuntimeEnv(paths.Runtime)

	switch command {
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "install":
		return runInstall(paths, rest)
	case "format":
		return runFormat(paths, rest)
	case "lint":
		return runLint(paths, rest)
	case "build":
		return runBuild(paths, rest)
	case "test":
		return runTest(paths, rest)
	case "deploy":
		return runDeploy(paths, rest)
	case "start":
		return runStart(paths, rest)
	case "join":
		return runJoin(paths, rest)
	case "shell-server":
		return runShellServer(paths, rest)
	default:
		printUsage()
		return fmt.Errorf("unknown mesh command: %s", command)
	}
}

func parseArgs(args []string) (version, command string, rest []string, warnedOldOrder bool, err error) {
	if len(args) == 0 {
		return defaultVersion, "help", nil, false, nil
	}
	if isHelp(args[0]) {
		return defaultVersion, "help", nil, false, nil
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "v1") {
		if len(args) < 2 {
			return "", "", nil, false, fmt.Errorf("missing command (usage: ./dialtone.sh mesh v1 <command> [args])")
		}
		return defaultVersion, args[1], args[2:], false, nil
	}
	if len(args) >= 2 && strings.EqualFold(strings.TrimSpace(args[1]), "v1") {
		return defaultVersion, args[0], args[2:], true, nil
	}
	return "", "", nil, false, fmt.Errorf("expected version as first mesh argument (usage: ./dialtone.sh mesh v1 <command> [args])")
}

func isHelp(v string) bool {
	switch strings.TrimSpace(v) {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

func printUsage() {
	logs.Raw("Usage: ./dialtone.sh mesh v1 <command> [args]")
	logs.Raw("")
	logs.Raw("Commands:")
	logs.Raw("  install [--skip-apt] [--with-arm64=true|false]")
	logs.Raw("          Install mesh/libudx build dependencies")
	logs.Raw("  format")
	logs.Raw("          Format Go sources for mesh v1")
	logs.Raw("  lint")
	logs.Raw("          Run static checks for mesh v1")
	logs.Raw("  build [--arch host|x86_64|arm64|all]")
	logs.Raw("          Build mesh C binary linked to libudx")
	logs.Raw("  test [--mode local|all]")
	logs.Raw("          Run mesh self-tests")
	logs.Raw("  deploy [--host <name|all|local>] [--with-install] [--dry-run]")
	logs.Raw("         [--bind-ip 0.0.0.0] [--bind-port 19001] [--peer-ip IP] [--peer-port P] [--no-send=true|false]")
	logs.Raw("          Deploy/start mesh on target host(s)")
	logs.Raw("  start [--bind-ip 0.0.0.0] [--bind-port 19001] [--peer-ip IP] [--peer-port P]")
	logs.Raw("        [--no-send=true|false] [--count N] [--interval-ms N] [--exit-after-ms N] [--foreground]")
	logs.Raw("          Start local mesh runtime (default: background)")
	logs.Raw("  join [--host <name|all|local>] [--repo-dir PATH] [--skip-self=true|false] [--with-install]")
	logs.Raw("       [--bind-ip 0.0.0.0] [--bind-port 19001] [--peer-ip IP] [--peer-port P] [--no-send=true|false]")
	logs.Raw("          Build and start mesh on local/remote host(s) using mesh mod itself")
	logs.Raw("  shell-server [--host <name|all|local>] [--repo-dir PATH] [--skip-self=true|false] [--with-build]")
	logs.Raw("               [--bind-ip 0.0.0.0] [--http-port 8787] [--script-path PATH] [--foreground] [--dry-run]")
	logs.Raw("          Serve dialtone bootstrap script via mesh binary (C HTTP mode)")
	logs.Raw("")
	logs.Raw("Examples:")
	logs.Raw("  ./dialtone.sh mesh v1 join")
	logs.Raw("  ./dialtone.sh mesh v1 join --host all --skip-self=true")
	logs.Raw("  ./dialtone.sh mesh v1 build --arch host")
	logs.Raw("  ./dialtone.sh mesh v1 shell-server --http-port 8787")
}

func resolvePaths(start string) (Paths, error) {
	rt, err := configv1.ResolveRuntime(start)
	if err != nil {
		return Paths{}, err
	}
	versionDir := filepath.Join(rt.SrcRoot, "mods", "mesh", "v1")
	binDir := filepath.Join(versionDir, "bin")
	return Paths{
		Runtime:    rt,
		Version:    "v1",
		VersionDir: versionDir,
		LibudxDir:  filepath.Join(versionDir, "libudx"),
		SourceC:    filepath.Join(versionDir, "mesh_v1.c"),
		BinDir:     binDir,
		BinAMD64:   filepath.Join(binDir, "dialtone_mesh_v1_x86_64"),
		BinARM64:   filepath.Join(binDir, "dialtone_mesh_v1_arm64"),
	}, nil
}

func runInstall(paths Paths, args []string) error {
	fs := flag.NewFlagSet("mesh-install", flag.ContinueOnError)
	skipApt := fs.Bool("skip-apt", false, "Skip apt package install")
	withARM64 := fs.Bool("with-arm64", true, "Install ARM64 cross compiler deps")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if runtime.GOOS == "linux" && !*skipApt {
		pkgs := []string{
			"curl", "git", "build-essential", "cmake", "ninja-build",
			"clang", "lld", "libuv1-dev", "libuv1", "pkg-config", "python3",
			"nodejs", "npm",
		}
		if *withARM64 {
			pkgs = append(pkgs, "gcc-aarch64-linux-gnu", "g++-aarch64-linux-gnu", "binutils-aarch64-linux-gnu")
		}
		if err := runCmd("", "sudo", "apt-get", "update"); err != nil {
			return err
		}
		installArgs := append([]string{"apt-get", "install", "-y"}, pkgs...)
		if err := runCmd("", "sudo", installArgs...); err != nil {
			return err
		}
	}

	if _, err := exec.LookPath("bare-make"); err != nil {
		if err := runCmd("", "sudo", "npm", "install", "-g", "bare-runtime", "bare-make"); err != nil {
			return err
		}
	}

	if err := ensureLibudx(paths); err != nil {
		return err
	}
	if err := runCmd(paths.LibudxDir, "npm", "install"); err != nil {
		return err
	}
	logs.Info("mesh v1 install complete")
	return nil
}

func runFormat(paths Paths, _ []string) error {
	return runCmd(paths.VersionDir, "gofmt", "-w", filepath.Join(paths.VersionDir, "go", "mesh.go"))
}

func runLint(paths Paths, _ []string) error {
	return runCmd(paths.Runtime.SrcRoot, goBin(paths.Runtime), "vet", "./mods/mesh/v1/go")
}

func runBuild(paths Paths, args []string) error {
	fs := flag.NewFlagSet("mesh-build", flag.ContinueOnError)
	arch := fs.String("arch", "host", "host|x86_64|arm64|all")
	host := fs.String("host", "local", "Target host: local, mesh node, or all")
	repoDir := fs.String("repo-dir", "", "Remote repo dir override")
	skipSelf := fs.Bool("skip-self", true, "When --host all, skip current local mesh node")
	withInstall := fs.Bool("with-install", false, "Run mesh install before build on target")
	dryRun := fs.Bool("dry-run", false, "Print remote commands without executing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := strings.ToLower(strings.TrimSpace(*host))
	if target != "" && target != "local" {
		return runBuildRemote(paths, target, strings.TrimSpace(*arch), strings.TrimSpace(*repoDir), *skipSelf, *withInstall, *dryRun)
	}
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		return err
	}
	if err := ensureLibudx(paths); err != nil {
		return err
	}
	if err := buildLibudxNative(paths); err != nil {
		return err
	}
	for _, target := range expandArch(*arch) {
		switch target {
		case "x86_64":
			if err := buildAMD64(paths); err != nil {
				return err
			}
		case "arm64":
			if err := buildARM64(paths); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported arch %s", target)
		}
	}
	return nil
}

func runBuildRemote(paths Paths, host, arch, repoDir string, skipSelf, withInstall, dryRun bool) error {
	runNode := func(node sshv1.MeshNode) error {
		if skipSelf && host == "all" && isSelfMeshNode(node) {
			logs.Raw("== %s ==\nSKIP self node", node.Name)
			return nil
		}
		rd := strings.TrimSpace(repoDir)
		if rd == "" {
			rd = defaultRepoDirForNode(node)
		}
		parts := []string{"set -e", "cd " + shellQuote(rd)}
		if withInstall {
			parts = append(parts, buildRemoteNixInstallCommand(rd))
		}
		parts = append(parts, buildRemoteNixBuildCommand(rd, arch))
		cmd := strings.Join(parts, " && ")
		logs.Raw("== %s ==", node.Name)
		if dryRun {
			logs.Raw("[DRY-RUN] %s", cmd)
			return nil
		}
		out, err := sshv1.RunNodeCommand(node.Name, cmd, sshv1.CommandOptions{})
		if strings.TrimSpace(out) != "" {
			logs.Raw("%s", strings.TrimRight(out, "\n"))
		}
		return err
	}

	if host == "all" {
		failed := 0
		for _, n := range sshv1.ListMeshNodes() {
			if err := runNode(n); err != nil {
				failed++
				logs.Raw("ERROR: %v", err)
			}
		}
		if failed > 0 {
			return fmt.Errorf("build finished with %d host failures", failed)
		}
		return nil
	}

	node, err := sshv1.ResolveMeshNode(host)
	if err != nil {
		return err
	}
	return runNode(node)
}

func buildRemoteNixInstallCommand(repoDir string) string {
	inner := strings.Join([]string{
		"set -e",
		"cd " + shellQuote(filepath.ToSlash(filepath.Join(repoDir, "src/mods/mesh/v1/libudx"))),
		"npm install",
	}, " && ")
	return strings.Join([]string{
		"if [ -f \"$HOME/.nix-profile/etc/profile.d/nix.sh\" ]; then . \"$HOME/.nix-profile/etc/profile.d/nix.sh\"; fi",
		"if [ -f /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh ]; then . /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh; fi",
		"command -v nix >/dev/null 2>&1",
		"nix --extra-experimental-features 'nix-command flakes' develop " + shellQuote("path:"+filepath.ToSlash(repoDir)) + " --command bash -lc " + shellQuote(inner),
	}, " && ")
}

func buildRemoteNixBuildCommand(repoDir, arch string) string {
	archTarget := strings.ToLower(strings.TrimSpace(arch))
	if archTarget == "" {
		archTarget = "host"
	}
	inner := strings.Join([]string{
		"set -e",
		"cd " + shellQuote(filepath.ToSlash(filepath.Join(repoDir, "src/mods/mesh/v1"))),
		"mkdir -p bin",
		"cd libudx",
		"npm install",
		"npx bare-make generate",
		"npx bare-make build",
		"cd ..",
		"UDX_LIB=$(find libudx/build -name libudx.a | head -n1)",
		"UV_LIB=$(find libudx/build -name libuv.a | head -n1)",
		"if [ -z \"$UDX_LIB\" ] || [ -z \"$UV_LIB\" ]; then echo DIALTONE_MESH_BUILD_MISSING_LIBS; exit 1; fi",
		"EXTRA_LIBS='-lpthread -ldl -lm'",
		"if [ \"$(uname -s)\" = \"Linux\" ]; then EXTRA_LIBS='-pthread -ldl -lrt -lm'; fi",
		"OUT=bin/dialtone_mesh_v1_x86_64",
		"if [ " + shellQuote(archTarget) + " = arm64 ]; then OUT=bin/dialtone_mesh_v1_arm64; fi",
		"if [ " + shellQuote(archTarget) + " = host ] && [ \"$(uname -m)\" = \"arm64\" ]; then OUT=bin/dialtone_mesh_v1_arm64; fi",
		"if [ " + shellQuote(archTarget) + " = host ] && [ \"$(uname -m)\" = \"aarch64\" ]; then OUT=bin/dialtone_mesh_v1_arm64; fi",
		"cc mesh_v1.c -Os -s -Wall -Wextra -Ilibudx/include -Ilibudx/build/_deps/github+libuv+libuv-src/include \"$UDX_LIB\" \"$UV_LIB\" $EXTRA_LIBS -o \"$OUT\"",
		"echo DIALTONE_MESH_BUILD_OK:$OUT",
	}, " && ")
	return strings.Join([]string{
		"if [ -f \"$HOME/.nix-profile/etc/profile.d/nix.sh\" ]; then . \"$HOME/.nix-profile/etc/profile.d/nix.sh\"; fi",
		"if [ -f /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh ]; then . /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh; fi",
		"command -v nix >/dev/null 2>&1",
		"nix --extra-experimental-features 'nix-command flakes' develop " + shellQuote("path:"+filepath.ToSlash(repoDir)) + " --command bash -lc " + shellQuote(inner),
	}, " && ")
}

func runTest(paths Paths, args []string) error {
	fs := flag.NewFlagSet("mesh-test", flag.ContinueOnError)
	mode := fs.String("mode", "all", "local|all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "local", "all":
		if _, err := ensureHostBinary(paths); err != nil {
			return err
		}
		bin := paths.BinAMD64
		if runtime.GOARCH == "arm64" {
			bin = paths.BinARM64
		}
		return runLocalSelfTest(bin)
	default:
		return fmt.Errorf("unsupported test mode %s (expected local|all)", *mode)
	}
}

func runDeploy(paths Paths, args []string) error {
	fs := flag.NewFlagSet("mesh-deploy", flag.ContinueOnError)
	host := fs.String("host", "all", "Target host: local, mesh node, or all")
	repoDir := fs.String("repo-dir", "", "Remote repo dir override")
	skipSelf := fs.Bool("skip-self", true, "When --host all, skip current local mesh node")
	withInstall := fs.Bool("with-install", false, "Run mesh install before build/start")
	dryRun := fs.Bool("dry-run", false, "Print remote commands without executing")
	bindIP := fs.String("bind-ip", "0.0.0.0", "Local bind IP")
	bindPort := fs.Int("bind-port", 19001, "Local bind UDP port")
	peerIP := fs.String("peer-ip", "", "Peer IP")
	peerPort := fs.Int("peer-port", 0, "Peer port")
	noSend := fs.Bool("no-send", true, "Run in receive/listen mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := strings.ToLower(strings.TrimSpace(*host))
	if target == "" || target == "local" {
		localArgs := []string{
			"--bind-ip", strings.TrimSpace(*bindIP),
			"--bind-port", strconv.Itoa(*bindPort),
			fmt.Sprintf("--no-send=%t", *noSend),
		}
		if strings.TrimSpace(*peerIP) != "" {
			localArgs = append(localArgs, "--peer-ip", strings.TrimSpace(*peerIP), "--peer-port", strconv.Itoa(*peerPort))
		}
		if *withInstall {
			if err := runInstall(paths, []string{"--skip-apt"}); err != nil {
				return err
			}
		}
		if err := runBuild(paths, []string{"--arch", "host"}); err != nil {
			return err
		}
		return runStart(paths, localArgs)
	}

	runNode := func(node sshv1.MeshNode) error {
		if *skipSelf && target == "all" && isSelfMeshNode(node) {
			logs.Raw("== %s ==\nSKIP self node", node.Name)
			return nil
		}
		rd := strings.TrimSpace(*repoDir)
		if rd == "" {
			rd = defaultRepoDirForNode(node)
		}
		startCmd := "./dialtone.sh mesh v1 start --bind-ip " + shellQuote(strings.TrimSpace(*bindIP)) +
			" --bind-port " + strconv.Itoa(*bindPort) +
			fmt.Sprintf(" --no-send=%t", *noSend)
		if strings.TrimSpace(*peerIP) != "" {
			startCmd += " --peer-ip " + shellQuote(strings.TrimSpace(*peerIP)) + " --peer-port " + strconv.Itoa(*peerPort)
		}
		parts := []string{"set -e", "cd " + shellQuote(rd)}
		if *withInstall {
			parts = append(parts, "./dialtone.sh mesh v1 install --skip-apt")
		}
		parts = append(parts, "./dialtone.sh mesh v1 build --arch host")
		parts = append(parts, startCmd)
		cmd := strings.Join(parts, " && ")
		logs.Raw("== %s ==", node.Name)
		if *dryRun {
			logs.Raw("[DRY-RUN] %s", cmd)
			return nil
		}
		out, err := sshv1.RunNodeCommand(node.Name, cmd, sshv1.CommandOptions{})
		if strings.TrimSpace(out) != "" {
			logs.Raw("%s", strings.TrimRight(out, "\n"))
		}
		return err
	}

	if target == "all" {
		failed := 0
		for _, n := range sshv1.ListMeshNodes() {
			if err := runNode(n); err != nil {
				failed++
				logs.Raw("ERROR: %v", err)
			}
		}
		if failed > 0 {
			return fmt.Errorf("deploy finished with %d host failures", failed)
		}
		return nil
	}
	node, err := sshv1.ResolveMeshNode(target)
	if err != nil {
		return err
	}
	return runNode(node)
}

func runStart(paths Paths, args []string) error {
	for _, a := range args {
		if isHelp(a) {
			fs := flag.NewFlagSet("mesh-start", flag.ContinueOnError)
			fs.String("bind-ip", "0.0.0.0", "Local bind IP")
			fs.Int("bind-port", 19001, "Local bind UDP port")
			fs.String("peer-ip", "", "Peer IP")
			fs.Int("peer-port", 0, "Peer port")
			fs.Bool("no-send", true, "Run in receive/listen mode")
			fs.Int("count", 1, "Send count")
			fs.Int("interval-ms", 500, "Send interval (ms)")
			fs.Int("exit-after-ms", 0, "Exit timeout (ms), 0 means run until stopped")
			fs.Bool("foreground", false, "Run in foreground")
			fs.PrintDefaults()
			return nil
		}
	}

	fs := flag.NewFlagSet("mesh-start", flag.ContinueOnError)
	bindIP := fs.String("bind-ip", "0.0.0.0", "Local bind IP")
	bindPort := fs.Int("bind-port", 19001, "Local bind UDP port")
	peerIP := fs.String("peer-ip", "", "Peer IP")
	peerPort := fs.Int("peer-port", 0, "Peer port")
	noSend := fs.Bool("no-send", true, "Run in receive/listen mode")
	count := fs.Int("count", 1, "Send count")
	intervalMS := fs.Int("interval-ms", 500, "Send interval (ms)")
	exitAfterMS := fs.Int("exit-after-ms", 0, "Exit timeout (ms), 0 means run until stopped")
	foreground := fs.Bool("foreground", false, "Run in foreground")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *bindPort <= 0 || *bindPort > 65535 {
		return fmt.Errorf("invalid --bind-port %d", *bindPort)
	}
	if *peerPort < 0 || *peerPort > 65535 {
		return fmt.Errorf("invalid --peer-port %d", *peerPort)
	}

	bin, err := ensureHostBinary(paths)
	if err != nil {
		return err
	}

	cmdArgs := []string{
		"--bind-ip", strings.TrimSpace(*bindIP),
		"--bind-port", strconv.Itoa(*bindPort),
		"--count", strconv.Itoa(*count),
		"--interval-ms", strconv.Itoa(*intervalMS),
		"--exit-after-ms", strconv.Itoa(*exitAfterMS),
	}
	if *noSend {
		cmdArgs = append(cmdArgs, "--no-send")
	}
	if strings.TrimSpace(*peerIP) != "" {
		if *peerPort <= 0 {
			return errors.New("--peer-port is required when --peer-ip is set")
		}
		cmdArgs = append(cmdArgs, "--peer-ip", strings.TrimSpace(*peerIP), "--peer-port", strconv.Itoa(*peerPort))
	}

	if *foreground {
		return runCmd("", bin, cmdArgs...)
	}
	stateDir := filepath.Join(paths.Runtime.DialtoneEnv, "mesh", "v1")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(stateDir, "mesh.log")
	pidPath := filepath.Join(stateDir, "mesh.pid")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(bin, cmdArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644)
	logs.Info("mesh started in background pid=%d log=%s", pid, logPath)
	return nil
}

func runJoin(paths Paths, args []string) error {
	for _, a := range args {
		if isHelp(a) {
			fs := flag.NewFlagSet("mesh-join", flag.ContinueOnError)
			fs.String("host", "all", "Target host: local, mesh node, or all")
			fs.String("repo-dir", "", "Remote repo dir override")
			fs.Bool("skip-self", true, "When --host all, skip current local mesh node")
			fs.Bool("with-install", false, "Run mesh install before build/start")
			fs.String("bind-ip", "0.0.0.0", "Local bind IP")
			fs.Int("bind-port", 19001, "Local bind UDP port")
			fs.String("peer-ip", "", "Peer IP")
			fs.Int("peer-port", 0, "Peer port")
			fs.Bool("no-send", true, "Run in receive/listen mode")
			fs.PrintDefaults()
			return nil
		}
	}

	fs := flag.NewFlagSet("mesh-join", flag.ContinueOnError)
	host := fs.String("host", "all", "Target host: local, mesh node, or all")
	repoDir := fs.String("repo-dir", "", "Remote repo dir override")
	skipSelf := fs.Bool("skip-self", true, "When --host all, skip current local mesh node")
	withInstall := fs.Bool("with-install", false, "Run mesh install before build/start")
	bindIP := fs.String("bind-ip", "0.0.0.0", "Local bind IP")
	bindPort := fs.Int("bind-port", 19001, "Local bind UDP port")
	peerIP := fs.String("peer-ip", "", "Peer IP")
	peerPort := fs.Int("peer-port", 0, "Peer port")
	noSend := fs.Bool("no-send", true, "Run in receive/listen mode")
	if err := fs.Parse(args); err != nil {
		return err
	}

	target := strings.ToLower(strings.TrimSpace(*host))
	if target == "" || target == "local" {
		localArgs := []string{
			"--bind-ip", strings.TrimSpace(*bindIP),
			"--bind-port", strconv.Itoa(*bindPort),
			fmt.Sprintf("--no-send=%t", *noSend),
		}
		if strings.TrimSpace(*peerIP) != "" {
			localArgs = append(localArgs, "--peer-ip", strings.TrimSpace(*peerIP), "--peer-port", strconv.Itoa(*peerPort))
		}
		if *withInstall {
			if err := runInstall(paths, []string{"--skip-apt"}); err != nil {
				return err
			}
		}
		if err := runBuild(paths, []string{"--arch", "host"}); err != nil {
			return err
		}
		return runStart(paths, localArgs)
	}

	if target == "all" {
		nodes := sshv1.ListMeshNodes()
		failed := 0
		for _, node := range nodes {
			if *skipSelf && isSelfMeshNode(node) {
				logs.Raw("== %s ==\nSKIP self node", node.Name)
				continue
			}
			if err := joinRemoteNode(node, strings.TrimSpace(*repoDir), *withInstall, strings.TrimSpace(*bindIP), *bindPort, strings.TrimSpace(*peerIP), *peerPort, *noSend); err != nil {
				failed++
				logs.Raw("== %s ==\nERROR: %v", node.Name, err)
			}
		}
		if failed > 0 {
			return fmt.Errorf("mesh join finished with %d host failures", failed)
		}
		return nil
	}

	node, err := sshv1.ResolveMeshNode(target)
	if err != nil {
		return err
	}
	return joinRemoteNode(node, strings.TrimSpace(*repoDir), *withInstall, strings.TrimSpace(*bindIP), *bindPort, strings.TrimSpace(*peerIP), *peerPort, *noSend)
}

func runShellServer(paths Paths, args []string) error {
	fs := flag.NewFlagSet("mesh-shell-server", flag.ContinueOnError)
	host := fs.String("host", "local", "Target host: local, mesh node, or all")
	repoDir := fs.String("repo-dir", "", "Remote repo dir override")
	skipSelf := fs.Bool("skip-self", true, "When --host all, skip current local mesh node")
	withBuild := fs.Bool("with-build", true, "Ensure host mesh binary is built before serving")
	bindIP := fs.String("bind-ip", "0.0.0.0", "Shell server bind IP")
	httpPort := fs.Int("http-port", 8787, "Shell server HTTP port")
	scriptPath := fs.String("script-path", "", "Path to dialtone.sh (default: <repo>/dialtone.sh)")
	foreground := fs.Bool("foreground", true, "Run in foreground (local only)")
	dryRun := fs.Bool("dry-run", false, "Print remote commands without executing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *httpPort <= 0 || *httpPort > 65535 {
		return fmt.Errorf("invalid --http-port %d", *httpPort)
	}
	script := strings.TrimSpace(*scriptPath)
	if script == "" {
		script = filepath.Join(paths.Runtime.RepoRoot, "dialtone.sh")
	}
	target := strings.ToLower(strings.TrimSpace(*host))
	if target == "" || target == "local" {
		if *withBuild {
			if err := runBuild(paths, []string{"--host", "local", "--arch", "host"}); err != nil {
				return err
			}
		}
		bin, err := ensureHostBinary(paths)
		if err != nil {
			return err
		}
		cmdArgs := []string{
			"--shell-server",
			"--bind-ip", strings.TrimSpace(*bindIP),
			"--http-port", strconv.Itoa(*httpPort),
			"--script-path", script,
		}
		if *foreground {
			return runCmd("", bin, cmdArgs...)
		}
		stateDir := filepath.Join(paths.Runtime.DialtoneEnv, "mesh", "v1")
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return err
		}
		logPath := filepath.Join(stateDir, "shell-server.log")
		pidPath := filepath.Join(stateDir, "shell-server.pid")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer logFile.Close()
		cmd := exec.Command(bin, cmdArgs...)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.Stdin = nil
		if err := cmd.Start(); err != nil {
			return err
		}
		pid := cmd.Process.Pid
		_ = cmd.Process.Release()
		_ = os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644)
		logs.Info("mesh shell-server started pid=%d log=%s", pid, logPath)
		return nil
	}

	runNode := func(node sshv1.MeshNode) error {
		if target == "all" && *skipSelf && isSelfMeshNode(node) {
			logs.Raw("== %s ==\nSKIP self node", node.Name)
			return nil
		}
		rd := strings.TrimSpace(*repoDir)
		if rd == "" {
			rd = defaultRepoDirForNode(node)
		}
		parts := []string{"set -e", "cd " + shellQuote(rd)}
		if *withBuild {
			parts = append(parts, "./dialtone.sh mesh v1 build --host local --arch host")
		}
		parts = append(parts, "./dialtone.sh mesh v1 shell-server --host local --bind-ip "+
			shellQuote(strings.TrimSpace(*bindIP))+" --http-port "+strconv.Itoa(*httpPort)+
			" --script-path "+shellQuote(filepath.ToSlash(filepath.Join(rd, "dialtone.sh")))+
			" --foreground")
		cmd := strings.Join(parts, " && ")
		logs.Raw("== %s ==", node.Name)
		if *dryRun {
			logs.Raw("[DRY-RUN] %s", cmd)
			return nil
		}
		out, err := sshv1.RunNodeCommand(node.Name, cmd, sshv1.CommandOptions{})
		if strings.TrimSpace(out) != "" {
			logs.Raw("%s", strings.TrimRight(out, "\n"))
		}
		return err
	}

	if target == "all" {
		failed := 0
		for _, n := range sshv1.ListMeshNodes() {
			if err := runNode(n); err != nil {
				failed++
				logs.Raw("ERROR: %v", err)
			}
		}
		if failed > 0 {
			return fmt.Errorf("shell-server finished with %d host failures", failed)
		}
		return nil
	}

	node, err := sshv1.ResolveMeshNode(target)
	if err != nil {
		return err
	}
	return runNode(node)
}

func joinRemoteNode(node sshv1.MeshNode, repoDirOverride string, withInstall bool, bindIP string, bindPort int, peerIP string, peerPort int, noSend bool) error {
	repoDir := strings.TrimSpace(repoDirOverride)
	if repoDir == "" {
		repoDir = defaultRepoDirForNode(node)
	}
	if repoDir == "" {
		return fmt.Errorf("cannot resolve repo dir for node %s", node.Name)
	}
	startCmd := "./dialtone.sh mesh v1 start --bind-ip " + shellQuote(bindIP) +
		" --bind-port " + strconv.Itoa(bindPort) +
		fmt.Sprintf(" --no-send=%t", noSend)
	if strings.TrimSpace(peerIP) != "" {
		startCmd += " --peer-ip " + shellQuote(peerIP) + " --peer-port " + strconv.Itoa(peerPort)
	}

	parts := []string{
		"set -e",
		"cd " + shellQuote(repoDir),
	}
	if withInstall {
		parts = append(parts, "./dialtone.sh mesh v1 install --skip-apt")
	}
	parts = append(parts, "./dialtone.sh mesh v1 build --arch host")
	parts = append(parts, startCmd)
	cmd := strings.Join(parts, " && ")

	logs.Raw("== %s ==", node.Name)
	out, err := sshv1.RunNodeCommand(node.Name, cmd, sshv1.CommandOptions{})
	if strings.TrimSpace(out) != "" {
		logs.Raw("%s", strings.TrimRight(out, "\n"))
	}
	return err
}

func ensureLibudx(paths Paths) error {
	if _, err := os.Stat(paths.LibudxDir); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(paths.LibudxDir), 0o755); err != nil {
		return err
	}
	return runCmd("", "git", "clone", "--depth", "1", "https://github.com/holepunchto/libudx.git", paths.LibudxDir)
}

func buildLibudxNative(paths Paths) error {
	if err := runCmd(paths.LibudxDir, "npm", "install"); err != nil {
		return err
	}
	if err := runCmd(paths.LibudxDir, "npx", "bare-make", "generate"); err != nil {
		return err
	}
	return runCmd(paths.LibudxDir, "npx", "bare-make", "build")
}

func buildAMD64(paths Paths) error {
	udxLib, err := findFile(paths.LibudxDir, "build", "libudx.a")
	if err != nil {
		return err
	}
	uvLib, err := findFile(paths.LibudxDir, "build", "libuv.a")
	if err != nil {
		return err
	}
	args := []string{
		paths.SourceC,
		"-Os", "-s", "-Wall", "-Wextra",
		"-I" + filepath.Join(paths.LibudxDir, "include"),
		"-I" + filepath.Join(paths.LibudxDir, "build", "_deps", "github+libuv+libuv-src", "include"),
		udxLib, uvLib,
	}
	if runtime.GOOS == "linux" {
		args = append(args, "-pthread", "-ldl", "-lrt", "-lm")
	} else {
		args = append(args, "-lpthread", "-ldl", "-lm")
	}
	args = append(args, "-o", paths.BinAMD64)
	return runCmd(paths.VersionDir, "gcc", args...)
}

func buildARM64(paths Paths) error {
	if _, err := exec.LookPath("aarch64-linux-gnu-gcc"); err != nil {
		return fmt.Errorf("missing aarch64-linux-gnu-gcc (run install first)")
	}
	buildDir := filepath.Join(paths.LibudxDir, "build-arm64-local")
	if err := runCmd(paths.VersionDir, "cmake",
		"-S", paths.LibudxDir,
		"-B", buildDir,
		"-G", "Ninja",
		"-DCMAKE_SYSTEM_NAME=Linux",
		"-DCMAKE_SYSTEM_PROCESSOR=aarch64",
		"-DCMAKE_C_COMPILER=aarch64-linux-gnu-gcc",
		"-DCMAKE_CXX_COMPILER=aarch64-linux-gnu-g++",
	); err != nil {
		return err
	}
	if err := runCmd(paths.VersionDir, "cmake", "--build", buildDir, "-j"); err != nil {
		return err
	}
	udxLib, err := findFile(paths.LibudxDir, "build-arm64-local", "libudx.a")
	if err != nil {
		return err
	}
	uvLib, err := findFile(paths.LibudxDir, "build-arm64-local", "libuv.a")
	if err != nil {
		return err
	}
	args := []string{
		paths.SourceC,
		"-Os", "-s", "-Wall", "-Wextra",
		"-I" + filepath.Join(paths.LibudxDir, "include"),
		"-I" + filepath.Join(paths.LibudxDir, "build-arm64-local", "_deps", "github+libuv+libuv-src", "include"),
		udxLib, uvLib,
		"-pthread", "-ldl", "-lrt", "-lm",
		"-o", paths.BinARM64,
	}
	return runCmd(paths.VersionDir, "aarch64-linux-gnu-gcc", args...)
}

func ensureHostBinary(paths Paths) (string, error) {
	var target string
	if runtime.GOARCH == "arm64" {
		target = paths.BinARM64
	} else {
		target = paths.BinAMD64
	}
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	if err := runBuild(paths, []string{"--arch", "host"}); err != nil {
		return "", err
	}
	return target, nil
}

func runCmd(dir, bin string, args ...string) error {
	logs.Info("run: %s %s", bin, strings.Join(args, " "))
	cmd := exec.Command(bin, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func findFile(root, containsDir, base string) (string, error) {
	targetDir := filepath.Join(root, containsDir)
	var found string
	_ = filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == base {
			found = path
			return errors.New("found")
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("file %s not found under %s", base, targetDir)
	}
	return found, nil
}

func expandArch(arch string) []string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "all":
		return []string{"x86_64", "arm64"}
	case "arm64", "aarch64":
		return []string{"arm64"}
	case "x86_64", "amd64":
		return []string{"x86_64"}
	case "host", "":
		if runtime.GOARCH == "arm64" {
			return []string{"arm64"}
		}
		return []string{"x86_64"}
	default:
		return []string{arch}
	}
}

func defaultRepoDirForNode(node sshv1.MeshNode) string {
	if len(node.RepoCandidates) > 0 {
		return node.RepoCandidates[0]
	}
	if strings.EqualFold(node.OS, "macos") || strings.EqualFold(node.OS, "darwin") {
		return filepath.ToSlash(filepath.Join("/Users", node.User, "dialtone"))
	}
	return filepath.ToSlash(filepath.Join("/home", node.User, "dialtone"))
}

func isSelfMeshNode(node sshv1.MeshNode) bool {
	if (os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "") && strings.EqualFold(node.Name, "wsl") {
		return true
	}
	hn, err := os.Hostname()
	if err != nil {
		return false
	}
	local := normalizeHost(hn)
	if local == "" {
		return false
	}
	candidates := []string{node.Name}
	candidates = append(candidates, node.Aliases...)
	sort.Strings(candidates)
	for _, c := range candidates {
		n := normalizeHost(c)
		if n == local || strings.Split(n, ".")[0] == strings.Split(local, ".")[0] {
			return true
		}
	}
	return false
}

func normalizeHost(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.TrimSuffix(v, ".")
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func goBin(rt configv1.Runtime) string {
	if strings.TrimSpace(rt.GoBin) != "" {
		return rt.GoBin
	}
	return "go"
}

func runLocalSelfTest(bin string) error {
	out, err := runCapture(bin, []string{"--help"}, 5*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "Usage:") {
		return fmt.Errorf("help output missing Usage")
	}

	tmp, err := os.MkdirTemp("", "mesh-v1-test-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	receiverLog := filepath.Join(tmp, "receiver.log")
	senderLog := filepath.Join(tmp, "sender.log")
	receiverFile, _ := os.Create(receiverLog)
	defer receiverFile.Close()

	ctxReceiver, cancelReceiver := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancelReceiver()
	receiver := exec.CommandContext(ctxReceiver, bin,
		"--bind-ip", "127.0.0.1", "--bind-port", "29002",
		"--peer-ip", "127.0.0.1", "--peer-port", "29001",
		"--local-id", "2", "--peer-id", "1",
		"--no-send", "--exit-after-ms", "2200",
	)
	receiver.Stdout = receiverFile
	receiver.Stderr = receiverFile
	if err := receiver.Start(); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := runCaptureToFile(bin, []string{
		"--bind-ip", "127.0.0.1", "--bind-port", "29001",
		"--peer-ip", "127.0.0.1", "--peer-port", "29002",
		"--local-id", "1", "--peer-id", "2",
		"--message", "mesh-test-payload", "--count", "2", "--interval-ms", "200",
		"--exit-after-ms", "1200",
	}, senderLog, 4*time.Second); err != nil {
		return err
	}
	_ = receiver.Wait()

	recvData, _ := os.ReadFile(receiverLog)
	if !strings.Contains(string(recvData), "mesh received[") || !strings.Contains(string(recvData), "mesh-test-payload") {
		return fmt.Errorf("mesh local test failed: receiver did not capture payload")
	}
	logs.Info("mesh local self-test passed")
	return nil
}

func runCapture(bin string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runCaptureToFile(bin string, args []string, file string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	_ = os.WriteFile(file, out, 0o644)
	return string(out), err
}
