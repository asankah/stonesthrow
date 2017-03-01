package stonesthrow

import (
	"context"
	"flag"
	"github.com/google/subcommands"
	"sync"
)

type FlagSetter func(*flag.FlagSet)
type RequestHandler func(*Session, RequestMessage, *flag.FlagSet) error

type CommandHandler struct {
	name       string
	synopsis   string
	usage      string
	flagSetter FlagSetter
	handler    RequestHandler
}

func (h CommandHandler) Name() string {
	return h.name
}

func (h CommandHandler) Synposis() string {
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

	err := h.handler(s, req, f)
	if err == InvalidArgumentError {
		return subcommands.ExitUsageError
	}
	if err != nil {
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

var allHandlers = []CommandHandler{
	CommandHandler{
		"branch",
		`List local branches.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.config.Repository.CheckHere(
				s, "git", "branch", "--list", "-vvv")
		}},

	CommandHandler{
		"build",
		`Build specified targets.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			err := s.SyncWorkdir(req.Revision)
			if err != nil {
				return err
			}
			return s.BuildTargets(req.Arguments...)
		}},

	CommandHandler{
		"clean",
		`'clean build' cleans the build directory, while 'clean source' cleans the source directory.`, "",
		func(f *flag.FlagSet) {
			f.Bool("out", false, "Clean the output directory.")
			f.Bool("src", false, "Clean the source directory.")
			f.Bool("force", false,
				"Actually do the cleaning. Without this flag, the command merely lists which files would be affected.")
		},
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			outValue := f.Lookup("out")
			srcValue := f.Lookup("src")
			forceValue := f.Lookup("force")

			if outValue.Value.String() == "true" {
				return s.CleanTargets(req.Arguments...)
			}

			if srcValue.Value.String() == "true" {
				if forceValue.Value.String() == "true" {
					return s.config.Repository.CheckHere(s, "git", "clean", "-f")
				} else {
					s.channel.Info("Specify 'clean source force' to remove files not recognized by git.")
					return s.config.Repository.CheckHere(s, "git", "clean", "-n")
				}
			}
			return InvalidArgumentError
		}},

	CommandHandler{
		"clobber",
		`Clobber the build directory.`, "",
		func(f *flag.FlagSet) {
			f.Bool("force", false, "Actually clobber. The command doesn't do anything if this flag is not specified.")
		},
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.Clobber(f.Lookup("force").Value.String() == "true")
		}},

	CommandHandler{"ping",
		`Diagnostic. Responds with a pong.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			s.channel.Info("Pong")
			return nil
		}},

	CommandHandler{"prepare",
		`Prepare build directory. Runs 'mb gen'.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.PrepareBuild()
		}},

	CommandHandler{"pull",
		`Pull a specific branch from upstream.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			if len(req.Arguments) != 1 {
				s.channel.Error("Need to specify branch")
				return InvalidArgumentError
			}
			err := s.config.Repository.GitFetch(s, req.Arguments[0])
			if err != nil {
				return err
			}
			return s.config.Repository.GitCheckoutRevision(s, req.Arguments[0])
		}},

	CommandHandler{"push",
		`Push the current branch to upstream.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.PushCurrentBranch()
		}},

	CommandHandler{"status",
		`Run 'git status'.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.GitStatus()
		}},

	CommandHandler{"sync",
		`Run 'gclient sync'.`, "", nil,
		func(s *Session, req RequestMessage, f *flag.FlagSet) error {
			return s.RunGclientSync()
		}}}

var initOnce sync.Once

func CommandNeedsRevision(command string) bool {
	switch command {
	case "status", "prepare", "ping", "clobber", "help", "quit",
		"list", "jobs", "killall", "join", "push", "clean", "sync":
		return false

	default:
		return true
	}
}

func AddHandler(command string, doc string, handler RequestHandler) {
	allHandlers = append(allHandlers, CommandHandler{command, doc, "", nil, handler})
}

func AddTestHandler(command string, handler RequestHandler) {
	allHandlers = append(allHandlers, CommandHandler{command, "Runs the specific test target.",
		`Use 'list tests' to retrieve a list of known test targets.

Arguments are as follows:
'all'           : Run all tests in suite.
'withoutput'    : Adds --test-launcher-print-test-stdio=always.
any option      : Passed along to test runner.
any other token : Treated as a test filter and passed along to the test runner using --gtest_filter.

E.g.: 'net_unittests foo*' is expanded into 'net_unttests --gtest_filter=foo*'
`, nil, handler})
}

func handleHelp(s *Session, req RequestMessage) error {
	var commandList CommandListMessage
	commandList.Repositories = make(map[string]Repository)

	var chromeRepo Repository
	chromeRepo.BuildPath = s.config.GetBuildPath()
	chromeRepo.SourcePath = s.config.GetSourcePath()
	chromeRepo.Revistion, _ = s.config.Repository.GitRevision(s, "HEAD")
	commandList.Synposis = "Remote build runner"
	commandList.ConfigFile = s.config.ConfigurationFile.FileName
	commandList.Repositories["chrome"] = chromeRepo

	commandList.Commands = make(map[string]Command)
	for command, handler := range allHandlers {
		if handler.isTest {
			continue
		}
		var c Command
		c.Doc = handler.doc
		commandList.Commands[command] = c
	}

	commandList.Commands["<any test target>"] = Command{
		Doc: `Runs the specific test target. Use 'list tests' to retrieve a list of known test targets.

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

func handleList(s *Session, req RequestMessage) error {
	var (
		testsOnly = false
	)
	if len(req.Arguments) == 1 && req.Arguments[0] == "tests" {
		testsOnly = true
	}
	allTargets, _ := s.GetAllTargets(testsOnly)
	var commandList CommandListMessage
	commandList.Synposis = "Known build targets."
	commandList.Commands = allTargets
	s.channel.ListCommands(commandList)
	return nil
}

func addDynamicHandlers(s *Session) {
	AddHandler("help", "Does what you think it does.", handleHelp)
	AddHandler("list", "Lists targets that are valid for 'build' and 'test'.", handleList)

	if s.config.Repository.GitRemote == "" {
		AddHandler("ru", `Run 'git rebase-update'.`,
			func(s *Session, req RequestMessage) error {
				fetch := true
				if len(req.Arguments) == 1 && req.Arguments[0] == "nofetch" {
					fetch = false
				}
				return s.GitRebaseUpdate(fetch)
			})
	} else {
		AddHandler("sync_workdir", `Synchronize remote work directory with local.`,
			func(s *Session, req RequestMessage) error {
				return s.SyncWorkdir(req.Revision)
			})
	}

	allTestTargets, err := s.GetAllTargets(true)
	if err != nil {
		return
	}
	for target := range allTestTargets {
		AddTestHandler(target, func(s *Session, req RequestMessage) error {
			return s.RunTestTarget(target, req.Arguments, req.Revision)
		})
	}
}

func DispatchRequest(s *Session, req RequestMessage) {
	initOnce.Do(func() { addDynamicHandlers(s) })

	handler, ok := allHandlers[req.Command]
	if !ok {
		s.channel.Error("Invalid method")
		return
	}
	err := handler.handler(s, req)
	if err != nil {
		s.channel.Error(err.Error())
	}
}
