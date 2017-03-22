package stonesthrow

import (
	"context"
	"flag"
	"github.com/google/subcommands"
	"google.golang.org/grpc"
	"io"
)

type ClientConnection struct {
	ClientConfig  Config
	ServerConfig  Config
	RpcConnection *grpc.ClientConn
	Sink          OutputSink
	Executor      Executor
}

func (c ClientConnection) IsRemote() bool {
	return c.ClientConfig.Host != c.ServerConfig.Host
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

var DefaultHandlers = []CommandHandler{
	{"branch",
		"Repository",
		`List local branches.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
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

	{"build", "Platform",
		`Build specified targets.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			builder_client := NewPlatformBuildHostClient(conn.RpcConnection)
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

	{"clean", "Platform",
		`'clean build' cleans the build directory, while 'clean source' cleans the source directory.`, "",
		func(f *flag.FlagSet) {
			f.Bool("out", false, "Clean the output directory.")
			f.Bool("src", false, "Clean the source directory.")
			f.Bool("force", false,
				"Actually do the cleaning. Without this flag, the command merely lists which files would be affected.")
		},
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			outValue := f.Lookup("out")
			forceValue := f.Lookup("force")

			builder_client := NewPlatformBuildHostClient(conn.RpcConnection)
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

	{"ping", "Service control",
		`Diagnostic. Responds with a pong.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			service_host_client := NewServiceHostClient(conn.RpcConnection)
			ping_result, err := service_host_client.Ping(ctx, &PingOptions{Ping: "Ping!"})
			if err != nil {
				return err
			}
			return conn.Sink.OnPong(ping_result)
		}},

	{"prepare", "Platform",
		`Prepare build directory. Runs 'mb gen'.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			build_host_client := NewPlatformBuildHostClient(conn.RpcConnection)
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

	{"pull", "Repository",
		`Pull a specific branch or branches from upstream.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			if len(f.Args()) == 0 {
				return NewInvalidArgumentError("No branch specified")
			}
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
			event_stream, err := repo_host_client.PullFromUpstream(ctx, &BranchList{Branch: f.Args()})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"push", "Repository",
		`Push local branches upstream.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			branches := f.Args()
			if len(branches) == 0 {
				branches = []string{"HEAD"}
			}
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
			event_stream, err := repo_host_client.PushToUpstream(ctx, &BranchList{Branch: branches})
			if err != nil {
				return err
			}
			return conn.Sink.Drain(event_stream)
		}},

	{"status", "Repository",
		`Run 'git status'.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
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

	{"sync", "Repository",
		`Run 'gclient sync'.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
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

	{"sync_workdir", "Repository",
		`Synchronize remote work directory with local.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
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

	{"ru", "Repository",
		`Rebase-update.`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			repo_host_client := NewRepositoryHostClient(conn.RpcConnection)
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

	{"quit", "Service control",
		`Quit server`, "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			service_host_client := NewServiceHostClient(conn.RpcConnection)
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

	{"list", "Platform",
		"List available targets", "", nil,
		func(ctx context.Context, conn *ClientConnection, f *flag.FlagSet) error {
			builder_client := NewPlatformBuildHostClient(conn.RpcConnection)
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
		}}}

func InvokeCommandline(
	ctx context.Context,
	client_config Config,
	server_config Config,
	sink OutputSink,
	rpc_connection *grpc.ClientConn,
	arguments ...string) error {
	flagset := flag.NewFlagSet("", flag.ContinueOnError)

	stdoutPipeReader, stdoutPipeWriter := io.Pipe()
	stderrPipeReader, stderrPipeWriter := io.Pipe()
	quitter := make(chan error)

	go func() {
		sink.DrainReader(CommandOutputEvent{Stream: CommandOutputEvent_OUT}, stdoutPipeReader)
		quitter <- nil
	}()
	go func() {
		sink.DrainReader(CommandOutputEvent{Stream: CommandOutputEvent_ERR}, stderrPipeReader)
		quitter <- nil
	}()

	commander := subcommands.NewCommander(flagset, "st_client")
	commander.Error = stderrPipeWriter
	commander.Output = stdoutPipeWriter

	for _, handler := range DefaultHandlers {
		commander.Register(handler, handler.group)
	}
	commander.Register(commander.FlagsCommand(), "")
	commander.Register(commander.HelpCommand(), "")

	err := flagset.Parse(arguments)
	if err != nil {
		return NewInvalidArgumentError("invalid commandline arguments: %#v", arguments)
	}

	conn := &ClientConnection{
		ClientConfig:  client_config,
		ServerConfig:  server_config,
		Sink:          sink,
		RpcConnection: rpc_connection,
		Executor:      NewJobEventExecutor(client_config.Host.Name, client_config.GetSourcePath(), nil, sink)}
	commander.Execute(ctx, conn)

	stdoutPipeReader.Close()
	stdoutPipeWriter.Close()
	stderrPipeReader.Close()
	stderrPipeWriter.Close()
	<-quitter
	<-quitter
	return nil
}
