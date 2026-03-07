package logger

import "context"

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Attr struct {
	Key   string
	Value any
}

func String(key, val string) Attr          { return Attr{Key: key, Value: val} }
func Int(key string, val int) Attr         { return Attr{Key: key, Value: val} }
func Int64(key string, val int64) Attr     { return Attr{Key: key, Value: val} }
func Float64(key string, val float64) Attr { return Attr{Key: key, Value: val} }
func Bool(key string, val bool) Attr       { return Attr{Key: key, Value: val} }
func Any(key string, val any) Attr         { return Attr{Key: key, Value: val} }
func Err(err error) Attr                   { return Attr{Key: "error", Value: err} }

type Logger interface {
	Debug(msg string, attrs ...Attr)
	Info(msg string, attrs ...Attr)
	Warn(msg string, attrs ...Attr)
	Error(msg string, attrs ...Attr)

	DebugContext(ctx context.Context, msg string, attrs ...Attr)
	InfoContext(ctx context.Context, msg string, attrs ...Attr)
	WarnContext(ctx context.Context, msg string, attrs ...Attr)
	ErrorContext(ctx context.Context, msg string, attrs ...Attr)
}
