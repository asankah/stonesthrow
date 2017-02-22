package stonesthrow

import (
	"sync"
)

type RequestHandler func(*Session, RequestMessage) error

type Handler struct {
	doc     string
	handler RequestHandler
	isTest  bool
}

var handlerMap = map[string]Handler{
	"build": {
		`Build specified targets.`,
		func(s *Session, req RequestMessage) error {
			err := s.SyncWorkdir(req.Revision)
			if err != nil {
				return err
			}
			return s.BuildTargets(req.Arguments...)
		}, false},

	"clean": {
		`Clean specified targets.`,
		func(s *Session, req RequestMessage) error {
			return s.CleanTargets(req.Arguments...)
		}, false},

	"clobber": {
		`Clobber the build directory.`,
		func(s *Session, req RequestMessage) error {
			return s.Clobber(len(req.Arguments) == 1 && req.Arguments[0] == "force")
		}, false},

	"ping": {
		`Diagnostic. Responds with a pong.`,
		func(s *Session, req RequestMessage) error {
			s.channel.Info("Pong")
			return nil
		}, false},

	"prepare": {
		`Prepare build directory. Runs 'mb gen'.`,
		func(s *Session, req RequestMessage) error {
			return s.PrepareBuild()
		}, false},

	"status": {
		`Run 'git status'.`,
		func(s *Session, req RequestMessage) error {
			return s.GitStatus()
		}, false},

	"sync": {
		`Run 'gclient sync'.`,
		func(s *Session, req RequestMessage) error {
			return s.RunGclientSync()
		}, false}}

var initOnce sync.Once


func CommandNeedsRevision(command string) bool {
	switch command {
	case "status", "prepare", "ping", "clobber", "help", "quit", "list":
		return false

	default:
		return true
	}
}

func AddHandler(command string, doc string, handler RequestHandler) {
	handlerMap[command] = Handler{doc, handler, false}
}

func AddTestHandler(command string, handler RequestHandler) {
	handlerMap[command] = Handler{"", handler, true}
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
	for command, handler := range handlerMap {
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
	for target, _ := range allTestTargets {
		AddTestHandler(target, func(s *Session, req RequestMessage) error {
			return s.RunTestTarget(target, req.Arguments, req.Revision)
		})
	}
}

func DispatchRequest(s *Session, req RequestMessage) {
	initOnce.Do(func() { addDynamicHandlers(s) })

	handler, ok := handlerMap[req.Command]
	if !ok {
		s.channel.Error("Invalid method")
		return
	}
	err := handler.handler(s, req)
	if err != nil {
		s.channel.Error(err.Error())
	}
}
