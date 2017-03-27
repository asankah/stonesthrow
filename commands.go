package stonesthrow

import (
	"context"
	"flag"
	"fmt"
	"github.com/google/subcommands"
	"google.golang.org/grpc"
	"io"
	"os"
	"path"
)

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

	f.StringVar(&c.platform, "platform", default_server_platform, "Server platform.")
	f.StringVar(&c.repository, "repository", "", "Repository")
	f.StringVar(&c.config_filename, "config", default_config_file, "Configuration file")
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

	err = server_config.SelectLocalServerConfig(&config_file, c.platform, c.repository)
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

var DefaultHandlers = []CommandHandler{
	{"branch",
		"repository management",
		`list local branches.`, "", nil,
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
			repo_info, err := repo_host_client.GetBranchConfig(ctx, repo_state)

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
			f.Bool("out", false, "clean the output directory.")
			f.Bool("src", false, "clean the source directory.")
			f.Bool("force", false,
				"actually do the cleaning. Without this flag, the command merely lists which files would be affected.")
		},
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			outValue := f.Lookup("out")
			forceValue := f.Lookup("force")

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

			if outValue.Value.String() == "true" {
				clobber_options.Target = ClobberOptions_OUTPUT
			}

			if forceValue.Value.String() == "true" {
				clobber_options.Force = true
			}

			output_client, err := builder_client.Clobber(ctx, &clobber_options)
			if err != nil {
				return err
			}

			return conn.Sink.Drain(output_client)
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
