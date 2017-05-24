package stonesthrow

import (
	"context"
	"flag"
	"fmt"
	"github.com/google/subcommands"
	net_context "golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"io"
	"os"
	"path"
	"path/filepath"
)

type FileExtractor struct {
	receiver   JobEventReceiver
	sender     JobEventSender
	base_path  string
	dont_write bool
}

func (f FileExtractor) Recv() (*JobEvent, error) {
	for {
		j, err := f.receiver.Recv()
		if err != nil {
			return nil, err
		}

		if j.GetZippedContent() != nil {
			ReceiveFiles(f.receiver.Context(), f.base_path, f.dont_write, j.GetZippedContent(), f.sender)
			continue
		}

		return j, err
	}
}

func (f FileExtractor) Header() (metadata.MD, error) { return f.receiver.Header() }
func (f FileExtractor) Trailer() metadata.MD         { return f.receiver.Trailer() }
func (f FileExtractor) CloseSend() error             { return f.receiver.CloseSend() }
func (f FileExtractor) Context() net_context.Context { return f.receiver.Context() }
func (f FileExtractor) SendMsg(m interface{}) error  { return f.receiver.SendMsg(m) }
func (f FileExtractor) RecvMsg(m interface{}) error  { return f.receiver.RecvMsg(m) }

type ClientConnection struct {
	ClientConfig Config
	ServerConfig Config
	Sinkerator   func(Config) OutputSink
	Sink         OutputSink
	Executor     Executor

	platform        string
	repository      string
	config_filename string
	rpcConnection   *grpc.ClientConn
}

func (c ClientConnection) IsRemote() bool {
	return c.ClientConfig.Host != c.ServerConfig.Host
}

func (c *ClientConnection) SetupTopLevelFlags(f *flag.FlagSet) {
	default_server_platform := path.Base(os.Args[0])
	default_config_file := GetDefaultConfigFileName()

	f.StringVar(&c.platform, "platform", default_server_platform, "target server platform.")
	f.StringVar(&c.repository, "repository", "", "repository name. defaults to the repository corresponding to the current directory.")
	f.StringVar(&c.config_filename, "config", default_config_file, "configuration file.")
}

func (c *ClientConnection) InitFromFlags(ctx context.Context, f *flag.FlagSet) error {
	if c.platform == "" {
		return fmt.Errorf("platform flag is required")
	}

	if c.config_filename == "" {
		return fmt.Errorf("config flag is required")
	}

	var config_file ConfigurationFile
	var client_config, server_config Config
	err := config_file.ReadFrom(c.config_filename)
	if err != nil {
		return err
	}

	err = server_config.Select(&config_file, "", c.repository, c.platform)
	if err != nil {
		return err
	}

	err = client_config.SelectForClient(&config_file, c.repository)
	if err != nil {
		return err
	}

	c.ClientConfig = client_config
	c.ServerConfig = server_config
	c.Sink = c.Sinkerator(server_config)
	c.Executor = NewJobEventExecutor(client_config.Host.Name, client_config.GetSourcePath(), nil, c.Sink)
	return nil
}

func (c *ClientConnection) GetConnection(ctx context.Context) (*grpc.ClientConn, error) {
	if c.rpcConnection != nil {
		return c.rpcConnection, nil
	}
	rpc_connection, err := ConnectTo(ctx, c.ClientConfig, c.ServerConfig)
	if err != nil {
		return nil, fmt.Errorf("can't connect to remote %s : %s", c.ServerConfig.Host.Name, err.Error())
	}
	c.rpcConnection = rpc_connection
	return rpc_connection, nil
}

type FlagSetter func(*flag.FlagSet)
type RequestHandler func(context.Context, *ClientConnection, *flag.FlagSet) error

