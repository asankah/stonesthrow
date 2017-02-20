package stonesthrow

import (
	"bufio"
	"fmt"
	"io"
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
	config       Config
	channel      Channel
	processAdder ProcessAdder
}

func (s *Session) RunCommand(workdir string, command ...string) (string, error) {
	return RunCommandWithWorkDir(workdir, command...)
}

func (s *Session) CommandAtSourceDir(command ...string) error {
	return s.CheckCommand(s.config.GetSourcePath(), command...)
}

func (s *Session) CheckCommand(workDir string, command ...string) error {
	// Nothing to do
	if len(command) == 0 {
		return EmptyCommandError
	}

	s.channel.BeginCommand(command, false)
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = nil // inherit
	cmd.Dir = workDir
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.channel.Error(fmt.Sprintf("Can't open stdout pipe: %s", err.Error()))
		return err
	}
	var stderrPipe io.ReadCloser
	stderrPipe, err = cmd.StderrPipe()
	if err != nil {
		s.channel.Error(fmt.Sprintf("Can't open stderr pipe: %s", err.Error()))
		return err
	}
	go s.channel.Stream(stdoutPipe)
	go s.channel.Stream(stderrPipe)
	cmd.Start()
	s.processAdder.AddProcess(command, cmd.Process)
	cmd.Wait()
	s.channel.EndCommand(cmd.ProcessState)
	if cmd.ProcessState.Success() {
		return nil
	}
	return ExternalCommandFailedError
}

func (s *Session) runMB(command ...string) error {
	if len(command) == 0 {
		return InvalidArgumentError
	}

	arguments := []string{
		"python", s.config.GetSourcePath("tools", "mb", "mb.py"),
		command[0],
		"--config=" + s.config.Platform.MbConfigName}
	if s.config.Host.GomaPath != "" {
		arguments = append(arguments, "--goma-dir", s.config.Host.GomaPath)
	}
	arguments = append(arguments, command[1:]...)
	return s.CommandAtSourceDir(arguments...)
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

func (s *Session) GetAllTargets(testOnly bool) (map[string]Command, error) {
	command := []string{"gn", "ls", s.config.GetBuildPath(), "--type=executable", "--as=label"}
	if testOnly {
		command = append(command, "--testonly=true")
	}
	allLabels, err := s.config.Repository.RunHere(s, command...)
	if err != nil {
		return nil, err
	}
	targetMap := make(map[string]Command)
	scanner := bufio.NewScanner(strings.NewReader(allLabels))
	for scanner.Scan() {
		label := scanner.Text()
		targetMap[ShortTargetNameFromGNLabel(label)] = Command{
			Aliases: []string{label}}
	}
	return targetMap, nil
}

func (s *Session) SyncWorkdir(targetHash string) error {
	err := s.config.Repository.GitCheckoutRevision(s, targetHash)
	if err == DepsChangedError {
		s.channel.Info("DEPS changed. Running 'sync'")
		err = s.RunGclientSync()
	}
	if err != nil {
		return err
	}
	return s.PrepareBuild()
}

func (s *Session) RunGclientSync() error {
	if s.config.PlatformName == "mac" {
		os.Setenv("FORCE_MAC_TOOLCHAIN", "1")
	}
	return s.CommandAtSourceDir("gclient", "sync")
}

func (s *Session) PrepareBuild() error {
	if _, err := os.Stat(s.config.GetBuildPath()); os.IsNotExist(err) {
		err = os.MkdirAll(s.config.GetBuildPath(), os.ModeDir|0750)
		if err != nil {
			return err
		}
	}
	return s.runMB("gen", s.config.GetBuildPath())
}

func (s *Session) EnsureGomaIfNecessary() error {
	gomaCtlStampFile := s.config.GetBuildPath("goma_ensure_start_stamp")
	fileInfo, ok := os.Stat(gomaCtlStampFile)
	if ok == nil && fileInfo.ModTime().Add(time.Second*60*60*6).After(time.Now()) {
		return nil
	}

	if stampFile, ok := os.Create(gomaCtlStampFile); ok == nil {
		stampFile.Close()
	}

	if s.config.PlatformName == "win" {
		attemptedToStartGoma := false
		gomaCommand := []string{path.Join(s.config.Host.GomaPath, "goma_ctl.bat")}
		for i := 0; i < 5; i += 1 {
			output, err := s.config.Repository.RunHere(s, append(gomaCommand, "status")...)
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
				s.CommandAtSourceDir(append(gomaCommand, "ensure_start")...)
			}
			s.channel.Info("Waiting for compiler proxy ...")
			time.Sleep(time.Second)
		}
		s.channel.Error("Timed out.")
		return TimedOutError
	} else {
		return s.CommandAtSourceDir(
			path.Join(s.config.Host.GomaPath, "goma_ctl.py"), "ensure_start")
	}
}

