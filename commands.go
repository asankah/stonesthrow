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
	default_config_file := GetDefaultConfigFile()

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

	err = server_config.SelectServerConfig(&config_file, c.platform, c.repository)
	if err != nil {
		return err
	}

	err = client_config.SelectLocalClientConfig(&config_file, c.platform, c.repository)
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
	err := conn.InitFromFlags(ctx, f)
	if err != nil {
		fmt.Printf(err.Error())
		return subcommands.ExitUsageError
	}

	err = h.handler(ctx, conn, f)
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
	Flag_BranchFilter  string
	Flag_Force         bool
	Flag_IncludeConfig bool
	Flag_NoWrite       bool
	Flag_Out           bool
	Flag_Recursive     bool
	Flag_Source        bool
	Flag_TargetPath    string
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

	{"build", "builder",
		`build specified targets.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			builder_client := NewPlatformBuildHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, conn.IsRemote())
			if err != nil {
				return err
			}
			build_options := BuildOptions{
				Platform:        conn.ServerConfig.Platform.Name,
				Targets:         f.Args(),
				RepositoryState: repo_state}
			build_client, err := builder_client.Build(ctx, &build_options)
			if err != nil {
				return err
			}
			return conn.Sink.Drain(build_client)
		}},

	{"clobber", "builder",
		`removes files from source or build directory.`, `Usage: clobber [-out|-src] [-force]

`,
		func(f *flag.FlagSet) {
			f.BoolVar(&Flag_Out, "out", false, "clean the output directory.")
			f.BoolVar(&Flag_Source, "src", false, "clean the source directory.")
			f.BoolVar(&Flag_Force, "force", false,
				"actually do the cleaning. Without this flag, the command merely lists which files would be affected.")
		},
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			builder_client := NewPlatformBuildHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			clobber_options := ClobberOptions{
				Platform:        conn.ServerConfig.Platform.Name,
				RepositoryState: repo_state,
				Target:          ClobberOptions_SOURCE,
				Force:           false}

			if Flag_Out {
				clobber_options.Target = ClobberOptions_OUTPUT
			}

			if Flag_Force {
				clobber_options.Force = true
			}

			output_client, err := builder_client.Clobber(ctx, &clobber_options)
			if err != nil {
				return err
			}

			return conn.Sink.Drain(output_client)
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

			builder_client := NewPlatformBuildHostClient(rpc_connection)
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

	{"prepare", "builder",
		`prepare build directory. Runs 'mb gen'.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			build_host_client := NewPlatformBuildHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			event_stream, err := build_host_client.Prepare(ctx, &BuildOptions{
				Platform:        conn.ServerConfig.Platform.Name,
				RepositoryState: repo_state})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
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

	{"sync", "repository management",
		`run 'gclient sync'.`, "", nil,
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
			event_stream, err := repo_host_client.SyncLocal(ctx, repo_state)
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

	{"ru", "repository management",
		`rebase-update.`, "", nil,
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
			event_stream, err := repo_host_client.RebaseUpdate(ctx, repo_state)
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
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			event_stream, err := service_host_client.Shutdown(ctx, repo_state)
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
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			event_stream, err := service_host_client.SelfUpdate(ctx, repo_state)
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"list", "builder",
		"list available targets", "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			rpc_connection, err := conn.GetConnection(ctx)
			if err != nil {
				return err
			}
			builder_client := NewPlatformBuildHostClient(rpc_connection)
			repo_state, err := GetRepositoryState(ctx, conn.ClientConfig.Repository, conn.Executor, false)
			if err != nil {
				return err
			}
			build_options := BuildOptions{
				Platform:        conn.ServerConfig.Platform.Name,
				Targets:         f.Args(),
				RepositoryState: repo_state}
			target_list, err := builder_client.ListTargets(ctx, &build_options)
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

func InvokeCommandline(ctx context.Context, sinkerator func(Config) OutputSink) error {
	flagset := flag.NewFlagSet("", flag.ContinueOnError)
	conn := &ClientConnection{Sinkerator: sinkerator}
	conn.SetupTopLevelFlags(flagset)

	commander := subcommands.NewCommander(flagset, os.Args[0])
	for _, handler := range DefaultHandlers {
		commander.Register(handler, handler.group)
	}
	commander.Register(commander.FlagsCommand(), "help and information")
	commander.Register(commander.HelpCommand(), "help and information")

	err := flagset.Parse(os.Args[1:])
	if err != nil {
		return NewInvalidArgumentError("invalid commandline arguments: %#v", os.Args)
	}

	commander.Execute(ctx, conn)
	return nil
}
