package stonesthrow

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var jobId int32

type Job struct {
	Id             int32
	Session        Session
	Request        RequestMessage
	StartTime      time.Time
	Running        bool
	CompletionCond *sync.Cond
	Processes      []ProcessRecord
}

type Server struct {
	config     Config
	quitSignal chan error

	activeJobsMutex sync.Mutex
	activeJobs      map[int32]Job
	nextProcessId   int32
	activeProcesses map[int32]*ProcessRecord
}

func (s *Server) AddProcess(jobId int32, command []string, process *os.Process) func(*os.ProcessState) {
	s.activeJobsMutex.Lock()
	thisProcessId := s.nextProcessId
	s.nextProcessId += 1
	if s.activeProcesses == nil {
		s.activeProcesses = make(map[int32]*ProcessRecord)
	}
	job := s.activeJobs[jobId]
	if job.Processes == nil {
		job.Processes = []ProcessRecord{}
	}
	job.Processes = append(job.Processes, ProcessRecord{
		Process:   process,
		Command:   command,
		StartTime: time.Now(),
		Running:   true})
	processRecord := &job.Processes[len(job.Processes)-1]
	s.activeProcesses[thisProcessId] = processRecord
	s.activeJobs[jobId] = job
	s.activeJobsMutex.Unlock()

	return func(state *os.ProcessState) {
		s.activeJobsMutex.Lock()
		processRecord.Running = false
		processRecord.EndTime = time.Now()
		processRecord.SystemTime = state.SystemTime()
		processRecord.UserTime = state.UserTime()
		delete(s.activeProcesses, thisProcessId)
		s.activeJobsMutex.Unlock()
	}
}

func (s *Server) startSession(c net.Conn, quitChannel chan error) {
	defer c.Close()

	readerWriter := bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
	jsConn := jsonConnection{stream: readerWriter}
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
		jobId, err := strconv.Atoi(req.Arguments[0])
		if err != nil {
			channel.Error(err.Error())
			return
		}
		s.activeJobsMutex.Lock()
		job, ok := s.activeJobs[int32(jobId)]
		if ok {
			job.Session.channel.SwapConnection(jsConn)
		}
		for ok && job.Running {
			job.CompletionCond.Wait()
			job, ok = s.activeJobs[int32(jobId)]
		}
		s.activeJobsMutex.Unlock()
	}

	myJobId := atomic.AddInt32(&jobId, 1)

	session := Session{config: s.config, channel: channel,
		processAdder: func(c []string, p *os.Process) func(*os.ProcessState) {
			return s.AddProcess(myJobId, c, p)
		}}

	myJob := Job{
		Id:             myJobId,
		Session:        session,
		Request:        *req,
		StartTime:      time.Now(),
		Running:        true,
		CompletionCond: sync.NewCond(&s.activeJobsMutex)}


	s.activeJobsMutex.Lock()
	s.activeJobs[myJob.Id] = myJob
	s.activeJobsMutex.Unlock()
	log.Printf("Dispatching request %s", req.Command)

	DispatchRequest(&session, *req)

	s.activeJobsMutex.Lock()
	myJob.Running = false
	myJob.CompletionCond.Broadcast()
	delete(s.activeJobs, myJob.Id)
	s.activeJobsMutex.Unlock()
	return
}

func (s *Server) Quit() {
	s.quitSignal <- nil
}

func (s *Server) listJobsHandler(session *Session, req RequestMessage) {
	var jobList JobListMessage
	jobList.Jobs = []JobRecord{}
	now := time.Now()
	s.activeJobsMutex.Lock()
	for _, job := range s.activeJobs {
		jobRecord := JobRecord{
			Id:        job.Id,
			Request:   job.Request,
			StartTime: job.StartTime,
			Duration:  now.Sub(job.StartTime)}
		jobRecord.Processes = []Process{}
		for _, proc := range job.Processes {
			endTime := now
			if !proc.Running {
				endTime = proc.EndTime
			}
			jobRecord.Processes = append(jobRecord.Processes, Process{
				Command: proc.Command,
				StartTime: proc.StartTime,
				Duration: endTime.Sub(proc.StartTime),
				Running: proc.Running,
				EndTime: proc.EndTime,
				SystemTime: proc.SystemTime,
				UserTime: proc.UserTime})
		}
		jobList.Jobs = append(jobList.Jobs, jobRecord)
	}
	s.activeJobsMutex.Unlock()
	session.channel.ListJobs(jobList)
}

func (s *Server) getActiveProcessList() []ProcessRecord {
	processList := []ProcessRecord{}
	s.activeJobsMutex.Lock()
	for _, p := range s.activeProcesses {
		processList = append(processList, *p)
	}
	s.activeJobsMutex.Unlock()
	return processList
}

func (s *Server) killProcessHandler(session *Session, req RequestMessage) {
	processList := s.getActiveProcessList()
	if len(processList) == 0 {
		session.channel.Info("No active processes")
		return
	}

	for _, proc := range processList {
		session.channel.Info(fmt.Sprintf("Killing process: %s", proc.Command))
		proc.Process.Kill()
	}
}

func (s *Server) Run(config Config) error {
	jobId = 1
	s.activeJobs = make(map[int32]Job)
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
	listener, err := net.Listen("tcp", s.config.GetListenAddress())
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
			go s.startSession(conn, s.quitSignal)

		case err = <-s.quitSignal:
			return err
		}
	}
}
