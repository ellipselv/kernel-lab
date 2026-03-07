package logger

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

type levelStyle struct {
	label string
	paint func(a ...interface{}) string
}

var styles = map[Level]levelStyle{
	LevelDebug: {"DBG", color.New(color.FgCyan).SprintFunc()},
	LevelInfo:  {"INF", color.New(color.FgGreen).SprintFunc()},
	LevelWarn:  {"WRN", color.New(color.FgYellow).SprintFunc()},
	LevelError: {"ERR", color.New(color.FgRed, color.Bold).SprintFunc()},
}

var dim = color.New(color.FgHiBlack).SprintFunc()

type ColorLogger struct {
	level Level
}

func NewColorLogger(level Level) *ColorLogger {
	return &ColorLogger{level: level}
}

func (l *ColorLogger) Debug(msg string, attrs ...Attr) { l.log(LevelDebug, msg, attrs) }
func (l *ColorLogger) Info(msg string, attrs ...Attr)  { l.log(LevelInfo, msg, attrs) }
func (l *ColorLogger) Warn(msg string, attrs ...Attr)  { l.log(LevelWarn, msg, attrs) }
func (l *ColorLogger) Error(msg string, attrs ...Attr) { l.log(LevelError, msg, attrs) }

func (l *ColorLogger) DebugContext(_ context.Context, msg string, attrs ...Attr) {
	l.Debug(msg, attrs...)
}
func (l *ColorLogger) InfoContext(_ context.Context, msg string, attrs ...Attr) {
	l.Info(msg, attrs...)
}
func (l *ColorLogger) WarnContext(_ context.Context, msg string, attrs ...Attr) {
	l.Warn(msg, attrs...)
}
func (l *ColorLogger) ErrorContext(_ context.Context, msg string, attrs ...Attr) {
	l.Error(msg, attrs...)
}

func (l *ColorLogger) log(lvl Level, msg string, attrs []Attr) {
	if l.level > lvl {
		return
	}

	s := styles[lvl]
	ts := dim(time.Now().Format("15:04:05.000"))

	var sb strings.Builder
	for _, a := range attrs {
		sb.WriteString(" ")
		sb.WriteString(dim(a.Key))
		sb.WriteString("=")
		sb.WriteString(fmt.Sprint(a.Value))
	}

	fmt.Fprintf(os.Stderr, "%s %s %s%s\n", ts, s.paint(s.label), msg, sb.String())
}