func (s *Session) BuildTargets(targets ...string) error {
	if len(targets) == 0 {
		s.channel.Error("No targets. Specify 'all' to build all targets. (Not recommended)")
		return NoTargetError
	}

	s.EnsureGomaIfNecessary()

	// Isolated targets are special in that they don't describe their dependencies. Hence
	// they need to be removed so that they can be rebuilt.
	for _, target := range targets {
		if strings.HasSuffix(target, "_run") {
			isolatedFilename := s.config.GetBuildPath(target[:len(target)-4] + ".isolated")
			os.Remove(isolatedFilename)
		}
	}

	command := []string{"ninja", "-C", s.config.GetBuildPath()}
	if s.config.Host.MaxBuildJobs != 0 {
		command = append(command, "-j", fmt.Sprintf("%d", s.config.Host.MaxBuildJobs))
	}
	command = append(command, targets...)
	return s.CommandAtSourceDir(command...)
}

func (s *Session) CleanTargets(targets ...string) error {
	if len(targets) == 0 {
		s.channel.Error("No targets. Sepcify 'all' to clean all targets.")
		return NoTargetError
	}

	for _, target := range targets {
		if !targetMatcher.MatchString(target) {
			return InvalidArgumentError
		}
	}

	command := []string{"ninja", "-C", s.config.GetBuildPath(), "-t", "clean"}
	command = append(command, targets...)
	return s.CommandAtSourceDir(command...)
}

func (s *Session) Clobber(force bool) error {
	if !force {
		s.channel.Info(fmt.Sprintf("Use 'force' to remove contents of %s", s.config.GetBuildPath()))
		return nil
	}

	s.channel.Info(fmt.Sprintf("Removing contents of %s", s.config.GetBuildPath()))
	err := os.RemoveAll(s.config.GetBuildPath())
	if err != nil {
		return err
	}
	return s.PrepareBuild()
}

func (s *Session) setTestRunnerEnvironment() {
	symbolizerPath := s.config.GetSourcePath(
		"third_party", "llvm-build", "Release+Asserts", "bin",
		"llvm-symbolizer")
	if fileInfo, err := os.Stat(symbolizerPath); err == nil && !fileInfo.IsDir() {
		os.Setenv("ASAN_OPTIONS", fmt.Sprintf(
			"detect_leaks=1 symbolize=1 external_symbolizer_path=\"%s\"", symbolizerPath))
		os.Setenv("LSAN_SYMBOLIZER_PATH", symbolizerPath)
	}
}

func (s *Session) RunTestTarget(target string, args []string, revision string) error {
	if len(args) == 0 {
		s.channel.Error("Specify \"all\" to run all tests")
		return InvalidArgumentError
	}
	if len(args) == 1 && args[0] == "all" {
		args = make([]string, 0)
	}

	commandLine := make([]string, 0)
	testFilters := make([]string, 0)

	// TODO(asanka): Need to determine the correct commandline to use instead of this.
	commandLine = append(commandLine, s.config.GetBuildPath(target))

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

	err := s.SyncWorkdir(revision)
	if err != nil {
		return err
	}
	err = s.BuildTargets(target)
	if err != nil {
		return err
	}
	s.setTestRunnerEnvironment()
	return s.CommandAtSourceDir(commandLine...)
}

func (s *Session) GitStatus() error {
	return s.CommandAtSourceDir("git", "status")
}

func (s *Session) updateGitWorkDir(workDir string) error {
	err := s.CheckCommand(workDir, "git", "checkout", "origin/master")
	if err != nil {
		return err
	}

	return s.CheckCommand(workDir, "git", "pull", "origin", "master")
}

func (s *Session) GitRebaseUpdate(fetch bool) error {
	if s.config.Repository.GitRemote != "" {
		return OnlyOnMasterError
	}

	output, err := s.config.Repository.RunHere(s, "git", "status", "--porcelain",
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
	previousHead, _ := s.config.Repository.RunHere(s, "git", "symbolic-ref", "-q", "HEAD")

	if fetch {
		err = s.updateGitWorkDir(s.config.GetSourcePath("clank"))
		if err != nil {
			return err
		}

		err = s.updateGitWorkDir(s.config.GetSourcePath())
		if err != nil {
			return err
		}

		err = s.RunGclientSync()
		if err != nil {
			return err
		}
	}

	err = s.CommandAtSourceDir("git", "rebase-update",
		"--no-fetch", "--keep-going")
	if previousHead != "" {
		s.CommandAtSourceDir("git", "checkout", previousHead)
	}

	return err
}
