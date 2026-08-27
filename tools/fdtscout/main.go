// FDT.Scout is a small HTTPS admin console for a CloudKey converted by CloudKey Wizard --
// password-gated user login, a real interactive web terminal (PTY over WebSocket), TLS
// certificate management, hostname/front-panel-text control, and basic health metrics over time.
//
// App-authored (not sourced from jnovack/cloudkey or any upstream project), built specifically
// for this Windows app's "Optional Extras" step. Installed by CloudKeyWizard's "fdtscout" Extra,
// which uploads this pre-cross-compiled linux/arm64 binary and a systemd unit -- see
// Services/ExtraCatalog.cs in the main CloudKeyWizard project for the install orchestration.
//
// Deliberately root-level and network-exposed by design (the whole point is a full admin console
// reachable without opening a fresh SSH session), which is a materially different risk profile
// than everything else this app does over an SSH-key/password-gated connection -- gated here by
// its own login instead. Flagged explicitly in the Extra's description and the printed post-
// install summary; not something to install and forget.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed static
var staticFiles embed.FS

func main() {
	bootstrapAdmin := flag.Bool("bootstrap-admin", false, "create/replace the single admin account from ADMIN_USERNAME/ADMIN_PASSWORD env vars, then exit")
	flag.Parse()

	if err := ensureDirs(); err != nil {
		log.Fatalf("couldn't create data/config directories: %v", err)
	}

	users, err := LoadUserStore()
	if err != nil {
		log.Fatalf("couldn't load user store: %v", err)
	}

	if *bootstrapAdmin {
		runBootstrapAdmin(users)
		return
	}

	runServer(users)
}

// runBootstrapAdmin is invoked once by the install orchestrator (not by the systemd service) to
// create the first admin account non-interactively, the same "no interactive stdin choreography"
// approach the Windows app's own SetPasswordCommand uses for chpasswd. Idempotent: re-running it
// (e.g. re-running the Extra) just resets that account's password rather than erroring on "already
// exists," consistent with every other bundled script's re-run behavior in this project.
func runBootstrapAdmin(users *UserStore) {
	username := os.Getenv("ADMIN_USERNAME")
	password := os.Getenv("ADMIN_PASSWORD")
	if username == "" || password == "" {
		fmt.Fprintln(os.Stderr, "ADMIN_USERNAME and ADMIN_PASSWORD must both be set")
		os.Exit(1)
	}
	if len(password) < 8 {
		fmt.Fprintln(os.Stderr, "ADMIN_PASSWORD must be at least 8 characters")
		os.Exit(1)
	}
	if err := users.AddOrReplace(username, password); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create admin account:", err)
		os.Exit(1)
	}
	fmt.Printf("admin account '%s' ready\n", username)
}

