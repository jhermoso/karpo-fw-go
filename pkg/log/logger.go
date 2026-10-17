// Package log ofrece los contratos de log estructurado de los servicios de Karpo.
package log

import (
	"context"
)

// Level define la gravedad del log.
type Level int

const (
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
)

// Logger es el contrato de dominio para el log estructurado.
// Las implementaciones pueden envolver slog, zap, un equivalente de log4net o una captura en memoria para pruebas.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
	WithContext(ctx context.Context) Logger
}
