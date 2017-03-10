package stonesthrow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	testFilterMatcher = regexp.MustCompile(`^[\w*][\w*._\-/:]*$`)
	optionMatcher     = regexp.MustCompile(`^--?\w[\w*,._\-/=]*$`)
	targetMatcher     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z_]*$`)
)

type ProcessRecord struct {
	Process    *os.Process
	Command    []string
	StartTime  time.Time
	Running    bool
	EndTime    time.Time
	SystemTime time.Duration
	UserTime   time.Duration
}

type Session struct {
	local        Config
	remote       Config
	channel      Channel
	processAdder ProcessAdder
}

func (s *Session) RunCommand(ctx context.Context, workdir string, command ...string) (string, error) {
	return RunCommandWithWorkDir(ctx, workdir, command...)
}

func (s *Session) CommandAtSourceDir(ctx context.Context, command ...string) error {
	return s.local.Repository.CheckHere(ctx, s, command...)
}

func (s *Session) CheckCommand(ctx context.Context, workDir string, command ...string) error {
	// Nothing to do?
	if len(command) == 0 {
		return EmptyCommandError
	}

	s.channel.BeginCommand(s.local.Host.Name, workDir, command, false)
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Env = nil // inherit
	cmd.Dir = workDir
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.channel.Error(fmt.Sprintf("Can't open stdout pipe: %s", err.Error()))
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		s.channel.Error(fmt.Sprintf("Can't open stderr pipe: %s", err.Error()))
		return err
	}

	quitter := make(chan int)
	go func() {
		s.channel.Stream(stdoutPipe)
		quitter <- 1
	}()
	go func() {
		s.channel.Stream(stderrPipe)
		quitter <- 2
	}()

	cmd.Start()
	if s.processAdder != nil {
		s.processAdder.AddProcess(command, cmd.Process)
	}
	err = cmd.Wait()
	stdoutPipe.Close()
	stderrPipe.Close()
	<-quitter
	<-quitter
	if err != nil {
		return err
	}
	if s.processAdder != nil {
		s.processAdder.RemoveProcess(cmd.Process, cmd.ProcessState)
	}
	s.channel.EndCommand(cmd.ProcessState)
	if cmd.ProcessState.Success() {
		return nil
	}

	return ExternalCommandFailedError
}

func (s *Session) runMB(ctx context.Context, command ...string) error {
	if len(command) == 0 {
		return InvalidArgumentError
	}

	arguments := []string{
		"python", s.local.GetSourcePath("tools", "mb", "mb.py"),
		command[0], s.local.GetBuildPath(),
		"--config=" + s.local.Platform.MbConfigName}
	if s.local.Host.GomaPath != "" {
		arguments = append(arguments, "--goma-dir", s.local.Host.GomaPath)
	}
	arguments = append(arguments, command[1:]...)
	return s.CommandAtSourceDir(ctx, arguments...)
}

func ShortTargetNameFromGNLabel(label string) string {
	components := strings.Split(label, ":")
	if len(components) > 0 {
		return components[len(components)-1]
	} else if strings.HasPrefix(label, "//") {
		return label[2:]
	} else {
		return label
	}
}

func (s *Session) SyncWorkdir(ctx context.Context, targetHash string) error {
	depsFile := s.local.GetSourcePath("DEPS")
	oldDepsHash, _ := s.local.Repository.GitHashObject(ctx, s, depsFile)
	err := s.local.Repository.GitCheckoutRevision(ctx, s, targetHash)
	if err != nil {
		return err
	}
	newDepsHash, _ := s.local.Repository.GitHashObject(ctx, s, depsFile)
	if oldDepsHash != newDepsHash {
		s.channel.Info("DEPS changed. Running 'sync'")
		return s.RunGclientSync(ctx)
	}
	return nil
}

func (s *Session) RunGclientSync(ctx context.Context) error {
	if s.local.PlatformName == "mac" {
		os.Setenv("FORCE_MAC_TOOLCHAIN", "1")
	}
	return s.local.Repository.CheckHere(ctx, s, "gclient", "sync")
}

func (s *Session) PrepareBuild(ctx context.Context) error {
	if _, err := os.Stat(s.local.GetBuildPath()); os.IsNotExist(err) {
		err = os.MkdirAll(s.local.GetBuildPath(), os.ModeDir|0750)
		if err != nil {
			return err
		}
	}
	return s.runMB(ctx, "gen")
}

func (s *Session) EnsureGomaIfNecessary(ctx context.Context) error {
	gomaCtlStampFile := s.local.GetBuildPath("goma_ensure_start_stamp")
	fileInfo, ok := os.Stat(gomaCtlStampFile)
	if ok == nil && fileInfo.ModTime().Add(time.Second*60*60*6).After(time.Now()) {
		return nil
	}

	if stampFile, ok := os.Create(gomaCtlStampFile); ok == nil {
		stampFile.Close()
	}

	if s.local.PlatformName == "win" {
		attemptedToStartGoma := false
		gomaCommand := []string{path.Join(s.local.Host.GomaPath, "goma_ctl.bat")}
		for i := 0; i < 5; i += 1 {
			output, err := s.local.Repository.RunHere(ctx, s, append(gomaCommand, "status")...)
			if err != nil {
				return err
			}
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Compiler proxy status:") &&
					strings.HasSuffix(line, "ok") {
					s.channel.Info(fmt.Sprintf("Compiler proxy running. %s", line))
					return nil
				}
			}

			if !attemptedToStartGoma {
				attemptedToStartGoma = true
				s.CommandAtSourceDir(ctx, append(gomaCommand, "ensure_start")...)
			}
			s.channel.Info("Waiting for compiler proxy ...")
			time.Sleep(time.Second)
		}
		s.channel.Error("Timed out.")
		return TimedOutError
	} else {
		return s.CommandAtSourceDir(
			ctx, path.Join(s.local.Host.GomaPath, "goma_ctl.py"), "ensure_start")
	}
}

func (s *Session) BuildTargets(ctx context.Context, targets ...string) error {
	if len(targets) == 0 {
		s.channel.Error("No targets. Specify 'all' to build all targets. (Not recommended)")
		return NoTargetError
	}

	s.EnsureGomaIfNecessary(ctx)

	// Isolated targets are special in that they don't describe their dependencies. Hence
	// they need to be removed so that they can be rebuilt.
	for _, target := range targets {
		if strings.HasSuffix(target, "_run") {
			isolatedFilename := s.local.GetBuildPath(target[:len(target)-4] + ".isolated")
			os.Remove(isolatedFilename)
		}
	}

	command := []string{"ninja", "-C", s.local.GetBuildPath()}
	if s.local.Host.MaxBuildJobs != 0 {
		command = append(command, "-j", fmt.Sprintf("%d", s.local.Host.MaxBuildJobs))
	}
	command = append(command, targets...)
	return s.CommandAtSourceDir(ctx, command...)
}

func (s *Session) CleanTargets(ctx context.Context, targets ...string) error {
	if len(targets) == 0 {
		s.channel.Error("No targets. Sepcify 'all' to clean all targets.")
		return NoTargetError
	}

	for _, target := range targets {
		if !targetMatcher.MatchString(target) {
			return InvalidArgumentError
		}
	}

	command := []string{"ninja", "-C", s.local.GetBuildPath(), "-t", "clean"}
	command = append(command, targets...)
	return s.CommandAtSourceDir(ctx, command...)
}

func (s *Session) Clobber(ctx context.Context, force bool) error {
	if !force {
		s.channel.Info(fmt.Sprintf("Use 'force' to remove contents of %s", s.local.GetBuildPath()))
		return nil
	}

	s.channel.Info(fmt.Sprintf("Removing contents of %s", s.local.GetBuildPath()))
	err := os.RemoveAll(s.local.GetBuildPath())
	if err != nil {
		return err
	}
	return s.PrepareBuild(ctx)
}

func (s *Session) setTestRunnerEnvironment() {
	symbolizerPath := s.local.GetSourcePath(
		"third_party", "llvm-build", "Release+Asserts", "bin",
		"llvm-symbolizer")
	if fileInfo, err := os.Stat(symbolizerPath); err == nil && !fileInfo.IsDir() {
		os.Setenv("ASAN_OPTIONS", fmt.Sprintf(
			"detect_leaks=1 symbolize=1 external_symbolizer_path=\"%s\"", symbolizerPath))
		os.Setenv("LSAN_SYMBOLIZER_PATH", symbolizerPath)
	}
}

func (s *Session) RunTestTarget(ctx context.Context, target string, args []string, revision string) error {
	if len(args) == 0 {
		s.channel.Error("Specify \"all\" to run all tests")
		return InvalidArgumentError
	}
	if len(args) == 1 && args[0] == "all" {
		args = make([]string, 0)
	}

	commandLine := []string{"run", target, "--no-build", "--"}
	testFilters := make([]string, 0)

	for _, arg := range args {
		switch {
		case arg == "with-output":
			commandLine = append(commandLine,
				"--test-launcher-print-test-stdio=always")
		case testFilterMatcher.MatchString(arg):
			testFilters = append(testFilters, arg)
		case optionMatcher.MatchString(arg):
			commandLine = append(commandLine, arg)
		default:
			return InvalidArgumentError
		}
	}

	if len(testFilters) > 0 {
		commandLine = append(commandLine, "--gtest_filter="+strings.Join(testFilters, ":"))
	}

	err := s.SyncWorkdir(ctx, revision)
	if err != nil {
		return err
	}
	err = s.BuildTargets(ctx, target)
	if err != nil {
		return err
	}
	s.setTestRunnerEnvironment()
	return s.runMB(ctx, commandLine...)
}

func (s *Session) GitStatus(ctx context.Context) error {
	return s.CommandAtSourceDir(ctx, "git", "status")
}

func (s *Session) updateGitWorkDir(ctx context.Context, workDir string) error {
	err := s.CheckCommand(ctx, workDir, "git", "checkout", "origin/master")
	if err != nil {
		return err
	}

	return s.CheckCommand(ctx, workDir, "git", "pull", "origin", "master")
}

func (s *Session) GitRebaseUpdate(ctx context.Context, fetch bool) error {
	if s.local.Repository.GitConfig.Remote != "" {
		return NoUpstreamError
	}

	output, err := s.local.Repository.RunHere(ctx, s, "git", "status", "--porcelain",
		"--untracked-files=normal")
	if err != nil {
		return err
	}

	if output != "" {
		s.channel.Error("Local modifications exist.")
		return WorkTreeDirtyError
	}

	// Ignoring error here since we should be able to run rebase-update with a
	// detached head. If there's no symolic ref, then we'll skip the final
	// checkout step.
	previousHead, _ := s.local.Repository.RunHere(ctx, s, "git", "symbolic-ref", "-q", "HEAD")

	if fetch {
		s.channel.Info("Updating clank")
		err = s.updateGitWorkDir(ctx, s.local.GetSourcePath("clank"))
		if err != nil {
			return err
		}

		s.channel.Info("Updating chromium")
		err = s.updateGitWorkDir(ctx, s.local.GetSourcePath())
		if err != nil {
			return err
		}

		err = s.RunGclientSync(ctx)
		if err != nil {
			return err
		}
	}

	err = s.local.Repository.CheckHere(ctx, s, "git", "clean", "-f")
	err = s.local.Repository.CheckHere(ctx, s, "git", "rebase-update",
		"--no-fetch", "--keep-going")
	if previousHead != "" {
		s.CommandAtSourceDir(ctx, "git", "checkout", previousHead)
	}

	return err
}

func (s *Session) resolveLocalBranches(ctx context.Context, branches []string) ([]string, error) {
	resolvedBranches := []string{}
	for _, branch := range branches {
		if branch == "HEAD" {
			head_branch, err := s.local.Repository.GitCurrentBranch(ctx, s)
			if err != nil {
				return nil, err
			}

			resolvedBranches = append(resolvedBranches, head_branch)
		} else {
			resolvedBranches = append(resolvedBranches, branch)
		}
	}

	return resolvedBranches, nil
}

func (s *Session) GitPushToUpstream(ctx context.Context, branches []string) error {
	branches, err := s.resolveLocalBranches(ctx, branches)
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		return InvalidArgumentError
	}

	localRepository := s.local.Repository

	var remoteRepository *RepositoryConfig
	var remoteConfig *Config

	if s.remote.IsValid() && s.local.Repository.GitConfig.RemoteHost == s.remote.Host {
		// The request was sent by the upstream. How convenient.
		remoteConfig = &s.remote
		remoteRepository = remoteConfig.Repository
	} else if s.local.Repository.GitConfig.RemoteHost != nil {
		// We know our remote host. But we'd need to establish a new connection to it.
		remoteConfig = &Config{}
		err = remoteConfig.SelectPeerConfig(s.local.ConfigurationFile,
			s.local.Repository.GitConfig.RemoteHostname,
			s.local.Repository.Name)
		if err != nil {
			return err
		}

		remoteRepository = remoteConfig.Repository
	}

	if remoteConfig == nil || remoteRepository == nil {
		s.channel.Info("Can't determine how to contact repository remote.")
		return ConfigIncompleteError
	}

	if localRepository == remoteRepository {
		s.channel.Info("The local and remote repositories are the same.")
		return NothingToDoError
	}

	branchConfigs, err := s.local.Repository.GitGetBranchConfig(ctx, s, branches,
		append(localRepository.GitConfig.SyncableProperties,
			remoteRepository.GitConfig.SyncableProperties...))
	if err != nil {
		return err
	}

	peerSession := Session{local: s.local, remote: *remoteConfig, channel: s.channel, processAdder: s.processAdder}
	err = peerSession.SendRequestToRemoteServer(
		RequestMessage{
			Command:        "__prepare_for_git_push__",
			Repository:     remoteConfig.Repository.Name,
			SourceHostname: s.local.Host.Name})
	if err != nil {
		return err
	}

	output, err := s.local.Repository.GitPush(ctx, s, branches, true)
	if err != nil {
		return err
	}
	s.channel.BeginCommand(s.local.Host.Name, s.local.Repository.SourcePath, []string{"git", "push"}, false)
	for _, line := range output {
		s.channel.Send(TerminalOutputMessage{Output: line})
	}
	s.channel.Send(EndCommandMessage{})

	return peerSession.SendRequestToRemoteServer(
		RequestMessage{
			Command:        "__apply_branch_config__",
			Repository:     remoteConfig.Repository.Name,
			SourceHostname: s.local.Host.Name,
			BranchConfigs:  branchConfigs})
}

func (s *Session) GitFetchFromUpstream(ctx context.Context, branches []string) error {
	if len(branches) == 1 && branches[0] == "all" {
		branches[0] = "refs/heads/*"
	}
	if len(branches) == 0 {
		return InvalidArgumentError
	}

	localRepository := s.local.Repository

	var remoteRepository *RepositoryConfig
	var remoteConfig *Config
	var err error

	if s.remote.IsValid() && s.local.Repository.GitConfig.RemoteHost == s.remote.Host {
		// The request was sent by the upstream. How convenient.
		remoteConfig = &s.remote
		remoteRepository = remoteConfig.Repository
	} else if s.local.Repository.GitConfig.RemoteHost != nil {
		// We know our remote host. But we'd need to establish a new connection to it.
		remoteConfig = &Config{}
		err = remoteConfig.SelectPeerConfig(s.local.ConfigurationFile,
			s.local.Repository.GitConfig.RemoteHostname,
			s.local.Repository.Name)
		if err != nil {
			return err
		}

		remoteRepository = remoteConfig.Repository
	}

	if remoteConfig == nil || remoteRepository == nil {
		s.channel.Info("Can't determine how to contact repository remote.")
		return ConfigIncompleteError
	}

	if localRepository == remoteRepository {
		s.channel.Info("The local and remote repositories are the same.")
		return NothingToDoError
	}

	err = s.local.Repository.CheckHere(ctx, s, "git", "checkout", "--detach", "origin/master")
	if err != nil {
		return err
	}
	err = s.local.Repository.GitFetch(ctx, s, branches)
	if err != nil {
		return err
	}

	branchConfigs := []BranchConfig{}
	allProperties := append(localRepository.GitConfig.SyncableProperties, remoteRepository.GitConfig.SyncableProperties...)
	gitConfig := make(map[string]string)
	for _, property := range allProperties {
		gitConfig[property] = "?"
	}
	for _, branch := range branches {
		branchConfigs = append(branchConfigs, BranchConfig{Name: branch, GitConfig: gitConfig})
	}

	peerSession := Session{local: s.local, remote: *remoteConfig, channel: s.channel, processAdder: s.processAdder}
	err = peerSession.SendRequestToRemoteServer(
		RequestMessage{
			Command:        "__get_branch_config__",
			Repository:     remoteConfig.Repository.Name,
			SourceHostname: s.local.Host.Name,
			BranchConfigs:  branchConfigs})
	if err != nil {
		return err
	}

	if len(branches) == 1 && branches[0] != "refs/heads/*" {
		return s.local.Repository.GitCheckoutRevision(ctx, s, branches[0])
	} else {
		return s.local.Repository.GitCheckoutRevision(ctx, s, "origin/master")
	}
	return nil
}

func (s *Session) SendBranchConfigToCaller(ctx context.Context, configs []BranchConfig) error {
	configsToReturn := []BranchConfig{}
	for _, config := range configs {
		properties := []string{}
		for property, _ := range config.GitConfig {
			properties = append(properties, property)
		}
		t, err := s.local.Repository.GitGetBranchConfig(ctx, s, []string{config.Name}, properties)
		if err != nil {
			return err
		}
		configsToReturn = append(configsToReturn, t...)
	}
	return s.channel.Send(RequestMessage{
		Command:        "__apply_branch_config__",
		Repository:     s.local.Repository.Name,
		SourceHostname: s.local.Host.Name,
		BranchConfigs:  configsToReturn})
}

func (s *Session) SendRequestToRemoteServer(request RequestMessage) error {
	if s.local.Host == s.remote.Host {
		s.channel.Error("Local and remote hosts are the same. Skipping remote request.")
		return NothingToDoError
	}
	s.channel.Info(fmt.Sprintf("Sending %s request to remote server %s", request.Command, s.remote.Host.Name))
	defer s.channel.Info("Done")
	return SendRequestToRemoteServer(s, s.local, s.remote, request,
		s.channel.NewSendChannel())
}
