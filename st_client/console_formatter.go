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

	re2 := regexp.MustCompile(`[^\w/\-]\.\.\\\.\.\\[\w/\\\-]*`)
	f.AddFilter(func(s string) string {
		return re2.ReplaceAllStringFunc(s, relPathRewriter)
	})

	re4 := regexp.MustCompile(`[^\w/\-]\.\./\.\./([\w/\-]*)`)
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
		` (STAMP|ACTION|AR|LINK|SOLINK|RC|LIB|LIBTOOL|LIBTOOL-STATIC) `,
		CAuxBuildStep(" $1 "))
	f.DimProgress()
}

func (f *ConsoleFormatter) SetupFilterChainForCommand(command []string) {
	f.filterChain = nil
	switch {
	case len(command) == 0:
		return

	case strings.Contains(command[0], "ninja"):
		f.AddNinjaFilters()
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

func (f *ConsoleFormatter) Format(message interface{}) error {
	switch t := message.(type) {
	case *stonesthrow.InfoMessage:
		f.Show("info",
			`{{info "Info"}}: {{.Info}}
`, t)

	case *stonesthrow.ErrorMessage:
		f.Show("error",
			`{{error "Error"}}: {{.Error}}
`, t)

	case *stonesthrow.BeginCommandMessage:
		f.Show("bc",
			`{{platform | subject}}: {{range .Command}}{{.}} {{end}}{{if .WorkDir}} [{{.WorkDir | info}}]{{end}}
`, t)
		f.SetupFilterChainForCommand(t.Command)

	case *stonesthrow.TerminalOutputMessage:
		fmt.Println(f.ApplyFilters(t.Output))

	case *stonesthrow.EndCommandMessage:
		if t.ReturnCode != 0 {
			f.Show("fail", `{{error "Failed"}}: Return code {{.ReturnCode | printf "%d" | info}}

`, t)
		}

	case *stonesthrow.CommandListMessage:
		f.Show("help", `{{if .Synposis}}{{.Synposis}}
{{end}}{{if .ConfigFile}}
  {{heading "Configuration"}}: {{.ConfigFile}}
{{end}}
{{if .Repositories}}  Repositories:{{range $repo, $r := .Repositories}}
    {{title $repo}}:
      {{heading "Path"}}    : {{$r.SourcePath}}
      {{heading "Revision"}}: {{$r.Revistion}}
      {{heading "Output"}}  : {{$r.BuildPath}}{{end}}

{{end}}
{{if .Commands}}  Commands:{{range $cmd, $c := .Commands}}
    {{title $cmd}}: {{if $c.Aliases}}[{{range $c.Aliases}}{{info .}}{{end}}]{{if $c.Doc}}
      {{$c.Doc}}{{end}}{{else}}{{$c.Doc}}{{end}}{{end}}
{{end}}`, t)

	case *stonesthrow.JobListMessage:
		f.Show("jl",
			`Running Jobs:{{range .Jobs}}
  Command: {{title .Request.Command}} {{range .Request.Arguments}}{{.}} {{end}} #{{.Id | printf "%d" | info}}
    On {{.Request.Repository}}@{{location .Request.Revision}}
    Running Since {{.StartTime}} ({{seconds .Duration | info}} seconds){{template "ps" .Processes}}{{else}}No running jobs.
{{end}}
{{define "ps"}}{{if .}}
    Child Processes:{{range .}}
      Command: {{range .Command}}{{.}} {{end}}{{if .Running}}
        Running since {{.StartTime}} ({{seconds .Duration | info}} seconds)
{{else}}
        Ran from {{.StartTime}} to {{.EndTime}} ({{seconds .Duration | info}} seconds)
	System time {{seconds .SystemTime | info}} seconds
	User time {{seconds .UserTime | info}} seconds
{{end}}{{end}}
{{else}}
    No Child Processes.
{{end}}{{end}}`, t)

	case *stonesthrow.ProcessListMessage:
		f.Show("ps", `Running Child Processes:{{range .Processes}}
  Command: {{range .Command}}{{.}} {{end}}
    Running since {{.StartTime}} ({{seconds .Duration | info}} seconds)
{{else}}
No running processes.
{{end}}
`, t)

	default:
		fmt.Printf("Unrecognized message %#v", message)
	}

	return nil
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
