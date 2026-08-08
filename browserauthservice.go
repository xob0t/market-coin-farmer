package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// BrowserAuthService drives a real Chromium browser so the user can log into
// Yandex interactively, then harvests the resulting cookies (including the
// HttpOnly auth cookies that page scripts cannot read) via the DevTools
// protocol and returns them in Netscape format.
type BrowserAuthService struct {
	mu     sync.Mutex
	active map[int]context.CancelFunc
	nextID int
}

// register records a login's cancel func so it can be torn down on app
// shutdown; unregister removes it once the login finishes.
func (s *BrowserAuthService) register(cancel context.CancelFunc) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = make(map[int]context.CancelFunc)
	}
	id := s.nextID
	s.nextID++
	s.active[id] = cancel
	return id
}

func (s *BrowserAuthService) unregister(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, id)
}

// ServiceShutdown is called by Wails when the app exits. It cancels any
// in-progress browser logins so their browser processes are torn down instead
// of being orphaned.
func (s *BrowserAuthService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.active {
		cancel()
		delete(s.active, id)
	}
	return nil
}

const (
	// Yandex sets the session cookie on the passport domain after a successful
	// login; its presence is our signal that the user is authenticated.
	yandexSessionCookie = "Session_id"
	yandexLoginURL      = "https://passport.yandex.ru/auth?retpath=https%3A%2F%2Fmarket.yandex.ru%2Fkolesoprizov"

	loginTimeout = 5 * time.Minute
	loginPoll    = 1500 * time.Millisecond
)

// LoginResult is returned to the frontend after a browser login attempt.
type LoginResult struct {
	Cookies string `json:"cookies"`
	Login   string `json:"login"`
}

// Available reports whether a Chromium-based browser (Edge or Chrome) could be
// found, so the frontend can offer browser login only when it will work and
// fall back to manual cookie import otherwise.
func (s *BrowserAuthService) Available() bool {
	_, err := findBrowser()
	return err == nil
}

// Login opens a browser window pointed at the Yandex login page and waits until
// the user authenticates, then returns the captured cookies. When proxy is a
// non-empty proxy URL, browser traffic is routed through a local relay so that
// authenticated proxies work despite Chromium's inability to pass credentials
// on the command line.
//
// The ctx is supplied by Wails and is cancelled when the frontend cancels the
// call's CancellablePromise; cancelling tears down the browser process.
func (s *BrowserAuthService) Login(ctx context.Context, proxy string) (LoginResult, error) {
	browserPath, err := findBrowser()
	if err != nil {
		return LoginResult{}, err
	}

	// A cancellable child of the Wails call context so ServiceShutdown can tear
	// the browser down on app exit, in addition to the frontend's own cancel.
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	id := s.register(runCancel)
	defer s.unregister(id)

	profileDir, err := os.MkdirTemp("", "mcf-login-*")
	if err != nil {
		return LoginResult{}, fmt.Errorf("failed to create browser profile: %w", err)
	}
	defer os.RemoveAll(profileDir)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(520, 720),
	)

	proxy = strings.TrimSpace(proxy)
	if proxy != "" {
		relay, relayErr := startProxyRelay(proxy)
		if relayErr != nil {
			return LoginResult{}, relayErr
		}
		defer relay.close()
		opts = append(opts, chromedp.ProxyServer("http://"+relay.addr()))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(runCtx, opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	timeoutCtx, cancelTimeout := context.WithTimeout(browserCtx, loginTimeout)
	defer cancelTimeout()

	if err := chromedp.Run(timeoutCtx, chromedp.Navigate(yandexLoginURL)); err != nil {
		return LoginResult{}, classifyBrowserError(err)
	}

	cookies, err := waitForLogin(timeoutCtx)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		Cookies: cookiesToNetscape(cookies),
		Login:   extractLogin(cookies),
	}, nil
}