func runServer(users *UserStore) {
	runtimeConfig := LoadRuntimeConfig()

	sessions := NewSessionStore()
	stopSessionCleanup := make(chan struct{})
	go sessions.RunCleanup(stopSessionCleanup)

	authLog := NewAuthLog()
	netConfig := NewNetConfigManager()

	certs := NewCertManager()
	if err := certs.LoadOrGenerate(); err != nil {
		log.Fatalf("couldn't load or generate TLS certificate: %v", err)
	}

	if users.Count() == 0 {
		log.Println("WARNING: no accounts exist yet -- run with -bootstrap-admin (ADMIN_USERNAME/ADMIN_PASSWORD env vars set) before anyone can log in")
	}

	metrics := NewMetricsCollector()
	stopMetrics := make(chan struct{})
	go metrics.Run(stopMetrics)
	startLCDDisplay(metrics)

	// Monitoring tab (ping/TCP/HTTP/DNS watch list) + WAN speed test + public IP tracking, all
	// background loops that no-op cheaply when the user hasn't configured anything yet (an empty
	// monitor list, a public-IP check that just runs and logs a baseline).
	monitorEngine := NewMonitorEngine()
	stopMonitors := make(chan struct{})
	go monitorEngine.Run(stopMonitors)

	wanSpeed := NewWANSpeedHistory()
	stopWANSpeed := make(chan struct{})
	go wanSpeed.Run(stopWANSpeed, 6*time.Hour)

	publicIP := NewPublicIPTracker()
	stopPublicIP := make(chan struct{})
	go publicIP.Run(stopPublicIP)

	// Log aggregation onto /volume (or DataDir if not mounted) -- the "big drive" idea.
	stopLogs := make(chan struct{})
	go runLogAggregationLoop(stopLogs)

	// Pushbullet: proactive alerts (notify.go, called from metrics/authlog/publicip above) plus
	// the inbound two-way command channel -- both entirely inert until a token+callsign are
	// actually configured in Settings.
	stopPushbullet := make(chan struct{})
	go runPushbulletListener(stopPushbullet, metrics)
	go runServiceWatch(stopPushbullet)
	go runDigestTimer(stopPushbullet, metrics)

	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("embedded static assets missing: %v", err)
	}

	mux := http.NewServeMux()

	// Public.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	mux.HandleFunc("GET /login", serveEmbedded(staticSub, "login.html"))
	mux.HandleFunc("POST /api/login", handleLogin(users, sessions, authLog))
	mux.HandleFunc("POST /api/logout", handleLogout(sessions))

	// Authenticated page.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookieName); err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		} else if _, ok := sessions.Validate(cookie.Value); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		serveEmbedded(staticSub, "index.html")(w, r)
	})

	// Authenticated API.
	mux.HandleFunc("GET /api/whoami", requireAuth(sessions, true, handleWhoami))
	mux.HandleFunc("GET /api/users", requireAuth(sessions, true, handleListUsers(users, authLog)))
	mux.HandleFunc("POST /api/users", requireAuth(sessions, true, handleAddUser(users)))
	mux.HandleFunc("DELETE /api/users/{username}", requireAuth(sessions, true, handleRemoveUser(users)))
	mux.HandleFunc("GET /api/auth-log", requireAuth(sessions, true, handleAuthLog(authLog)))
	mux.HandleFunc("GET /api/cert/status", requireAuth(sessions, true, handleCertStatus(certs)))
	mux.HandleFunc("POST /api/cert/generate", requireAuth(sessions, true, handleCertGenerate(certs)))
	mux.HandleFunc("POST /api/cert/upload", requireAuth(sessions, true, handleCertUpload(certs)))
	mux.HandleFunc("GET /api/lcd", requireAuth(sessions, true, handleLCDStatus))
	mux.HandleFunc("POST /api/lcd", requireAuth(sessions, true, handleLCDSet))
	mux.HandleFunc("POST /api/lcd/apparmor-fix", requireAuth(sessions, true, handleLCDApparmorFix))
	mux.HandleFunc("GET /api/metrics", requireAuth(sessions, true, handleMetrics(metrics)))
	mux.HandleFunc("GET /api/sysinfo", requireAuth(sessions, true, handleSysinfo))
	mux.HandleFunc("GET /api/processes", requireAuth(sessions, true, handleProcesses))
	mux.HandleFunc("POST /api/processes/{pid}/kill", requireAuth(sessions, true, handleProcessKill))
	mux.HandleFunc("GET /api/apps", requireAuth(sessions, true, handleAppsList))
	mux.HandleFunc("POST /api/apps/{id}/action", requireAuth(sessions, true, handleAppAction))
	mux.HandleFunc("GET /api/install", requireAuth(sessions, true, handleInstallList))
	mux.HandleFunc("POST /api/install/{id}", requireAuth(sessions, true, handleInstallRun))
	mux.HandleFunc("GET /api/about", requireAuth(sessions, true, handleAbout))
	mux.HandleFunc("GET /ws/terminal", requireAuth(sessions, true, handleTerminalWS))
	mux.HandleFunc("GET /api/config", requireAuth(sessions, true, handleConfigStatus))
	mux.HandleFunc("POST /api/config", requireAuth(sessions, true, handleConfigUpdate))
	mux.HandleFunc("GET /api/network", requireAuth(sessions, true, handleNetworkStatus(netConfig)))
	mux.HandleFunc("POST /api/network/apply", requireAuth(sessions, true, handleNetworkApply(netConfig)))
	mux.HandleFunc("POST /api/network/confirm", requireAuth(sessions, true, handleNetworkConfirm(netConfig)))
	mux.HandleFunc("GET /api/ntp", requireAuth(sessions, true, handleNTPStatus))
	mux.HandleFunc("POST /api/ntp", requireAuth(sessions, true, handleNTPUpdate))
	mux.HandleFunc("GET /api/dns", requireAuth(sessions, true, handleDNSStatus))
	mux.HandleFunc("POST /api/dns", requireAuth(sessions, true, handleDNSUpdate))
	mux.HandleFunc("GET /api/timezone", requireAuth(sessions, true, handleTimezoneStatus))
	mux.HandleFunc("POST /api/timezone", requireAuth(sessions, true, handleTimezoneUpdate))
	mux.HandleFunc("GET /api/display", requireAuth(sessions, true, handleDisplayStatus))
	mux.HandleFunc("POST /api/display", requireAuth(sessions, true, handleDisplayUpdate))
	mux.HandleFunc("GET /api/storage/usb", requireAuth(sessions, true, handleUSBList))
	mux.HandleFunc("POST /api/storage/usb/{device}/{action}", requireAuth(sessions, true, handleUSBMountAction))
	mux.HandleFunc("GET /api/files", requireAuth(sessions, true, handleFilesList))
	mux.HandleFunc("GET /api/files/download", requireAuth(sessions, true, handleFileDownload))

	// Monitoring tab.
	mux.HandleFunc("GET /api/monitors", requireAuth(sessions, true, handleMonitorsList(monitorEngine)))
	mux.HandleFunc("POST /api/monitors", requireAuth(sessions, true, handleMonitorsSave(monitorEngine)))
	mux.HandleFunc("GET /api/monitors/{id}/history", requireAuth(sessions, true, handleMonitorHistory(monitorEngine)))
	mux.HandleFunc("POST /api/monitors/{id}/run", requireAuth(sessions, true, handleMonitorRunNow(monitorEngine)))
	mux.HandleFunc("GET /api/wanspeed", requireAuth(sessions, true, handleWANSpeedHistory(wanSpeed)))
	mux.HandleFunc("POST /api/wanspeed/run", requireAuth(sessions, true, handleWANSpeedRunNow(wanSpeed)))
	mux.HandleFunc("GET /api/publicip", requireAuth(sessions, true, handlePublicIPHistory(publicIP)))
	mux.HandleFunc("GET /api/ddns", requireAuth(sessions, true, handleDDNSStatus))
	mux.HandleFunc("POST /api/ddns", requireAuth(sessions, true, handleDDNSUpdate))

	// Active scouting: IP range scan + port scan, both user-triggered only, never scheduled.
	mux.HandleFunc("GET /api/scan/subnet", requireAuth(sessions, true, handleScanSubnet))
	mux.HandleFunc("POST /api/scan/ports", requireAuth(sessions, true, handleScanPorts))
	mux.HandleFunc("GET /api/lan", requireAuth(sessions, true, handleLANList))
	mux.HandleFunc("POST /api/wol", requireAuth(sessions, true, handleWakeOnLAN))

	// Logs (journalctl aggregated onto real storage) + scheduled tasks (real crontab, tagged).
	mux.HandleFunc("GET /api/logs", requireAuth(sessions, true, handleLogsList))
	mux.HandleFunc("GET /api/logs/read", requireAuth(sessions, true, handleLogsRead))
	mux.HandleFunc("GET /api/tasks", requireAuth(sessions, true, handleTasksList))
	mux.HandleFunc("POST /api/tasks", requireAuth(sessions, true, handleTasksSave))

	// Pushbullet.
	mux.HandleFunc("GET /api/pushbullet", requireAuth(sessions, true, handlePushbulletStatus))
	mux.HandleFunc("POST /api/pushbullet", requireAuth(sessions, true, handlePushbulletUpdate))

	// Docker tab: install/verify, container lifecycle, run-from-image, logs, storage location.
	mux.HandleFunc("GET /api/docker", requireAuth(sessions, true, handleDockerStatus))
	mux.HandleFunc("POST /api/docker/install", requireAuth(sessions, true, handleDockerInstall))
	mux.HandleFunc("GET /api/docker/containers", requireAuth(sessions, true, handleDockerContainersList))
	mux.HandleFunc("POST /api/docker/containers/{id}/{action}", requireAuth(sessions, true, handleDockerContainerAction))
	mux.HandleFunc("GET /api/docker/containers/{id}/logs", requireAuth(sessions, true, handleDockerContainerLogs))
	mux.HandleFunc("POST /api/docker/run", requireAuth(sessions, true, handleDockerRun))
	mux.HandleFunc("POST /api/docker/storage", requireAuth(sessions, true, handleDockerStorageSet))

	server := &http.Server{
		Addr:      fmt.Sprintf(":%d", runtimeConfig.Port),
		Handler:   mux,
		TLSConfig: &tls.Config{GetCertificate: certs.GetCertificate},
	}

	go func() {
		log.Printf("FDT.Scout %s listening on :%d (HTTPS)", Version, runtimeConfig.Port)
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	var redirectServer *http.Server
	if runtimeConfig.RedirectHTTP {
		redirectServer = startRedirectServer(runtimeConfig.Port)
	}
	_ = redirectServer // stopped implicitly at process exit; a config change restarts the whole service anyway

	// Graceful shutdown on SIGTERM (normal `systemctl stop`) so the metrics collector gets to
	// flush its most recent samples to disk instead of losing up to persistEvery of history.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	close(stopMetrics)
	close(stopSessionCleanup)
	close(stopMonitors)
	close(stopWANSpeed)
	close(stopPublicIP)
	close(stopLogs)
	close(stopPushbullet)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

// startRedirectServer binds :80 and answers every request with a redirect to the same host on the
// real HTTPS port -- "Redirect port 80 to 443" from the Certificates tab, targeting whatever port
// is actually configured rather than a hardcoded 443, so this still works after a custom port
// change. Best-effort: if :80 is already taken (unlikely on this device, but not impossible) this
// logs and moves on rather than crashing the whole service over a convenience feature.
func startRedirectServer(httpsPort int) *http.Server {
	redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if colon := strings.LastIndex(host, ":"); colon != -1 {
			host = host[:colon]
		}
		target := fmt.Sprintf("https://%s:%d%s", host, httpsPort, r.URL.RequestURI())
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
	srv := &http.Server{Addr: ":80", Handler: redirectHandler}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("port 80 redirect listener failed (continuing without it): %v", err)
		}
	}()
	return srv
}

func serveEmbedded(fsys fs.FS, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}
}
