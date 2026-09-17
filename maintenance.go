package goserver

import (
	"crypto/subtle"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const maintenanceBypassHeader = "X-Goserver-Maintenance-Token"

type maintenanceMode struct {
	fallback    bool
	file        string
	interval    time.Duration
	bypassToken string

	lastCheck    atomic.Int64
	enabledValue atomic.Bool
	refreshMu    sync.Mutex
}

func newMaintenanceMode(cfg Config) *maintenanceMode {
	path, err := maintenanceFilePath()
	if err != nil {
		path = ""
	}
	return newMaintenanceModeAt(cfg, path)
}

func maintenanceFilePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executable), "maintenance"), nil
}

func newMaintenanceModeAt(cfg Config, path string) *maintenanceMode {
	interval := cfg.MaintenanceCheckInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	m := &maintenanceMode{
		fallback:    cfg.MaintenanceMode,
		file:        path,
		interval:    interval,
		bypassToken: cfg.maintenanceBypassToken,
	}
	m.enabledValue.Store(cfg.MaintenanceMode)
	return m
}

func (m *maintenanceMode) enabled() bool {
	if m.fallback || m.file == "" {
		return m.fallback
	}
	now := time.Now().UnixNano()
	if lastCheck := m.lastCheck.Load(); lastCheck != 0 && now-lastCheck < m.interval.Nanoseconds() {
		return m.enabledValue.Load()
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	now = time.Now().UnixNano()
	if lastCheck := m.lastCheck.Load(); lastCheck != 0 && now-lastCheck < m.interval.Nanoseconds() {
		return m.enabledValue.Load()
	}
	info, err := os.Stat(m.file)
	if err != nil {
		m.enabledValue.Store(m.fallback)
	} else {
		m.enabledValue.Store(!info.IsDir())
	}
	m.lastCheck.Store(now)
	return m.enabledValue.Load()
}

func (m *maintenanceMode) allows(r *http.Request) bool {
	if m.bypassToken == "" || r == nil {
		return false
	}
	presented := r.Header.Get(maintenanceBypassHeader)
	return len(presented) == len(m.bypassToken) && subtle.ConstantTimeCompare([]byte(presented), []byte(m.bypassToken)) == 1
}

const maintenancePage = `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta name="robots" content="noindex, nofollow">
	<title>Updating</title>
	<style>
		:root { color-scheme: dark; --accent: #00dc82; --bg: #07110d; --text: #edf8f2; }
		* { box-sizing: border-box; }
		body { min-height: 100dvh; margin: 0; display: grid; place-items: center; overflow: hidden; background: var(--bg); color: var(--text); font-family: ui-sans-serif, system-ui, sans-serif; text-align: center; }
		main { padding: 2rem; }
		.signal { position: relative; width: 8rem; height: 8rem; margin: 0 auto 1.75rem; border: 2px solid var(--accent); border-radius: 2.25rem; box-shadow: inset 0 0 0 .65rem rgb(0 220 130 / .12), 0 0 2rem rgb(0 220 130 / .2); transform: rotate(18deg); animation: spin 7s linear infinite; }
		.signal::before, .signal::after { position: absolute; top: 50%; left: -14%; width: 128%; height: 1px; background: var(--accent); content: ""; opacity: .65; }
		.signal::before { transform: rotate(-35deg); }
		.signal::after { transform: rotate(55deg); }
		.signal i { position: absolute; inset: 0; margin: auto; width: .75rem; height: .75rem; border-radius: 50%; background: var(--accent); box-shadow: 0 0 1rem var(--accent); animation: pulse 1.5s ease-in-out infinite alternate; }
		h1 { margin: 0 0 .75rem; font-size: clamp(1.5rem, 4vw, 2rem); }
		p { max-width: 34rem; margin: 0; line-height: 1.55; opacity: .75; }
		@keyframes spin { to { transform: rotate(378deg); } }
		@keyframes pulse { to { transform: scale(.55); opacity: .4; } }
		@media (prefers-reduced-motion: reduce) { .signal, .signal i { animation: none; } }
	</style>
</head>
<body><main>
	<div class="signal" aria-hidden="true"><i></i></div>
	<h1>We are making things better</h1>
	<p>The site is being updated and will be back shortly. Thank you for your patience.</p>
</main></body>
</html>`

func serveMaintenance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Retry-After", "300")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, maintenancePage)
	}
}