// waitForLogin polls the browser's cookie store until the Yandex session cookie
// appears, then returns the full cookie set.
func waitForLogin(ctx context.Context) ([]*network.Cookie, error) {
	ticker := time.NewTicker(loginPoll)
	defer ticker.Stop()

	for {
		var cookies []*network.Cookie
		err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var innerErr error
			cookies, innerErr = storage.GetCookies().Do(ctx)
			return innerErr
		}))
		if err != nil {
			return nil, classifyBrowserError(err)
		}
		if hasSessionCookie(cookies) {
			return cookies, nil
		}

		select {
		case <-ctx.Done():
			return nil, classifyBrowserError(ctx.Err())
		case <-ticker.C:
		}
	}
}

func hasSessionCookie(cookies []*network.Cookie) bool {
	for _, cookie := range cookies {
		if cookie.Name == yandexSessionCookie && strings.Contains(cookie.Domain, "yandex") && cookie.Value != "" {
			return true
		}
	}
	return false
}

// classifyBrowserError turns internal chromedp/context errors into a message
// the user can act on. A closed window or elapsed deadline both mean "no login
// happened" rather than a crash.
func classifyBrowserError(err error) error {
	if err == nil {
		return nil
	}
	if err == context.DeadlineExceeded {
		return fmt.Errorf("время ожидания входа истекло")
	}
	if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
		return fmt.Errorf("окно браузера было закрыто до завершения входа")
	}
	return err
}

// cookiesToNetscape renders CDP cookies as a Netscape cookies.txt document,
// matching the format the manual-import path already accepts.
func cookiesToNetscape(cookies []*network.Cookie) string {
	sorted := append([]*network.Cookie(nil), cookies...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Domain != sorted[j].Domain {
			return sorted[i].Domain < sorted[j].Domain
		}
		return sorted[i].Name < sorted[j].Name
	})

	// Persist session cookies far into the future so the stored jar keeps them.
	fallbackExpiry := time.Now().Add(365 * 24 * time.Hour).Unix()

	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n")
	for _, cookie := range sorted {
		if cookie.Name == "" {
			continue
		}
		includeSubdomains := "FALSE"
		if strings.HasPrefix(cookie.Domain, ".") {
			includeSubdomains = "TRUE"
		}
		secure := "FALSE"
		if cookie.Secure {
			secure = "TRUE"
		}
		expiry := int64(cookie.Expires)
		if expiry <= 0 {
			expiry = fallbackExpiry
		}
		path := cookie.Path
		if path == "" {
			path = "/"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			cookie.Domain, includeSubdomains, path, secure,
			strconv.FormatInt(expiry, 10), cookie.Name, cookie.Value)
	}
	return b.String()
}

// extractLogin reads the human-readable Yandex login from cookies when present,
// used to pre-name the account. Empty is fine — it is filled in on refresh.
func extractLogin(cookies []*network.Cookie) string {
	for _, cookie := range cookies {
		if cookie.Name == "yandex_login" && cookie.Value != "" {
			return cookie.Value
		}
	}
	return ""
}

// findBrowser locates a Chromium-based browser, preferring Edge (present on
// every Windows install) and falling back to Chrome.
func findBrowser() (string, error) {
	for _, candidate := range browserCandidates() {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("не найден браузер Edge или Chrome — установите один из них для входа")
}

func browserCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		var dirs []string
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
			if v := os.Getenv(env); v != "" {
				dirs = append(dirs, v)
			}
		}
		var candidates []string
		for _, dir := range dirs {
			candidates = append(candidates,
				filepath.Join(dir, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"),
			)
		}
		return candidates
	case "darwin":
		return []string{
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		return []string{
			lookupPath("microsoft-edge"),
			lookupPath("microsoft-edge-stable"),
			lookupPath("google-chrome"),
			lookupPath("google-chrome-stable"),
			lookupPath("chromium"),
			lookupPath("chromium-browser"),
		}
	}
}

func lookupPath(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return ""
}
