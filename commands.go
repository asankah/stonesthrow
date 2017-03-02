package stonesthrow

import (
	"context"
	"flag"
	"github.com/google/subcommands"
	"sync"
)

type FlagSetter func(*flag.FlagSet)
type RequestHandler func(context.Context, *Session, RequestMessage, *flag.FlagSet) error

type CommandHandler struct {
	name       string
	synopsis   string
	usage      string
	flagSetter FlagSetter
	handler    RequestHandler

	needsRevision    bool
	isHiddenFromHelp bool
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
	session := args[0].(*Session)
	request := args[1].(RequestMessage)

	err := h.handler(ctx, session, request, f)
	if err == InvalidArgumentError {
		return subcommands.ExitUsageError
	}
	if err != nil {
		session.channel.Error(err.Error())
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

func (h CommandHandler) NeedsRevision() bool {
	return h.needsRevision
}

var (
	flagset            *flag.FlagSet
	commander          *subcommands.Commander
	handlerMap         map[string]*CommandHandler
	initOnce           sync.Once
	initConfigHandlers sync.Once
)

var DefaultHandlers = []CommandHandler{
	CommandHandler{
		"branch",
		`List local branches.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.config.Repository.CheckHere(
				ctx, s, "git", "branch", "--list", "-vvv")
		}, false, false},

	CommandHandler{
		"build",
		`Build specified targets.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			err := s.SyncWorkdir(ctx, req.Revision)
			if err != nil {
				return err
			}
			return s.BuildTargets(ctx, req.Arguments...)
		},
		true, false},

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
					return s.config.Repository.CheckHere(ctx, s, "git", "clean", "-f")
				} else {
					s.channel.Info("Specify 'clean source force' to remove files not recognized by git.")
					return s.config.Repository.CheckHere(ctx, s, "git", "clean", "-n")
				}
			}
			return InvalidArgumentError
		}, false, false},

	CommandHandler{
		"clobber",
		`Clobber the build directory.`, "",
		func(f *flag.FlagSet) {
			f.Bool("force", false, "Actually clobber. The command doesn't do anything if this flag is not specified.")
		},
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.Clobber(ctx, f.Lookup("force").Value.String() == "true")
		}, false, false},

	CommandHandler{"ping",
		`Diagnostic. Responds with a pong.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			s.channel.Info("Pong")
			return nil
		}, false, false},

	CommandHandler{"prepare",
		`Prepare build directory. Runs 'mb gen'.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.PrepareBuild(ctx)
		}, false, false},

	CommandHandler{"pull",
		`Pull a specific branch from upstream.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			if len(req.Arguments) != 1 {
				s.channel.Error("Need to specify branch")
				return InvalidArgumentError
			}
			err := s.config.Repository.GitFetch(ctx, s, req.Arguments[0])
			if err != nil {
				return err
			}
			return s.config.Repository.GitCheckoutRevision(ctx, s, req.Arguments[0])
		}, false, false},

	CommandHandler{"push",
		`Push the current branch to upstream.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.PushCurrentBranch(ctx)
		}, false, false},

	CommandHandler{"status",
		`Run 'git status'.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.GitStatus(ctx)
		}, false, false},

	CommandHandler{"sync",
		`Run 'gclient sync'.`, "", nil,
		func(ctx context.Context, s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.RunGclientSync(ctx)
		},
		true, false},

	CommandHandler{"list",
		"List available targets", "",
		func(f *flag.FlagSet) {
			f.Bool("tests", false, "Lists test targets only")
		},
		handleList, false, false}}

func AddHandler(command string, doc string, handler RequestHandler) {
	initialize()
	newHandler := CommandHandler{command, doc, "", nil, handler, false, false}
	handlerMap[newHandler.name] = &newHandler
	commander.Register(newHandler, "")
}

func AddTestHandler(command string, handler RequestHandler) {
	initialize()
	newHandler := CommandHandler{command, "Runs the specific test target.", "",
		nil, handler, true, true}
	handlerMap[newHandler.name] = &newHandler
	commander.Register(newHandler, "test")
}

func handleHelp(ctx context.Context, s *Session, _ RequestMessage, _ *flag.FlagSet) error {
	var commandList CommandListMessage
	commandList.Repositories = make(map[string]Repository)

	var chromeRepo Repository
	chromeRepo.BuildPath = s.config.GetBuildPath()
	chromeRepo.SourcePath = s.config.GetSourcePath()
	chromeRepo.Revistion, _ = s.config.Repository.GitRevision(ctx, s, "HEAD")
	commandList.Synposis = "Remote build runner"
	commandList.ConfigFile = s.config.ConfigurationFile.FileName
	commandList.Repositories["chrome"] = chromeRepo

	commandList.Commands = make(map[string]Command)
	for _, handler := range DefaultHandlers {
		if handler.isHiddenFromHelp {
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
	allTargets, _ := s.GetAllTargets(testsOnly)
	var commandList CommandListMessage
	commandList.Synposis = "Known build targets."
	commandList.Commands = allTargets
	s.channel.ListCommands(commandList)
	return nil
}

func initialize() {
	initOnce.Do(func() {
		flagset = flag.NewFlagSet("", flag.ContinueOnError)
		commander = subcommands.NewCommander(flagset, "")
		handlerMap = make(map[string]*CommandHandler)

		for idx, handler := range DefaultHandlers {
			commander.Register(handler, "")
			handlerMap[handler.name] = &DefaultHandlers[idx]
		}
	})
}

func initializePerConfigHandlers(ctx context.Context, s *Session) {
	if s.config.Repository.GitConfig.Remote == "" {
		AddHandler("ru", `Run 'git rebase-update'.`,
			func(ctx context.Context, s *Session, req RequestMessage, _ *flag.FlagSet) error {
				fetch := true
				if len(req.Arguments) == 1 && req.Arguments[0] == "nofetch" {
					fetch = false
				}
				return s.GitRebaseUpdate(ctx, fetch)
			})
	} else {
		AddHandler("sync_workdir", `Synchronize remote work directory with local.`,
			func(ctx context.Context, s *Session, req RequestMessage, _ *flag.FlagSet) error {
				return s.SyncWorkdir(ctx, req.Revision)
			})
	}

	AddHandler("help", `Does what you think it does`, handleHelp)

	allTestTargets, err := s.GetAllTargets(true)
	if err != nil {
		return
	}
	for target := range allTestTargets {
		AddTestHandler(target, func(ctx context.Context, s *Session, req RequestMessage, _ *flag.FlagSet) error {
			return s.RunTestTarget(ctx, target, req.Arguments, req.Revision)
		})
	}
}

func GetHandlerForCommand(command string) (*CommandHandler, bool) {
	initialize()
	handler, ok := handlerMap[command]
	return handler, ok
}

func DispatchRequest(c context.Context, s *Session, req RequestMessage) {
	initConfigHandlers.Do(func() { initializePerConfigHandlers(c, s) })

	arguments := []string{req.Command}
	arguments = append(arguments, req.Arguments...)

	err := flagset.Parse(arguments)
	if err != nil {
		s.channel.Error("Invalid method. Use 'help' for a list of commands")
		return
	}

	commander.Execute(c, s, req)
}
