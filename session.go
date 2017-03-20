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

// A Session is where the work happens. Everything else is just plumbing.
//
// Any code that makes a change should be run within a session.
type Session struct {
	local  Config // Config describing the local end. This is where the work happens.
	remote Config // Config describing the peer end.

	JobEventExecutor
}

func (s *Session) Repository() RepositoryCommands {
	return RepositoryCommands{
		Repository: s.local.Repository,
		Executor:   *s}
}

type BuildFlags int

const (
	INCLUDE_BUILD_ARGS BuildFlags = iota
	SKIP_BUILD_ARGS
)

func (s *Session) InvokeMetaBuild(ctx context.Context, flags BuildFlags, command ...string) error {
	if len(command) == 0 {
		return NewEmptyCommandError("")
	}

	arguments := []string{"python", s.local.GetSourcePath("tools", "mb", "mb.py"), command[0]}

	if flags == INCLUDE_BUILD_ARGS {
		arguments = append(arguments, "--config="+s.local.Platform.MbConfigName)
		if s.local.Host.GomaPath != "" {
			arguments = append(arguments, "--goma-dir", s.local.Host.GomaPath)
		}
	} else {
		arguments = append(arguments, "--no-build")
	}
	arguments = append(arguments, s.local.GetBuildPath())
	if len(command) > 1 {
		arguments = append(arguments, command[1:]...)
	}
	return s.Repository().ExecutePassthrough(ctx, arguments...)
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
	oldDepsHash, _ := s.Repository().GitHashObject(ctx, depsFile)
	err := s.Repository().GitCheckoutRevision(ctx, targetHash)
	if err != nil {
		return err
	}
	newDepsHash, _ := s.Repository().GitHashObject(ctx, depsFile)
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
	return s.ExecuteInWorkDirPassthrough(s.local.GetSourcePath(), ctx, "gclient", "sync")
}

func (s *Session) PrepareBuild(ctx context.Context) error {
	if _, err := os.Stat(s.local.GetBuildPath()); os.IsNotExist(err) {
		err = os.MkdirAll(s.local.GetBuildPath(), os.ModeDir|0750)
		if err != nil {
			return err
		}
	}
	return s.InvokeMetaBuild(ctx, INCLUDE_BUILD_ARGS, "gen")
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
		for i := 0; i < 5; i += 1 {
			output, err := s.ExecuteInWorkDirNoStream(s.local.Host.GomaPath, ctx, "goma_ctl.bat", "status")
			if err != nil {
				return err
			}
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "compiler proxy ") &&
					strings.Contains(line, " status: ") &&
					strings.HasSuffix(line, "ok") {
					s.channel.Info(fmt.Sprintf("Compiler proxy running. %s", line))
					return nil
				}
			}

			if !attemptedToStartGoma {
				attemptedToStartGoma = true
				gomaCommand := []string{path.Join(s.local.Host.GomaPath, "goma_ctl.bat")}
				cmd := exec.CommandContext(ctx, "cmd.exe", "/c", gomaCommand[0], "ensure_start")
				err = cmd.Start()
				// Don't wait for 'goma_ctl.bat ensure_start' to terminate. It won't.
				if err != nil {
					return err
				}
			}
			s.channel.Info("Waiting for compiler proxy ...")
			time.Sleep(time.Second)
		}
		s.channel.Error("Timed out.")
		return NewTimedOutError("Couldn't start compiler proxy.")
	} else {
		return s.ExecuteInWorkDirPassthrough(s.local.Host.GomaPath, ctx, "goma_ctl.py", "ensure_start")
	}
}

func (s *Session) BuildTargets(ctx context.Context, targets ...string) error {
	if len(targets) == 0 {
		s.channel.Error("No targets. Specify 'all' to build all targets. (Not recommended)")
		return NewNoTargetError("No target specified for build command")
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

	command := []string{"ninja"}
	if s.local.Host.MaxBuildJobs != 0 {
		command = append(command, "-j", fmt.Sprintf("%d", s.local.Host.MaxBuildJobs))
	}
	command = append(command, targets...)
	return s.ExecuteInWorkDirPassthrough(s.local.GetBuildPath(), ctx, command...)
}

func (s *Session) CleanTargets(ctx context.Context, targets ...string) error {
	if len(targets) == 0 {
		s.channel.Error("No targets. Sepcify 'all' to clean all targets.")
		return NewNoTargetError("No target specified for clean")
	}

	for _, target := range targets {
		if !targetMatcher.MatchString(target) {
			return NewInvalidArgumentError("%s is not a valid target", target)
		}
	}

	command := []string{"ninja", "-t", "clean"}
	command = append(command, targets...)
	return s.ExecuteInWorkDirPassthrough(s.local.GetBuildPath(), ctx, command...)
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
		return NewNoTargetError("")
	}
	if len(args) == 1 && args[0] == "all" {
		args = make([]string, 0)
	}

	commandLine := []string{"run", target, "--"}
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
			return NewInvalidArgumentError("%s is not a valid argument", arg)
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
	return s.InvokeMetaBuild(ctx, SKIP_BUILD_ARGS, commandLine...)
}

