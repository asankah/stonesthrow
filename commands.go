package stonesthrow

import (
	"context"
	"flag"
	"github.com/google/subcommands"
	"io"
	"sync"
)

type FlagSetter func(*flag.FlagSet)
type RequestHandler func(context.Context, *Session, RequestMessage, *flag.FlagSet) error

type NeedsRevision bool
type ShowInHelp bool

var (
	NO_REVISION    = NeedsRevision(true)
	NEEDS_REVISION = NeedsRevision(false)
)

var (
	SHOW_IN_HELP   = ShowInHelp(true)
	HIDE_FROM_HELP = ShowInHelp(false)
)

type CommandHandler struct {
	name       string
	synopsis   string
	usage      string
	flagSetter FlagSetter
	handler    RequestHandler

	needsRevision NeedsRevision
	showInHelp    ShowInHelp
}

func (h CommandHandler) Name() string {
	return h.name
}

func (h CommandHandler) Synopsis() string {
	return h.synopsis
}

func (h CommandHandler) Usage() string {
	return h.usage
}

func (h CommandHandler) SetFlags(f *flag.FlagSet) {
	if h.flagSetter != nil {
		h.flagSetter(f)
	}
}

func (h CommandHandler) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if h.handler == nil {
		return subcommands.ExitFailure
	}

	session := args[0].(*Session)
	request := args[1].(RequestMessage)

	err := h.handler(ctx, session, request, f)
	if IsInvalidArgumentError(err) {
		return subcommands.ExitUsageError
	}
	if err != nil {
		session.channel.Error(err.Error())
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

func (h CommandHandler) NeedsRevision() bool {
	return h.needsRevision == NEEDS_REVISION
}

var (
	handlerMap         map[string]*CommandHandler
	initOnce           sync.Once
	initConfigHandlers sync.Once
)

var DefaultHandlers = []CommandHandler{
	CommandHandler{
		"branch",
		`List local branches.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.Repository().ExecutePassthrough(ctx, "git", "branch", "--list", "-vvv")
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{
		"build",
		`Build specified targets.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			if !s.local.Host.IsSameHost(req.SourceHostname) {
				err := s.SyncWorkdir(ctx, req.Revision)
				if err != nil {
					return err
				}
			}
			return s.BuildTargets(ctx, req.Arguments...)
		},
		NEEDS_REVISION, SHOW_IN_HELP},

	CommandHandler{
		"clean",
		`'clean build' cleans the build directory, while 'clean source' cleans the source directory.`, "",
		func(f *flag.FlagSet) {
			f.Bool("out", false, "Clean the output directory.")
			f.Bool("src", false, "Clean the source directory.")
			f.Bool("force", false,
				"Actually do the cleaning. Without this flag, the command merely lists which files would be affected.")
		},
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			outValue := f.Lookup("out")
			srcValue := f.Lookup("src")
			forceValue := f.Lookup("force")

			if outValue.Value.String() == "true" {
				return s.CleanTargets(ctx, req.Arguments...)
			}

			if srcValue.Value.String() == "true" {
				if forceValue.Value.String() == "true" {
					return s.Repository().ExecutePassthrough(ctx, "git", "clean", "-f")
				} else {
					s.channel.Info("Specify 'clean source force' to remove files not recognized by git.")
					return s.Repository().ExecutePassthrough(ctx, "git", "clean", "-n")
				}
			}
			return NewInvalidArgumentError("Either 'src' or 'out' should be specified")
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{
		"clobber",
		`Clobber the build directory.`, "",
		func(f *flag.FlagSet) {
			f.Bool("force", false, "Actually clobber. The command doesn't do anything if this flag is not specified.")
		},
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.Clobber(ctx, f.Lookup("force").Value.String() == "true")
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"ping",
		`Diagnostic. Responds with a pong.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			s.channel.Info("Pong")
			return nil
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"prepare",
		`Prepare build directory. Runs 'mb gen'.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.PrepareBuild(ctx)
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"pull",
		`Pull a specific branch from upstream.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			if len(req.Arguments) == 0 {
				s.channel.Error("Need to specify branch")
				return NewInvalidArgumentError("No branch specified")
			}
			return s.GitFetchFromUpstream(ctx, req.Arguments)
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"push",
		`Push local branches upstream.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			branches := req.Arguments
			if len(branches) == 0 {
				branches = []string{"HEAD"}
			}
			return s.GitPushToUpstream(ctx, branches)
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"status",
		`Run 'git status'.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.GitStatus(ctx)
		}, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"sync",
		`Run 'gclient sync'.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.RunGclientSync(ctx)
		},
		NEEDS_REVISION, SHOW_IN_HELP},

	CommandHandler{"list",
		"List available targets", "",
		func(f *flag.FlagSet) {
			f.Bool("tests", false, "Lists test targets only")
		},
		handleList, NO_REVISION, SHOW_IN_HELP},

	CommandHandler{"__prepare_for_git_push__", "", "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.Repository().ExecutePassthrough(ctx, "git", "checkout", "--detach", "origin/master")

		}, NO_REVISION, HIDE_FROM_HELP},

	CommandHandler{"__get_branch_config__", "", "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.SendBranchConfigToCaller(ctx, req.BranchConfigs)
		}, NO_REVISION, HIDE_FROM_HELP},

	CommandHandler{"__apply_branch_config__", "", "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.Repository().GitSetBranchConfig(ctx, req.BranchConfigs)
		}, NO_REVISION, HIDE_FROM_HELP}}

