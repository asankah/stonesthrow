package stonesthrow

import (
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

var nextSessionId int

type SessionInfo struct {
	Id             int
	Session        *Session
	Request        RequestMessage
	StartTime      time.Time
	Running        bool
	CompletionCond *sync.Cond
	ProcessMap     map[int]*ProcessRecord
}

type ProcessAdder interface {
	AddProcess(command []string, process *os.Process)
	RemoveProcess(process *os.Process, state *os.ProcessState)
}

type SessionTracker struct {
	Sessions map[int]*SessionInfo

	mut sync.Mutex
}

type sessionTrackerProcessAdder struct {
	j           *SessionTracker
	sessionInfo *SessionInfo
}

func (j sessionTrackerProcessAdder) AddProcess(command []string, process *os.Process) {
	j.j.AddProcessToSession(j.sessionInfo, command, process)
}

func (j sessionTrackerProcessAdder) RemoveProcess(process *os.Process, state *os.ProcessState) {
	j.j.RemoveProcessFromSession(j.sessionInfo, process, state)
}

func (j *SessionTracker) Init() {
	j.Sessions = make(map[int]*SessionInfo)
}

// AddSession adds a session to the list of tracked sessions. Calling
// AddSession is idempotent.
func (j *SessionTracker) AddSession(s *SessionInfo) {
	if s.Id != 0 {
		j.mut.Lock()
		// thrown runtime error if s was not already added.
		_ = j.Sessions[s.Id]
		j.mut.Unlock()
		return
	}
	j.mut.Lock()
	nextSessionId += 1
	s.Id = nextSessionId
	s.CompletionCond = sync.NewCond(&j.mut)
	j.Sessions[s.Id] = s
	j.mut.Unlock()
}

func (j *SessionTracker) RemoveSession(s *SessionInfo) {
	j.mut.Lock()
	s.Running = false
	delete(j.Sessions, s.Id)
	j.mut.Unlock()
	s.CompletionCond.Broadcast()
}

func (j *SessionTracker) SwapConnectionForSession(id int, conn Connection) error {
	j.mut.Lock()
	defer j.mut.Unlock()
	sessionInfo, ok := j.Sessions[id]
	if !ok {
		return InvalidArgumentError
	}

	sessionInfo.Session.channel.SwapConnection(conn)
	if sessionInfo.Running {
		sessionInfo.CompletionCond.Wait()
	}
	return nil
}

func (j *SessionTracker) GetSession(id int) *SessionInfo {
	j.mut.Lock()
	s, _ := j.Sessions[id]
	j.mut.Unlock()
	return s
}

func (j *SessionTracker) GetSessionProcessAdder(s *SessionInfo) ProcessAdder {
	j.AddSession(s)
	return sessionTrackerProcessAdder{j: j, sessionInfo: s}
}

func (j *SessionTracker) AddProcessToSession(s *SessionInfo, command []string, process *os.Process) {
	j.mut.Lock()
	sessionInfo := j.Sessions[s.Id]
	if sessionInfo.ProcessMap == nil {
		sessionInfo.ProcessMap = make(map[int]*ProcessRecord)
	}
	sessionInfo.ProcessMap[process.Pid] = &ProcessRecord{
		Process:   process,
		Command:   command,
		StartTime: time.Now(),
		Running:   true}
	j.mut.Unlock()
}

func (j *SessionTracker) RemoveProcessFromSession(s *SessionInfo, process *os.Process, state *os.ProcessState) {
	j.mut.Lock()
	defer j.mut.Unlock()
	sessionInfo, ok := j.Sessions[s.Id]
	if !ok {
		return
	}
	processRecord, ok := sessionInfo.ProcessMap[process.Pid]
	if !ok {
		return
	}
	processRecord.Running = false
	processRecord.EndTime = time.Now()
	if state != nil {
		processRecord.SystemTime = state.SystemTime()
		processRecord.UserTime = state.UserTime()
	}
}

func (j *SessionTracker) GetJobList() JobListMessage {
	var sessionList JobListMessage
	j.mut.Lock()
	defer j.mut.Unlock()

	now := time.Now()
	for _, sessionInfo := range j.Sessions {
		jobRecord := JobRecord{
			Id:        sessionInfo.Id,
			Request:   sessionInfo.Request,
			StartTime: sessionInfo.StartTime,
			Duration:  now.Sub(sessionInfo.StartTime)}
		for _, proc := range sessionInfo.ProcessMap {
			endTime := now
			if !proc.Running {
				endTime = proc.EndTime
			}
			jobRecord.Processes = append(jobRecord.Processes, Process{
				Command:    proc.Command,
				StartTime:  proc.StartTime,
				Duration:   endTime.Sub(proc.StartTime),
				Running:    proc.Running,
				EndTime:    proc.EndTime,
				SystemTime: proc.SystemTime,
				UserTime:   proc.UserTime})
		}
		sessionList.Jobs = append(sessionList.Jobs, jobRecord)
	}
	return sessionList
}

func (j *SessionTracker) KillRunningProcesses() ProcessListMessage {
	var processList ProcessListMessage

	j.mut.Lock()
	defer j.mut.Unlock()

	for _, sessionInfo := range j.Sessions {
		for _, proc := range sessionInfo.ProcessMap {
			if !proc.Running {
				continue
			}

			proc.Process.Kill()
			processList.Processes = append(processList.Processes, Process{
				Command:   proc.Command,
				StartTime: proc.StartTime,
				Running:   proc.Running})
		}
	}
	return processList
}

type Server struct {
	config         Config
	quitSignal     chan error
	sessionTracker SessionTracker
}

func (s *Server) runSessionWithConnection(c io.ReadWriter, quitChannel chan error) {
	jsConn := &jsonConnection{in: c, out: c}
	jsConn.Init()
	channel := Channel{conn: jsConn}

	blob, err := channel.Receive()

	if err == io.EOF {
		log.Printf("Done with stream")
		return
	}

	if err != nil {
		log.Printf("Failed Receive: %s", err)
		return
	}

	req, ok := blob.(*RequestMessage)
	if !ok {
		log.Printf("Failed to decode message: %s", blob)
		return
	}

	if req.Command == "quit" {
		channel.Info("Quitting")
		quitChannel <- nil
		return
	}

	if req.Command == "join" && len(req.Arguments) == 1 {
		sessionId, err := strconv.Atoi(req.Arguments[0])
		if err != nil {
			channel.Error(err.Error())
			return
		}
		err = s.sessionTracker.SwapConnectionForSession(sessionId, jsConn)
		if err != nil {
			channel.Error(err.Error())
		}
		return
	}

	sessionInfo := SessionInfo{
		Request:   *req,
		StartTime: time.Now(),
		Running:   true}

	s.sessionTracker.AddSession(&sessionInfo)

	sessionInfo.Session = &Session{config: s.config, channel: channel,
		processAdder: s.sessionTracker.GetSessionProcessAdder(&sessionInfo)}

	log.Printf("Dispatching request %s", req.Command)
	DispatchRequest(sessionInfo.Session, *req)

	s.sessionTracker.RemoveSession(&sessionInfo)
	return
}

func (s *Server) Quit() {
	s.quitSignal <- nil
}

func (s *Server) listJobsHandler(session *Session, req RequestMessage) {
	session.channel.ListJobs(s.sessionTracker.GetJobList())
}

func (s *Server) killProcessHandler(session *Session, req RequestMessage) {
	session.channel.ListProcesses(s.sessionTracker.KillRunningProcesses())
}

func (s *Server) Run(config Config) error {
	nextSessionId = 1
	s.sessionTracker.Init()
	AddHandler("jobs", "List running jobs", func(sess *Session, req RequestMessage) error {
		s.listJobsHandler(sess, req)
		return nil
	})
	AddHandler("killall", "Kill all child processes", func(sess *Session, req RequestMessage) error {
		s.killProcessHandler(sess, req)
		return nil
	})
	AddHandler("quit", "Quit server", nil)
	AddHandler("join", `Join a running job.

The ID of the job should be specified as the only argument. Any new processes started by the job will use the newly established channel for IO. Use 'jobs' to find the job ID for a long running command.`, nil)
	if !config.IsValid() {
		return ConfigIncompleteError
	}
	s.config = config
	ep := s.config.Platform.EndpointFor(config.Host)
	if ep == nil {
		return EndpointNotFoundError
	}

	log.Printf("Starting server for %s at %s on %s. This is PID %d", config.PlatformName, ep.Address, ep.HostName, os.Getpid())
	listener, err := net.Listen(ep.Network, ep.Address)
	if err != nil {
		return err
	}

	defer listener.Close()

	connections := make(chan net.Conn)
	s.quitSignal = make(chan error)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				s.quitSignal <- err
			}
			connections <- conn
		}
	}()

	for {
		var conn net.Conn
		var err error
		select {
		case conn = <-connections:
			conn := conn
			go func() {
				s.runSessionWithConnection(conn, s.quitSignal)
				conn.Close()
			}()

		case err = <-s.quitSignal:
			return err
		}
	}
}
