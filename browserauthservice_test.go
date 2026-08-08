package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"github.com/mengzhuo/cookiestxt"
)

// TestCookieExtraction proves the real DevTools-protocol capture path: it
// launches a browser, plants an HttpOnly Session_id cookie (as Yandex would
// after login), reads the cookies back through storage.GetCookies, and checks
// that our detection, Netscape rendering, and login extraction all work — and
// that the rendered cookies round-trip through the same parser the account
// loader uses.
func TestCookieExtraction(t *testing.T) {
	browserPath, err := findBrowser()
	if err != nil {
		t.Skipf("no Chromium browser available: %v", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	expires := cdpTime(time.Now().Add(24 * time.Hour))
	var cookies []*network.Cookie
	err = chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return storage.SetCookies([]*network.CookieParam{
				{Name: yandexSessionCookie, Value: "planted-session-value", Domain: ".yandex.ru", Path: "/", Secure: true, HTTPOnly: true, Expires: expires},
				{Name: "yandex_login", Value: "test-user", Domain: ".yandex.ru", Path: "/", Expires: expires},
				{Name: "plain", Value: "v", Domain: "market.yandex.ru", Path: "/", Expires: expires},
			}).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var innerErr error
			cookies, innerErr = storage.GetCookies().Do(ctx)
			return innerErr
		}),
	)
	if err != nil {
		t.Fatalf("browser cookie round-trip failed: %v", err)
	}

	if !hasSessionCookie(cookies) {
		t.Fatalf("hasSessionCookie did not detect the planted Session_id in %d cookies", len(cookies))
	}
	if login := extractLogin(cookies); login != "test-user" {
		t.Fatalf("extractLogin = %q, want %q", login, "test-user")
	}

	netscape := cookiesToNetscape(cookies)
	if !strings.Contains(netscape, "planted-session-value") {
		t.Fatalf("Netscape output is missing the session value:\n%s", netscape)
	}

	// The account loader parses cookies with this exact library; make sure our
	// output survives the round trip and the HttpOnly session cookie is kept.
	parsed, err := cookiestxt.Parse(strings.NewReader(netscape))
	if err != nil {
		t.Fatalf("loader cannot parse our Netscape output: %v\n%s", err, netscape)
	}
	var foundSession bool
	for _, c := range parsed {
		if c.Name == yandexSessionCookie && c.Value == "planted-session-value" {
			foundSession = true
		}
	}
	if !foundSession {
		t.Fatalf("Session_id did not survive Netscape round trip through the loader parser:\n%s", netscape)
	}

	t.Logf("captured %d cookies; Netscape output round-trips cleanly", len(cookies))
}

func cdpTime(t time.Time) *cdp.TimeSinceEpoch {
	epoch := cdp.TimeSinceEpoch(t)
	return &epoch
}