func AddHandler(command string, doc string, handler RequestHandler, needsRevision NeedsRevision) {
	newHandler := CommandHandler{command, doc, "", nil, handler, needsRevision, SHOW_IN_HELP}
	handlerMap[newHandler.name] = &newHandler
}

func AddTestHandler(command string, handler RequestHandler) {
	newHandler := CommandHandler{command, "Runs the specific test target.", "",
		nil, handler, NEEDS_REVISION, HIDE_FROM_HELP}
	handlerMap[newHandler.name] = &newHandler
}

func handleHelp(ctx context.Context, s *Session, _ RequestMessage, _ *flag.FlagSet) error {
	var commandList CommandListMessage
	commandList.Repositories = make(map[string]Repository)

	var chromeRepo Repository
	chromeRepo.BuildPath = s.local.GetBuildPath()
	chromeRepo.SourcePath = s.local.GetSourcePath()
	chromeRepo.Revistion, _ = s.Repository().GitRevision(ctx, "HEAD")
	commandList.Synposis = "Remote build runner"
	commandList.ConfigFile = s.local.ConfigurationFile.FileName
	commandList.Repositories["chrome"] = chromeRepo

	commandList.Commands = make(map[string]Command)
	for _, handler := range DefaultHandlers {
		if handler.showInHelp == HIDE_FROM_HELP {
			continue
		}
		var c Command
		c.Synopsis = handler.Synopsis()
		c.Usage = handler.Usage()
		commandList.Commands[handler.name] = c
	}

	commandList.Commands["<any test target>"] = Command{
		Synopsis: `Runs the specific test target.`,
		Usage: `Use 'list tests' to retrieve a list of known test targets.

Arguments are as follows:
'all'           : Run all tests in suite.
'withoutput'    : Adds --test-launcher-print-test-stdio=always.
any option      : Passed along to test runner.
any other token : Treated as a test filter and passed along to the test runner using --gtest_filter.

E.g.: 'net_unittests foo*' is expanded into 'net_unttests --gtest_filter=foo*'
`}

	s.channel.ListCommands(commandList)
	return nil
}

func handleList(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
	var (
		testsOnly = false
	)
	if f.Lookup("tests").Value.String() == "true" {
		testsOnly = true
	}
	allTargets, _ := s.local.Platform.GetAllTargets(testsOnly)
	var commandList CommandListMessage
	commandList.Synposis = "Known build targets."
	commandList.Commands = allTargets
	s.channel.ListCommands(commandList)
	return nil
}

func InitializeCommands() {
	handlerMap = make(map[string]*CommandHandler)

	for idx, handler := range DefaultHandlers {
		handlerMap[handler.name] = &DefaultHandlers[idx]
	}

	AddHandler("help", `Does what you think it does`, handleHelp, NO_REVISION)
}

func InitializeHostCommands(localConfig Config) {
	if localConfig.Repository.GitConfig.Remote == "" {
		AddHandler("ru", `Run 'git rebase-update'.`,
			func(ctx context.Context, s *Session, req RequestMessage, _ *flag.FlagSet) error {
				fetch := true
				if len(req.Arguments) == 1 && req.Arguments[0] == "nofetch" {
					fetch = false
				}
				return s.GitRebaseUpdate(ctx, fetch)
			}, NO_REVISION)
	} else {
		AddHandler("sync_workdir", `Synchronize remote work directory with local.`,
			func(ctx context.Context, s *Session, req RequestMessage, _ *flag.FlagSet) error {
				return s.SyncWorkdir(ctx, req.Revision)
			}, NEEDS_REVISION)
	}

	allTestTargets, err := localConfig.Platform.GetAllTargets(true)
	if err != nil {
		return
	}

	for target := range allTestTargets {
		targetCopy := target
		AddTestHandler(target,
			func(ctx context.Context, s *Session, req RequestMessage, _ *flag.FlagSet) error {
				return s.RunTestTarget(ctx, targetCopy, req.Arguments, req.Revision)
			})
	}
}

var (
	CommandsThatDontNeedARevision = map[string]bool{
		"ru":     true,
		"quit":   true,
		"reload": true,
		"jobs":   true,
		"kill":   true,
		"join":   true,
		"help":   true}
)

func CommandNeedsRevision(command string) bool {
	commandHandler, ok := handlerMap[command]
	if !ok {
		_, ok := CommandsThatDontNeedARevision[command]
		return !ok
	}
	return commandHandler.NeedsRevision()
}

func HandleRequestOnLocalHost(c context.Context, s *Session, req RequestMessage) {
	arguments := []string{req.Command}
	arguments = append(arguments, req.Arguments...)
	flagset := flag.NewFlagSet("", flag.ContinueOnError)

	stdoutPipeReader, stdoutPipeWriter := io.Pipe()
	stderrPipeReader, stderrPipeWriter := io.Pipe()
	quitter := make(chan int)

	go func() {
		s.channel.Stream(stdoutPipeReader)
		quitter <- 1
	}()
	go func() {
		s.channel.Stream(stderrPipeReader)
		quitter <- 1
	}()

	commander := subcommands.NewCommander(flagset, "st_client")
	for _, handler := range handlerMap {
		commander.Register(*handler, "")
	}

	err := flagset.Parse(arguments)
	if err != nil {
		s.channel.Error("Invalid method. Use 'help' for a list of commands")
		return
	}

	commander.Error = stderrPipeWriter
	commander.Output = stdoutPipeWriter

	commander.Execute(c, s, req)

	stdoutPipeReader.Close()
	stdoutPipeWriter.Close()
	stderrPipeReader.Close()
	stderrPipeWriter.Close()
	<-quitter
	<-quitter
}
