package main

import (
	"fmt"
	"github.com/asankah/stonesthrow"
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

func literalToRegex(s string) string {
	s = strings.Replace(s, "\\", "\\\\", -1)
	s = strings.Replace(s, ".", "\\.", -1)
	s = strings.Replace(s, "+", "\\+", -1)
	return s
}

func (f *ConsoleFormatter) DimProgress() {
	f.AddRegExpReplace(
		`^\[(\d*)/(\d*)\]`,
		"["+CDark("$1")+"/"+CDark("$2")+"]")
}

func (f *ConsoleFormatter) AddPathRemappers() {
	re := regexp.MustCompile(literalToRegex(f.config.Repository.SourcePath) + `[\w/\\_-]*`)
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
		"location": CLocation,
		"lines": func(s string) []string {
			return strings.Split(s, "\n")
		},
		"platform": func() string {
			return f.config.PlatformName
		},
		"seconds": func(d time.Duration) string {
			return fmt.Sprintf("%2.2f", time.Duration(d).Seconds())
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
}

func (f *ConsoleFormatter) OnTargetList(tl *stonesthrow.TargetList) error {}

func (f *ConsoleFormatter) OnBuilderJobs(bj *stonesthrow.BuilderJobs) error {}

func (f *ConsoleFormatter) OnJobEvent(je *stonesthrow.JobEvent) error {
	switch {
	case je.GetLogEvent() != nil:
		switch je.GetLogEvent().GetSeverity() {
		case stonesthrow.LogEvent_INFO:
			f.Show("info",
				`{{info "Info"}}: {{.}}
`, je.GetLogEvent().GetMsg())

		case stonesthrow.LogEvent_ERROR:
			f.Show("error",
				`{{error "Error"}}: {{.}}
`, je.GetLogEvent().GetMsg())

		}

	case je.GetBeginCommandEvent() != nil:
		e := je.GetBeginCommandEvent()
		f.Show("bc",
			`{{.Hostname | subject}}: {{range .Command.Command}}{{.}} {{end}}{{if .Command.Directory}} [{{.Command.Directory | info}}]{{end}}
`, e)
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
	}
}

func (f *ConsoleFormatter) OnPong(pr *stonesthrow.PingResult) error {}

func (f *ConsoleFormatter) Drain(jr stonesthrow.JobEventReceiver) error {}

func (f *ConsoleFormatter) DrainReader(o stonesthrow.CommandOutputEvent, r io.Reader) error {}

func (f *ConsoleFormatter) Send(je *stonesthrow.JobEvent) error {
	return OnJobEvent(je)
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
