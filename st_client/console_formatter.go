package main

import (
	"bufio"
	"fmt"
	"github.com/asankah/stonesthrow"
	"io"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"
)

type terminalOutputFilter func(string) string

type ConsoleFormatter struct {
	filterChain []terminalOutputFilter
	templates   map[string]*template.Template
	config      *stonesthrow.Config
	porcelain   bool
}

func applyRegexpTemplate(re *regexp.Regexp, template string, message string) string {
	match := re.FindStringSubmatchIndex(message)
	if match == nil {
		return message
	}
	return string(re.ExpandString([]byte(""), template, message, match))
}

func (f *ConsoleFormatter) AddFilter(filter terminalOutputFilter) {
	f.filterChain = append(f.filterChain, filter)
}

func (f *ConsoleFormatter) AddRegexTemplate(rxs string, template string) {
	re := regexp.MustCompile(rxs)
	f.AddFilter(func(s string) string {
		return applyRegexpTemplate(re, template, s)
	})
}

func (f *ConsoleFormatter) AddRegExpReplace(rxs, replaceWith string) {
	re := regexp.MustCompile(rxs)
	f.AddFilter(func(s string) string {
		return re.ReplaceAllString(s, replaceWith)
	})
}

func normalizePathSeparators(s string) string {
	if len(s) > 2 && s[1] == ':' {
		s = s[2:]
	}
	return strings.Replace(s, "\\", "/", -1)
}

func (f *ConsoleFormatter) DimProgress() {
	f.AddRegExpReplace(
		`^\[(\d*)/(\d*)\]`,
		"["+CDark("$1")+"/"+CDark("$2")+"]")
}

func (f *ConsoleFormatter) AddPathRemappers() {
	re := regexp.MustCompile(regexp.QuoteMeta(f.config.Repository.SourcePath) + `[\w/\\_-]*`)
	sourceLen := len(f.config.Repository.SourcePath)
	sourcePathRewriter := func(s string) string {
		return "/" + normalizePathSeparators(s[sourceLen:])
	}
	f.AddFilter(func(s string) string {
		return re.ReplaceAllStringFunc(s, sourcePathRewriter)
	})

	relLen := len("../../")
	relPathRewriter := func(s string) string {
		return "//" + normalizePathSeparators(s[relLen:])
	}

	re2 := regexp.MustCompile(`(?:[^\w/\-]|^)\.\.\\\.\.\\[\w/\\\-]*`)
	f.AddFilter(func(s string) string {
		return re2.ReplaceAllStringFunc(s, relPathRewriter)
	})

	re4 := regexp.MustCompile(`(?:[^\w/\-]|^)\.\./\.\./([\w/\-]*)`)
	f.AddFilter(func(s string) string {
		return re4.ReplaceAllStringFunc(s, relPathRewriter)
	})
}

func (f *ConsoleFormatter) AddNinjaFilters() {
	f.AddPathRemappers()
	f.AddRegexTemplate(
		`^FAILED: (\S+)`,
		CError("FAILED")+": while building "+CSubject("$1"))
	f.AddRegexTemplate(
		`^.*/clang\+\+.*-c (\S+).*$`,
		"    clang++ "+CDark("...")+" -c "+CSubject("$1"))
	f.AddRegexTemplate(
		`(.*):(\d+):(\d+): (\S+): (.*)$`,
		CLocation("$1")+":"+CLocation("$2")+":"+CLocation("$3")+": "+CError("$4")+": $5")
	f.AddRegExpReplace(
		` (CC|CXX|OBJCXX) `,
		CSourceBuildStep(" $1 "))
	f.AddRegExpReplace(
		` (STAMP|ACTION|AR|LINK|SOLINK|RC|LIB|LIBTOOL|LIBTOOL-STATIC|LINK\(DLL\)|ASM|COPY) `,
		CAuxBuildStep(" $1 "))
	f.DimProgress()
}

func (f *ConsoleFormatter) AddTestFilters() {
	f.AddPathRemappers()
	f.AddRegExpReplace(regexp.QuoteMeta("[ RUN      ]")+" ([^ ]*)",
		"[ "+CActionLabel("RUN")+"      ] "+CSubject("$1"))
	f.AddRegExpReplace(regexp.QuoteMeta("[  FAILED  ]")+" ([^ ]*)",
		"[  "+CError("FAILED")+"  ] "+CSubject("$1"))
	f.AddRegExpReplace(`^(.*)\((.*)\): `,
		CSubject("$1")+"("+CLocation("$2")+"): ")
	f.DimProgress()
}

func (f *ConsoleFormatter) SetupFilterChainForCommand(command []string) {
	wholeCommand := "^" + strings.Join(command, "^") + "^"
	f.filterChain = nil
	switch {
	case len(command) == 0:
		return

	case strings.Contains(wholeCommand, "^ninja^"):
		f.AddNinjaFilters()

	case strings.Contains(wholeCommand, "tests^"):
		f.AddTestFilters()
	}
}

func (f *ConsoleFormatter) ApplyFilters(s string) string {
	if f.filterChain == nil {
		return s
	}

	output := s
	for _, filter := range f.filterChain {
		output = filter(output)
	}
	return output
}

func (f *ConsoleFormatter) ClearFilters() {
	f.filterChain = nil
}

