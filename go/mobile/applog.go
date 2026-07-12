package mobile

import (
	"sync"

	"github.com/howardkim0/car_monitor/go/internal/applog"
)

// appLogger is package-level and independent of any Session deliberately:
// a Session is recreated on every Bluetooth reconnect (see
// ObdForegroundService.openConnection()), but the app log must stay open
// across that churn, only capped by size (DESIGN.md section 6) — not
// reopened/rotated every time the car's Bluetooth link drops.
var (
	appLogMu  sync.Mutex
	appLogger *applog.Logger
)

// InitAppLog opens the app log under dir (the app's private storage
// root). Call once, e.g. from ObdForegroundService.onCreate().
func InitAppLog(dir string) error {
	appLogMu.Lock()
	defer appLogMu.Unlock()

	logger, err := applog.Open(dir)
	if err != nil {
		return err
	}
	appLogger = logger
	return nil
}

// LogError writes a persisted ERROR line. A no-op if InitAppLog hasn't
// been called (or CloseAppLog already has) — logging must never be a
// reason for a caller to have to handle an error itself.
func LogError(message string) {
	logger := currentAppLogger()
	if logger != nil {
		logger.Errorf("%s", message)
	}
}

// LogDebug writes a persisted DEBUG line. See LogError.
func LogDebug(message string) {
	logger := currentAppLogger()
	if logger != nil {
		logger.Debugf("%s", message)
	}
}

func currentAppLogger() *applog.Logger {
	appLogMu.Lock()
	defer appLogMu.Unlock()
	return appLogger
}

// CloseAppLog closes the app log. Call once, e.g. from
// ObdForegroundService.onDestroy().
func CloseAppLog() error {
	appLogMu.Lock()
	defer appLogMu.Unlock()

	if appLogger == nil {
		return nil
	}
	err := appLogger.Close()
	appLogger = nil
	return err
}
