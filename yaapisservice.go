package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
)

const (
	marketOrigin  = "https://market.yandex.ru"
	fortunePath   = "/kolesoprizov?track=menu"
	fortuneURL    = marketOrigin + fortunePath
	fortunePageID = "market:fortune-wheel"

	// Chrome 151 is the browser identity recorded in the reference HAR.
	browserUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
	browserClientHint = `"Not=A?Brand";v="99", "Google Chrome";v="151", "Chromium";v="151"`

	promoLoyaltyAPI = marketOrigin + "/api/web/market.front.mfPromoLoyalty.MfPromoLoyalty/"
)

// errIPBlocked marks Yandex's 403 VPN/proxy block page. The ERR_IP_BLOCKED
// prefix is matched by the frontend to show a per-account status instead of
// a toast.
var errIPBlocked = fmt.Errorf("ERR_IP_BLOCKED: Yandex rejected the request (403, VPN/proxy block)")

var (
	fortunePrizesURL = promoLoyaltyAPI + "resolveFortuneWheelPrizesScreenData"
	fortuneMainURL   = promoLoyaltyAPI + "resolveFortuneWheelMainScreenData"
	fortuneSpinURL   = promoLoyaltyAPI + "resolveFortuneWheelSpin"
	dailyScreenURL   = promoLoyaltyAPI + "resolveFortuneWheelDailyRewardsScreenData"
	dailyClaimURL    = promoLoyaltyAPI + "resolveFortuneWheelDailySignInReceiveReward"
	gamesHubURL      = promoLoyaltyAPI + "resolveGamesHubMiniMainScreenDataV2"
	gamesHubEventURL = promoLoyaltyAPI + "resolveGamesHubGameEvent"
)

// YaApiService handles interactions with the rewards API.
type YaApiService struct {
	clientCache     map[string]tls_client.HttpClient
	clientCacheLock sync.RWMutex
	authCache       map[string]marketAuthContext
	authCacheLock   sync.RWMutex
	eventCache      map[string]gameEventCacheEntry
	eventCacheLock  sync.RWMutex
}

type marketAuthContext struct {
	TokenSK     string
	Login       string
	CoinBalance string
	AppVersion  string
	FrontGlue   string
	Language    string
	FetchedAt   time.Time
}

type gameEventCacheEntry struct {
	Names     []string
	ExpiresAt time.Time
}

func (rs *YaApiService) init() {
	rs.clientCacheLock.Lock()
	if rs.clientCache == nil {
		rs.clientCache = make(map[string]tls_client.HttpClient)
	}
	rs.clientCacheLock.Unlock()

	rs.authCacheLock.Lock()
	if rs.authCache == nil {
		rs.authCache = make(map[string]marketAuthContext)
	}
	rs.authCacheLock.Unlock()

	rs.eventCacheLock.Lock()
	if rs.eventCache == nil {
		rs.eventCache = make(map[string]gameEventCacheEntry)
	}
	rs.eventCacheLock.Unlock()
}

var (
	skRegex           = regexp.MustCompile(`"sk":\s*"(.*?)"`)
	loginRegex        = regexp.MustCompile(`"login":\s*"(.*?)"`)
	coinBalanceRegex  = regexp.MustCompile(`"coinsAmount":\s*(\d+)`)
	appVersionRegex   = regexp.MustCompile(`"-version":"([^"]+-desktop\.[^"]+)"`)
	frontGlueRegex    = regexp.MustCompile(`"marketFrontGlue":"([^"]+)"`)
	languageRegex     = regexp.MustCompile(`"lang":"([^"]+)"`)
	scriptSourceRegex = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+)["']`)
	eventNameRegex    = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{3,}\b`)
)

type appHostRequest struct {
	Path   string `json:"path"`
	Params any    `json:"params"`
}

type fortuneMainResponse struct {
	Result struct {
		WheelInfo struct {
			UserInfo struct {
				Coins int `json:"coins"`
			} `json:"userInfo"`
		} `json:"wheelInfo"`
	} `json:"result"`
}

type dailyScreenResponse struct {
	Result struct {
		DailySignInInfo struct {
			RewardAvailable bool `json:"rewardAvailable"`
		} `json:"dailySignInInfo"`
	} `json:"result"`
}

// GetRewardsJson retrieves current prizes, login, and coin balance.
func (rs *YaApiService) GetRewardsJson(ctx context.Context, account *Account) (string, string, string, error) {
	if err := rs.ensureAuth(ctx, account); err != nil {
		return "", "", account.Login, fmt.Errorf("failed to authenticate: %w", err)
	}

	rewardsBody, err := rs.doJSON(ctx, account, http.MethodPost, fortunePrizesURL, "PROMOLOYALTY", appHostRequest{
		Path: fortunePath,
		Params: map[string]any{
			"wheel_ids": []string{"default_wheel"},
		},
	})
	if err != nil {
		return "", "", account.Login, fmt.Errorf("failed to get prizes: %w", err)
	}

	mainBody, err := rs.doJSON(ctx, account, http.MethodPost, fortuneMainURL, "PROMOLOYALTY", appHostRequest{
		Path:   fortunePath,
		Params: map[string]any{},
	})
	if err != nil {
		return "", "", account.Login, fmt.Errorf("failed to get wheel state: %w", err)
	}

	var mainResponse fortuneMainResponse
	if err := json.Unmarshal(mainBody, &mainResponse); err != nil {
		return "", "", account.Login, fmt.Errorf("failed to decode wheel state: %w", err)
	}
	account.CoinBalance = strconv.Itoa(mainResponse.Result.WheelInfo.UserInfo.Coins)

	return string(rewardsBody), account.Login, account.CoinBalance, nil
}

// ClaimDailyCoins reads the current daily plan and claims it only when available.
func (rs *YaApiService) ClaimDailyCoins(ctx context.Context, account *Account) (string, error) {
	if err := rs.ensureAuth(ctx, account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	screenBody, err := rs.doJSON(ctx, account, http.MethodPost, dailyScreenURL, "PROMOLOYALTY", appHostRequest{
		Path:   fortunePath,
		Params: map[string]any{},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get daily rewards: %w", err)
	}

	var screen dailyScreenResponse
	if err := json.Unmarshal(screenBody, &screen); err != nil {
		return "", fmt.Errorf("failed to decode daily rewards: %w", err)
	}
	if !screen.Result.DailySignInInfo.RewardAvailable {
		return string(screenBody), nil
	}

	claimBody, err := rs.doJSON(ctx, account, http.MethodPost, dailyClaimURL, "PROMOLOYALTY", appHostRequest{
		Path:   fortunePath,
		Params: map[string]any{},
	})
	if err != nil {
		return "", fmt.Errorf("failed to claim daily reward: %w", err)
	}

	return string(claimBody), nil
}

// Roll executes a spin using the active AppHost endpoint.
func (rs *YaApiService) Roll(ctx context.Context, account *Account) (string, error) {
	if err := rs.ensureAuth(ctx, account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	body, err := rs.doJSON(ctx, account, http.MethodPost, fortuneSpinURL, "PROMOLOYALTY", appHostRequest{
		Path: fortunePath,
		Params: map[string]any{
			"wheelId": "default_wheel",
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to spin wheel: %w", err)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return string(body), nil
}