type CommandHandler struct {
	name       string
	group      string
	synopsis   string
	usage      string
	flagSetter FlagSetter
	handler    RequestHandler
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

	conn := args[0].(*ClientConnection)

	err := h.handler(ctx, conn, f)
	if IsInvalidArgumentError(err) {
		return subcommands.ExitUsageError
	}
	if err != nil && err != io.EOF {
		conn.Sink.OnJobEvent(&JobEvent{
			LogEvent: &LogEvent{
				Severity: LogEvent_ERROR,
				Msg:      err.Error()}})
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

var (
	Flag_BranchFilter          string
	Flag_Force                 bool
	Flag_IncludeConfig         bool
	Flag_NoWrite               bool
	Flag_Out                   bool
	Flag_Recursive             bool
	Flag_Source                bool
	Flag_TargetPath            string
	Flag_AutomaticDependencies bool
)

var DefaultHandlers = []CommandHandler{
	{"branch",
		"repository management",
		`list branches.`, `Usage: branch [-c] [branch filter]
`,
		func(f *flag.FlagSet) {
			f.BoolVar(&Flag_IncludeConfig, "c", false, "include branch configuration")
		},
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			repo_host_client := NewRepositoryHostClient(rpc_connection)
			branch_config_options := BranchConfigOptions{
				BranchSpec:       Flag_BranchFilter,
				IncludeGitConfig: Flag_IncludeConfig}
			repo_info, err := repo_host_client.GetBranchConfig(ctx, &branch_config_options)

			if err != nil {
				return err
			}
			return conn.Sink.OnGitRepositoryInfo(repo_info)
		}},

	{"shell", "builder",
		`run a command in the build directory.`, `Usage: run command options ...

The shell command specified by [command] and [options] will be executed in the output directory corresponding to the target platform. The following symbols will be expanded if found:

    {src} : Expands to the full path to the source directory for the repository.
    {out} : Expands to the full path to the output directory for the platform.
    {st}  : Expands to the full path to the host's StonesThrow installation.

The tokens are expanded in the option value for '-dir' in addition to the command specification. Shell globs will not be expanded on the remote side.

E.g.:
    run {src}/foo/bar {out}/a

... executes foo/bar relative to the source directory. The only argument to bar is the absolute path to |a| which is assumed to be in the output directory.
`, func(f *flag.FlagSet) {
			f.StringVar(&Flag_TargetPath, "dir", "{out}", "directory under which the command should be executed.")
		},
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			builder_client := NewBuildHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(
				ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			run_options := RunOptions{
				Repository: repo_state.Repository,
				Revision:   repo_state.Revision,
				Platform:   conn.ServerConfig.Platform.Name,
				Command: &ShellCommand{
					Directory: Flag_TargetPath,
					Command:   f.Args()}}
			event_stream, err := builder_client.RunShellCommand(ctx, &run_options)
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"get", "builder",
		`get a file or multiple files from a build directory.`, `Usage: get [-src|-out] [-n] [-r] path [glob]
`,
		func(f *flag.FlagSet) {
			f.BoolVar(&Flag_NoWrite, "n", false, "don't write any files. Just list what would've been transferred.")
			f.BoolVar(&Flag_Recursive, "r", false, "recursively select files that match GLOB")
			f.StringVar(&Flag_TargetPath, "out", "", "target path. received files will be placed relative to this path.")
		},
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			if f.NArg() == 0 {
				return NewInvalidArgumentError("no path or glob specified")
			}

			if f.NArg() > 2 {
				return NewInvalidArgumentError("too many paths specified")
			}

			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}

			options := FetchFileOptions{}
			if f.NArg() == 1 {
				options.FilenameGlob = f.Arg(0)
			} else {
				options.RelativePath = f.Arg(0)
				options.FilenameGlob = f.Arg(1)
			}
			options.Recurse = Flag_Recursive

			builder_client := NewBuildHostClient(rpc_connection)
			stream, err := builder_client.FetchFile(ctx, &options)
			if err != nil {
				return err
			}

			base_path := Flag_TargetPath
			if base_path == "" {
				base_path = filepath.Join(conn.ClientConfig.Repository.SourcePath, conn.ServerConfig.Platform.RelativeBuildPath)
			}
			base_path, _ = filepath.Abs(base_path)
			extractor := FileExtractor{
				receiver:   stream,
				sender:     conn.Sink,
				base_path:  base_path,
				dont_write: Flag_NoWrite}
			return conn.Sink.Drain(extractor)
		}},

	{"ping", "service control",
		`diagnostic. responds with a pong.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			service_host_client := NewServiceHostClient(rpc_connection)
			ping_result, err := service_host_client.Ping(ctx, &PingOptions{Ping: "Ping!"})
			if err != nil {
				return err
			}
			return conn.Sink.OnPong(ping_result)
		}},

	{"pull", "repository management",
		`pull a specific branch or branches from upstream.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			repo_host_client := NewRepositoryHostClient(rpc_connection)
			event_stream, err := repo_host_client.PullFromUpstream(ctx, &BranchList{Branch: f.Args()})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"push", "repository management",
		`push local branches upstream.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			branches := f.Args()
			if len(branches) == 0 {
				branches = []string{"HEAD"}
			}
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			repo_host_client := NewRepositoryHostClient(rpc_connection)
			event_stream, err := repo_host_client.PushToUpstream(ctx, &BranchList{Branch: branches})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"status", "repository management",
		`run 'git status'.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			repo_host_client := NewRepositoryHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			event_stream, err := repo_host_client.Status(ctx, repo_state)
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"sync_workdir", "repository management",
		`synchronize remote work directory with local.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			repo_host_client := NewRepositoryHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, conn.IsRemote())
			if err != nil {
				return err
			}
			event_stream, err := repo_host_client.SyncRemote(ctx, repo_state)
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"quit", "service control",
		`quit server`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			service_host_client := NewServiceHostClient(rpc_connection)
			event_stream, err := service_host_client.Shutdown(ctx, &ShutdownOptions{})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"update", "service control",
		`self-update`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			service_host_client := NewServiceHostClient(rpc_connection)
			event_stream, err := service_host_client.SelfUpdate(ctx, &SelfUpdateOptions{})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"list_targets", "builder",
		"list available targets", "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			builder_client := NewBuildHostClient(rpc_connection)
			target_list, err := builder_client.ListTargets(ctx, &ListTargetsOptions{})
			if err != nil {
				return err
			}
			return conn.Sink.OnTargetList(target_list)
		}},

	{"passthrough", "service control",
		"run passthrough client.", "Only used internally", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			return RunPassthroughClient(conn.ClientConfig, conn.ServerConfig)
		}}}

func IsInvokingBuiltinCommand(flagset *flag.FlagSet) bool {
	if flagset.NArg() == 0 {
		return false
	}
	command := flagset.Arg(0)
	for _, handler := range DefaultHandlers {
		if handler.name == command {
			return true
		}
	}
	return false
}

func RegisterRemoteCommands(ctx context.Context, conn *ClientConnection, commander *subcommands.Commander) error {
	rpc_connection, err := conn.GetConnection(ctx)
	if err != nil {
		return err
	}

	builder_client := NewBuildHostClient(rpc_connection)
	command_list, err := builder_client.ListScriptCommands(ctx, &ListCommandsOptions{
		Repository: conn.ServerConfig.Repository.Name,
		Platform:   conn.ServerConfig.Platform.Name})
	if err != nil {
		return err
	}

	for _, command := range command_list.GetCommand() {
		depends_on_source := command.GetDependsOnSource()
		command_name := command.GetName()[0]
		handler := CommandHandler{
			command.GetName()[0],
			"builder (script)",
			command.GetDescription(),
			command.GetUsage(),
			nil,
			func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
				rpc_connection, err := conn.GetConnection(ctx)
				if err != nil {
					return err
				}
				builder_client := NewBuildHostClient(rpc_connection)
				repo_state, err := GetRepositoryState(
					ctx, conn.ClientConfig.Repository, conn.Executor,
					depends_on_source && conn.IsRemote())
				if err != nil {
					return err
				}

				args := append([]string{command_name}, f.Args()...)
				ro := RunOptions{
					Repository: repo_state.Repository,
					Revision:   repo_state.Revision,
					Platform:   conn.ServerConfig.Platform.Name,
					Command: &ShellCommand{
						Command:   args,
						Directory: "{out}"}}

				event_stream, err := builder_client.RunScriptCommand(ctx, &ro)
				if err != nil {
					return err
				}

				return conn.Sink.Drain(event_stream)
			}}
		commander.Register(handler, handler.group)
	}
	return nil
}

func InvokeCommandline(ctx context.Context, sinkerator func(Config) OutputSink) error {
	toplevel_flags := flag.NewFlagSet("", flag.ContinueOnError)
	conn := &ClientConnection{Sinkerator: sinkerator}
	conn.SetupTopLevelFlags(toplevel_flags)

	// We expect the initial Parse call to fail since it doesn't recognize
	// the subcommands which haven't been added yet. The only thing we are
	// interested in is if the user specified -h at the toplevel.
	toplevel_flag_err := toplevel_flags.Parse(os.Args[1:])

	err := conn.InitFromFlags(ctx, toplevel_flags)
	if err != nil {
		toplevel_flags.Usage()
		return err
	}

	child_flags := flag.NewFlagSet("", flag.ContinueOnError)
	commander := subcommands.NewCommander(child_flags, os.Args[0])
	for _, handler := range DefaultHandlers {
		commander.Register(handler, handler.group)
	}

	if !IsInvokingBuiltinCommand(toplevel_flags) {
		err = RegisterRemoteCommands(ctx, conn, commander)
		if err != nil {
			return err
		}
	}

	commander.Register(commander.FlagsCommand(), "help and information")
	commander.Register(commander.HelpCommand(), "help and information")

	if toplevel_flag_err == flag.ErrHelp {
		commander.HelpCommand().Execute(ctx, toplevel_flags)
		return nil
	}

	err = child_flags.Parse(toplevel_flags.Args())
	if err != nil {
		return NewInvalidArgumentError("invalid commandline arguments: %#v", os.Args)
	}

	commander.Execute(ctx, conn)
	return nil
}