func (f *ConsoleFormatter) GetTemplate(name string, templateValue string) (*template.Template, error) {
	if f.templates == nil {
		f.templates = make(map[string]*template.Template)
	}

	t, ok := f.templates[name]
	if ok {
		return t, nil
	}

	t = template.New(name)
	t.Funcs(template.FuncMap{
		"title":    CTitle,
		"heading":  CInfo,
		"dark":     CDark,
		"info":     CInfo,
		"field":    CSubject,
		"subject":  CSubject,
		"error":    CError,
		"success":  CSucceeded,
		"location": CLocation,
		"lines": func(s string) []string {
			return strings.Split(s, "\n")
		},
		"platform": func() string {
			return f.config.PlatformName
		},
		"seconds": func(d time.Duration) string {
			return fmt.Sprintf("%2.2f", time.Duration(d).Seconds())
		},
		"branch_succeeded": func(v stonesthrow.GitBranchTaskEvent_Result) bool {
			return v == stonesthrow.GitBranchTaskEvent_SUCCEEDED
		},
		"shorthost": func(h string) string {
			return f.config.Host.HostsConfig.ShortHost(h)
		}})
	_, err := t.Parse(templateValue)
	if err != nil {
		return nil, err
	}
	f.templates[name] = t
	return t, nil
}

func (f *ConsoleFormatter) Show(name string, templateValue string, o interface{}) {
	t, err := f.GetTemplate(name, templateValue)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = t.Execute(os.Stdout, o)
	if err != nil {
		fmt.Println(err)
	}
}

func (f *ConsoleFormatter) OnGitRepositoryInfo(ri *stonesthrow.GitRepositoryInfo) error {
	f.Show("repo-info", `{{/*

  Branch info:
  */}}{{define "branch-info"  }}
{{heading .Name}}	{{dark "@"}}{{.Revision}} {{/* 

  Revisions:
    */}}{{if .RevisionsAhead}}{{success "+"}}{{.RevisionsAhead | printf "%d" | success}}{{end}}{{/*
*/}}{{if .RevisionsBehind}}{{error "-"}}{{.RevisionsBehind | printf "%d" | error}}{{end}}{{/*

  Configuration values
    */}}{{range $key, $value := .Config}}
    {{field $key}}:	{{$value}}{{end}}{{end}}{{/*
 
  Upstream info:
  */}}{{define "upstream-info"}}
{{heading .Name}}:
    {{field "Push"}} : {{.PushUrl}}
    {{field "Fetch"}}: {{.FetchUrl}}{{end}}{{/*
 
  Main template:
  */}}{{title "Branches"}}{{range .Branches}}{{template "branch-info" .}}{{end}}
{{title "Upstreams"}}{{range .Upstreams}}{{template "upstream-info" .}}{{end}}
`, ri)
	return nil
}

func (f *ConsoleFormatter) OnTargetList(tl *stonesthrow.TargetList) error {
	return nil
}

func (f *ConsoleFormatter) OnBuilderJobs(bj *stonesthrow.BuilderJobs) error {
	return nil
}

func (f *ConsoleFormatter) OnJobEvent(je *stonesthrow.JobEvent) error {
	switch {
	case je.GetLogEvent() != nil:
		switch je.GetLogEvent().GetSeverity() {
		case stonesthrow.LogEvent_INFO:
			f.Show("info",
				`{{.Host | shorthost | subject}}:{{info "Info"}}: {{.Msg}}
`, je.GetLogEvent())

		case stonesthrow.LogEvent_ERROR:
			f.Show("error",
				`{{.Host | shorthost | subject}}:{{error "Error"}}: {{.Msg}}
`, je.GetLogEvent())

		case stonesthrow.LogEvent_DEBUG:
			f.Show("debug",
				`{{.Host | shorthost | subject}}:{{dark "Debug"}}: {{.Msg}}
`, je.GetLogEvent())

		}

	case je.GetBeginCommandEvent() != nil:
		e := je.GetBeginCommandEvent()
		f.Show("bc",
			`{{.Host | shorthost | subject}}: {{range .Command}}{{.}} {{end}}{{if .Directory}} [{{.Directory | info}}]{{end}}
`, e.GetCommand())
		f.SetupFilterChainForCommand(e.Command.Command)

	case je.GetCommandOutputEvent() != nil:
		e := je.GetCommandOutputEvent()
		fmt.Println(f.ApplyFilters(e.GetOutput()))

	case je.GetEndCommandEvent() != nil:
		e := je.GetEndCommandEvent()
		if e.ReturnCode != 0 {
			f.Show("fail", `{{error "Failed"}}: Return code {{.ReturnCode | printf "%d" | info}}

`, e)
		}
		f.ClearFilters()

	case je.GetBranchTaskEvent() != nil:
		e := je.GetBranchTaskEvent()
		f.Show("branch-task", `[{{title .Branch}}] {{if .Result | branch_succeed}}{{success "OK}}{{else}}{{error "FAILED"}}{{end}} {{info .Revision}} ({{.Reason}})
`, e)
	}
	return nil
}

func (f *ConsoleFormatter) OnPong(pr *stonesthrow.PingResult) error {
	f.Show("pong", `{{info "Pong"}}: {{.Pong}}
`, pr)
	return nil
}

func (f *ConsoleFormatter) Drain(jr stonesthrow.JobEventReceiver) error {
	for {
		je, err := jr.Recv()
		if err != nil {
			return err
		}

		f.OnJobEvent(je)
	}
}

func (f *ConsoleFormatter) DrainReader(o stonesthrow.CommandOutputEvent, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	je := stonesthrow.JobEvent{CommandOutputEvent: &o}
	for scanner.Scan() {
		o.Output = scanner.Text()
		f.OnJobEvent(&je)
	}
	return nil
}

func (f *ConsoleFormatter) Send(je *stonesthrow.JobEvent) error {
	return f.OnJobEvent(je)
}

func WriteTestString() {
	fmt.Println(CError("Error") +
		CInfo("Info") +
		CDark("Dark") +
		CTitle("Title") +
		CSubject("Subject") +
		CLocation("Location") +
		CSourceBuildStep("CSourceBuildStep") +
		CAuxBuildStep("CAuxBuildStep"))
}