func (s *Session) GitStatus(ctx context.Context) error {
	return s.Repository().ExecutePassthrough(ctx, "git", "status")
}

func (s *Session) updateGitWorkDir(ctx context.Context, workDir string) error {
	err := s.ExecuteInWorkDirPassthrough(workDir, ctx, "git", "checkout", "origin/master")
	if err != nil {
		return err
	}

	return s.ExecuteInWorkDirPassthrough(workDir, ctx, "git", "pull", "origin", "master")
}

func (s *Session) GitRebaseUpdate(ctx context.Context, fetch bool) error {
	if s.local.Repository.GitConfig.Remote != "" {
		return NewNoUpstreamError("No upstream configured for repository at %s", s.local.Repository.SourcePath)
	}

	status, err := s.Repository().GitStatus(ctx)
	if err != nil {
		return err
	}

	if status.HasModified {
		s.channel.Error("Local modifications exist.")
		return NewWorkTreeDirtyError("")
	}

	// Ignoring error here since we should be able to run rebase-update with a
	// detached head. If there's no symolic ref, then we'll skip the final
	// checkout step.
	previousHead, _ := s.Repository().GitCurrentBranch(ctx)

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

	err = s.Repository().ExecutePassthrough(ctx, "git", "clean", "-f")
	err = s.Repository().ExecutePassthrough(ctx, "git", "rebase-update",
		"--no-fetch", "--keep-going")
	if previousHead != "" {
		s.Repository().ExecutePassthrough(ctx, "git", "checkout", previousHead)
	}

	return err
}

func (s *Session) resolveLocalBranches(ctx context.Context, branches []string) ([]string, error) {
	resolvedBranches := []string{}
	for _, branch := range branches {
		if branch == "HEAD" {
			head_branch, err := s.Repository().GitCurrentBranch(ctx)
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
		return NewInvalidArgumentError("No branches specified for 'git push'")
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
		return NewConfigIncompleteError("Can't route RPC to upstream")
	}

	if localRepository == remoteRepository {
		s.channel.Info("The local and remote repositories are the same.")
		return NewNothingToDoError("local == remote")
	}

	branchConfigs, err := s.Repository().GitGetBranchConfig(ctx, branches,
		append(localRepository.GitConfig.SyncableProperties,
			remoteRepository.GitConfig.SyncableProperties...))
	if err != nil {
		return err
	}

	peerSession := Session{
		s.local,
		*remoteConfig,
		ChannelExecutor{
			channel:      s.channel,
			processAdder: s.processAdder,
			label:        s.local.Host.Name}}
	err = peerSession.SendRequestToRemoteServer(
		RequestMessage{
			Command:        "__prepare_for_git_push__",
			Repository:     remoteConfig.Repository.Name,
			SourceHostname: s.local.Host.Name})
	if err != nil {
		return err
	}

	err = s.Repository().GitPush(ctx, branches)
	if err != nil {
		return err
	}

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
		return NewInvalidArgumentError("No branches specified for 'git fetch'")
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
		return NewConfigIncompleteError("Can't route RPC to remote")
	}

	if localRepository == remoteRepository {
		s.channel.Info("The local and remote repositories are the same.")
		return NewNothingToDoError("local == remote")
	}

	err = s.Repository().ExecutePassthrough(ctx, "git", "checkout", "--detach", "origin/master")
	if err != nil {
		return err
	}
	err = s.Repository().GitFetch(ctx, branches)
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

	peerSession := Session{
		s.local,
		*remoteConfig,
		ChannelExecutor{
			channel:      s.channel,
			processAdder: s.processAdder,
			label:        s.local.Host.Name}}
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
		return s.Repository().GitCheckoutRevision(ctx, branches[0])
	} else {
		return s.Repository().GitCheckoutRevision(ctx, "origin/master")
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
		t, err := s.Repository().GitGetBranchConfig(ctx, []string{config.Name}, properties)
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
		return NewNothingToDoError("local == remote")
	}
	s.channel.Info(fmt.Sprintf("Sending %s request to remote server %s", request.Command, s.remote.Host.Name))
	defer s.channel.Info("Done")
	return SendRequestToRemoteServer(s, s.local, s.remote, request,
		s.channel.NewSendChannel())
}
