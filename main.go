// Channel Surfer test build.
// Serves the embedded guide on a private localhost port and opens it in an
// app-style window (Edge or Chrome --app). Quits on its own a few seconds
// after the window is closed.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
)

//go:embed web
var webFS embed.FS

var lastPing atomic.Int64

var logPath = filepath.Join(os.TempDir(), "ChannelSurfer.log")

func logf(format string, a ...any) {
	line := time.Now().Format("2006-01-02 15:04:05 ") + fmt.Sprintf(format, a...) + "\n"
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	log.Print(line)
}

func main() {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	// Fixed port keeps the page's origin stable, so saved favorites and
	// account status survive restarts. Falls back to any free port.
	ln, err := net.Listen("tcp", "127.0.0.1:47815")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatal(err)
		}
	}
	url := "http://" + ln.Addr().String() + "/"
	logf("Channel Surfer running at %s", url)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		lastPing.Store(time.Now().Unix())
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/guide", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if os.Getenv("CS_FAKE_GUIDE") != "" { // test-only sample data
			http.ServeFile(w, r, os.Getenv("CS_FAKE_GUIDE"))
			return
		}
		g, err := getGuide(r.URL.Query().Get("refresh") == "1")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "log": logPath})
			return
		}
		_ = json.NewEncoder(w).Encode(g)
	})
	mux.HandleFunc("/api/yttv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if os.Getenv("CS_FAKE_YTTV") != "" { // test-only sample data
			http.ServeFile(w, r, os.Getenv("CS_FAKE_YTTV"))
			return
		}
		g, err := getYTTV(r.URL.Query().Get("refresh") == "1")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "log": logPath})
			return
		}
		_ = json.NewEncoder(w).Encode(g)
	})
	go func() { log.Fatal(http.Serve(ln, mux)) }()

	openWindow(url)

	// Exit once the page stops pinging (window closed). Give it 60s to first load.
	start := time.Now()
	for range time.Tick(2 * time.Second) {
		lp := lastPing.Load()
		if lp == 0 {
			if time.Since(start) > 60*time.Second {
				os.Exit(0)
			}
			continue
		}
		if time.Since(time.Unix(lp, 0)) > 8*time.Second {
			os.Exit(0)
		}
	}
}

func openWindow(url string) {
	// Persistent private profile so streamer sign-ins survive restarts.
	base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		base = os.TempDir()
	}
	profile := filepath.Join(base, "ChannelSurfer", "Profile")
	_ = os.MkdirAll(profile, 0o700)
	appArgs := []string{"--app=" + url, "--window-size=1280,760", "--user-data-dir=" + profile, "--no-first-run"}

	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		local := os.Getenv("LOCALAPPDATA")
		candidates := []string{
			filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				if exec.Command(c, appArgs...).Start() == nil {
					return
				}
			}
		}
		// Fallback: default browser
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return
	}

	for _, c := range []string{"microsoft-edge", "google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(c); err == nil {
			if exec.Command(p, appArgs...).Start() == nil {
				return
			}
		}
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	_ = exec.Command(opener, url).Start()
}
