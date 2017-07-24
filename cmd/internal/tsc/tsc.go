package tsc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode"
	"unicode/utf8"
)

func nonil(err ...error) error {
	for _, err := range err {
		if err != nil {
			return err
		}
	}
	return nil
}

type Event struct {
	Name    string      // https://developer.github.com/webhooks/#events
	Payload interface{} // https://developer.github.com/v3/activity/events/types/
	Args    map[string]string
}

type Script struct {
	// ErrorLog specifies an optional logger for errors serving requests.
	// If nil, logging goes to os.Stderr via the log package's standard logger.
	ErrorLog *log.Logger

	OutputFunc func() io.Writer

	bash bool
	tmpl *template.Template
	args map[string]string
}

func New(file string, args []string) (*Script, error) {
	if len(args)&1 == 1 {
		return nil, errors.New("number of arguments for template script must be even")
	}
	s := &Script{}
	if len(args) != 0 {
		s.args = make(map[string]string, len(args)/2)
		for i := 0; i < len(args); i += 2 {
			if len(args[i]) < 2 || args[i][0] != '-' {
				return nil, errors.New("invalid flag name: " + args[i])
			}
			r, n := utf8.DecodeRuneInString(args[i][1:])
			if r == utf8.RuneError {
				return nil, errors.New("invalid flag name: " + args[i])
			}
			s.args[string(unicode.ToUpper(r))+args[i][1+n:]] = args[i+1]
		}
	}
	s.bash = strings.HasSuffix(file, ".sh") || strings.HasSuffix(file, ".bash")
	var err error
	s.tmpl, err = template.New(filepath.Base(file)).Funcs(s.funcs()).ParseFiles(file)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Script) Webhook(event string, payload interface{}) {
	e := &Event{
		Name:    event,
		Payload: payload,
		Args:    s.args,
	}
	var err error
	if s.bash {
		err = s.runBash(e)
	} else {
		err = s.execute(s.output(), e)
	}
	if err != nil {
		s.logf("ERROR template script error: %v", err)
	}
}

func (s *Script) runBash(e *Event) (err error) {
	var buf bytes.Buffer
	if err = s.execute(&buf, e); err != nil {
		return err
	}

	cmd := exec.Command("bash")
	cmd.Stdin = bytes.NewReader(buf.Bytes())
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	cmd.Env = append(os.Environ(), "WEBHOOK=1") // cache?

	if err = cmd.Run(); err != nil {
		// dump script for later troubleshooting
		f, e := ioutil.TempFile("webhook", "debug")
		if e == nil {
			_, e = io.Copy(f, bytes.NewReader(buf.Bytes()))
			e = nonil(e, f.Close())
		}
		if e != nil {
			return fmt.Errorf("%s (failed to dump script file: %s)", err, e)
		}
		return fmt.Errorf("%s: failed with: %s", f.Name(), err)
	}

	return nil
}

func (s *Script) execute(w io.Writer, e *Event) error {
	err := s.tmpl.Execute(w, e)
	if c, ok := w.(io.Closer); ok {
		err = nonil(err, c.Close())
	}
	return err
}

func (s *Script) funcs() template.FuncMap {
	return template.FuncMap{
		"env": func(s string) string {
			return os.Getenv(s)
		},
		"exec": func(cmd string, args ...string) (string, error) {
			out, err := exec.Command(cmd, args...).Output()
			return string(bytes.TrimSpace(out)), err
		},
		"sleep": func(s string) (string, error) {
			d, err := time.ParseDuration(s)
			if err != nil {
				return "", err
			}
			time.Sleep(d)
			return "", nil
		},
		"log": func(v ...interface{}) string {
			if len(v) != 0 {
				s.log(v...)
			}
			return ""
		},
		"logf": func(format string, v ...interface{}) string {
			if format == "" {
				return ""
			}
			if len(v) == 0 {
				s.logf("%s", format)
			} else {
				s.logf(format, v...)
			}
			return ""
		},
	}
}

func (s *Script) output() io.Writer {
	if s.OutputFunc != nil {
		return s.OutputFunc()
	} else {
		return ioutil.Discard
	}
}

func (s *Script) logf(format string, v ...interface{}) {
	if s.ErrorLog != nil {
		s.ErrorLog.Printf(format, v...)
	} else {
		log.Printf(format, v...)
	}
}

func (s *Script) log(v ...interface{}) {
	if s.ErrorLog != nil {
		s.ErrorLog.Println(v...)
	} else {
		log.Println(v...)
	}
}
