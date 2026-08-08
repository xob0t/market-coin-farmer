package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/mengzhuo/cookiestxt"
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

type gamesHubGame struct {
	GameID    string `json:"gameId"`
	GameToken string `json:"gameToken"`
	GameURL   string `json:"gameUrl"`
}

type gamesHubMission struct {
	MissionID    string `json:"missionId"`
	Status       string `json:"status"`
	RewardAmount int    `json:"rewardAmount"`
	Progress     struct {
		AccumulatedValue int `json:"accumulatedValue"`
		GoalValue        int `json:"goalValue"`
	} `json:"progress"`
	Action struct {
		OpenGameAction gamesHubGame `json:"openGameAction"`
	} `json:"action"`
}

type gamesHubScreenResponse struct {
	Result struct {
		GamesSection struct {
			Games        []gamesHubGame `json:"games"`
			FeaturedGame struct {
				Button struct {
					Action struct {
						OpenGameAction gamesHubGame `json:"openGameAction"`
					} `json:"action"`
				} `json:"button"`
			} `json:"featuredGame"`
		} `json:"gamesSection"`
		MissionsSection struct {
			MissionGroups []struct {
				Missions []gamesHubMission `json:"missions"`
			} `json:"missionGroups"`
		} `json:"missionsSection"`
	} `json:"result"`
}

type gameLevel struct {
	Level         string `json:"level"`
	RequiredScore int    `json:"requiredScore"`
	RewardPoints  int    `json:"rewardPoints"`
	AchievedToken string `json:"achievedToken"`
	IsAchieved    bool   `json:"isAchieved"`
}

type gameStatusResponse struct {
	Results []struct {
		Data struct {
			GameStatus struct {
				Levels []gameLevel `json:"levels"`
			} `json:"gameStatus"`
		} `json:"data"`
	} `json:"results"`
}

type gameClaimResult struct {
	GameID        string   `json:"gameId"`
	Processed     bool     `json:"processed"`
	ClaimedLevels []string `json:"claimedLevels,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type gameClaimSummary struct {
	DiscoveredGames       int               `json:"discoveredGames"`
	MissionsOpened        int               `json:"missionsOpened"`
	ChallengeEvents       int               `json:"challengeEvents"`
	CompletedChallenges   int               `json:"completedChallenges"`
	ClaimedLevels         int               `json:"claimedLevels"`
	UnsupportedChallenges []string          `json:"unsupportedChallenges,omitempty"`
	ChallengeErrors       []string          `json:"challengeErrors,omitempty"`
	Games                 []gameClaimResult `json:"games"`
}

// GetRewardsJson retrieves current prizes, login, and coin balance.
func (rs *YaApiService) GetRewardsJson(account *Account) (string, string, string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", "", account.Login, fmt.Errorf("failed to authenticate: %w", err)
	}

	rewardsBody, err := rs.doJSON(account, http.MethodPost, fortunePrizesURL, "PROMOLOYALTY", appHostRequest{
		Path: fortunePath,
		Params: map[string]any{
			"wheel_ids": []string{"default_wheel"},
		},
	})
	if err != nil {
		return "", "", account.Login, fmt.Errorf("failed to get prizes: %w", err)
	}

	mainBody, err := rs.doJSON(account, http.MethodPost, fortuneMainURL, "PROMOLOYALTY", appHostRequest{
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
func (rs *YaApiService) ClaimDailyCoins(account *Account) (string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	screenBody, err := rs.doJSON(account, http.MethodPost, dailyScreenURL, "PROMOLOYALTY", appHostRequest{
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

	claimBody, err := rs.doJSON(account, http.MethodPost, dailyClaimURL, "PROMOLOYALTY", appHostRequest{
		Path:   fortunePath,
		Params: map[string]any{},
	})
	if err != nil {
		return "", fmt.Errorf("failed to claim daily reward: %w", err)
	}

	return string(claimBody), nil
}

// Roll executes a spin using the active AppHost endpoint.
func (rs *YaApiService) Roll(account *Account) (string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	body, err := rs.doJSON(account, http.MethodPost, fortuneSpinURL, "PROMOLOYALTY", appHostRequest{
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

// ClaimGameRewards discovers current GamesHub games, submits their current
// maximum result, and claims any achieved legacy reward levels. New games are
// picked up from the server response without code changes.
func (rs *YaApiService) ClaimGameRewards(account *Account) (string, error) {
	if err := rs.ensureAuth(account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	screenBody, err := rs.doJSON(account, http.MethodPost, gamesHubURL, "PROMOLOYALTY", appHostRequest{
		Path:   fortunePath,
		Params: map[string]any{},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get GamesHub state: %w", err)
	}

	var screen gamesHubScreenResponse
	if err := json.Unmarshal(screenBody, &screen); err != nil {
		return "", fmt.Errorf("failed to decode GamesHub state: %w", err)
	}

	games := make(map[string]gamesHubGame)
	for _, game := range screen.Result.GamesSection.Games {
		addGamesHubGame(games, game)
	}
	addGamesHubGame(games, screen.Result.GamesSection.FeaturedGame.Button.Action.OpenGameAction)

	summary := gameClaimSummary{}
	attemptedChallenges := make(map[string]struct{})
	for _, group := range screen.Result.MissionsSection.MissionGroups {
		for _, mission := range group.Missions {
			addGamesHubGame(games, mission.Action.OpenGameAction)
			if discoveredGame, exists := games[mission.Action.OpenGameAction.GameID]; exists {
				if mission.Action.OpenGameAction.GameToken == "" {
					mission.Action.OpenGameAction.GameToken = discoveredGame.GameToken
				}
				if mission.Action.OpenGameAction.GameURL == "" {
					mission.Action.OpenGameAction.GameURL = discoveredGame.GameURL
				}
			}
			missionStatus := strings.ToUpper(mission.Status)
			if mission.MissionID == "" || strings.Contains(missionStatus, "COMPLETED") || strings.Contains(missionStatus, "RECEIVED") {
				continue
			}
			if _, err := rs.doLegacyResolver(account,
				"src/resolvers/gamesHub/resolveGamesHubMissionClickV2:resolveGamesHubMissionClickV2",
				"PROMOLOYALTY",
				map[string]any{"missionId": mission.MissionID},
			); err == nil {
				summary.MissionsOpened++
			}

			remaining := mission.Progress.GoalValue - mission.Progress.AccumulatedValue
			if remaining <= 0 {
				continue
			}
			eventType := rs.challengeEventType(account, mission)
			if eventType == "" {
				summary.UnsupportedChallenges = append(summary.UnsupportedChallenges, mission.MissionID)
				continue
			}
			if _, err := rs.doJSON(account, http.MethodPost, gamesHubEventURL, "PROMOLOYALTY", appHostRequest{
				Path: fortunePath,
				Params: map[string]any{
					"gameToken": mission.Action.OpenGameAction.GameToken,
					"eventType": eventType,
					"count":     remaining,
				},
			}); err != nil {
				summary.ChallengeErrors = append(summary.ChallengeErrors, mission.MissionID+": "+err.Error())
				continue
			}
			summary.ChallengeEvents++
			attemptedChallenges[mission.MissionID] = struct{}{}
		}
	}

	gameIDs := make([]string, 0, len(games))
	for gameID := range games {
		gameIDs = append(gameIDs, gameID)
	}
	sort.Strings(gameIDs)
	summary.DiscoveredGames = len(gameIDs)

	for _, gameID := range gameIDs {
		result := rs.processGamesHubGame(account, games[gameID])
		summary.ClaimedLevels += len(result.ClaimedLevels)
		summary.Games = append(summary.Games, result)
	}

	// Market Rush remains exposed through the legacy bulk reward resolver and
	// is not part of the GamesHub V2 game list in the reference capture.
	marketRushResult := gameClaimResult{GameID: "market_rush"}
	if _, err := rs.claimLegacyGameRewards(account, "market_rush", nil); err != nil {
		marketRushResult.Error = err.Error()
	} else {
		marketRushResult.Processed = true
	}
	summary.Games = append(summary.Games, marketRushResult)

	refreshedBody, refreshErr := rs.doJSON(account, http.MethodPost, gamesHubURL, "PROMOLOYALTY", appHostRequest{
		Path:   fortunePath,
		Params: map[string]any{},
	})
	if refreshErr == nil {
		var refreshed gamesHubScreenResponse
		if json.Unmarshal(refreshedBody, &refreshed) == nil {
			for _, group := range refreshed.Result.MissionsSection.MissionGroups {
				for _, mission := range group.Missions {
					if _, attempted := attemptedChallenges[mission.MissionID]; !attempted {
						continue
					}
					if mission.Progress.GoalValue > 0 && mission.Progress.AccumulatedValue >= mission.Progress.GoalValue {
						summary.CompletedChallenges++
						continue
					}
					status := strings.ToUpper(mission.Status)
					if strings.Contains(status, "COMPLETED") || strings.Contains(status, "RECEIVED") {
						summary.CompletedChallenges++
					}
				}
			}
		}
	}

	body, err := json.Marshal(summary)
	if err != nil {
		return "", fmt.Errorf("failed to encode game summary: %w", err)
	}
	return string(body), nil
}

func (rs *YaApiService) challengeEventType(account *Account, mission gamesHubMission) string {
	keywords := missionEventKeywords(mission)
	if len(keywords) == 0 {
		return ""
	}

	eventNames := rs.discoverGameEventNames(account, mission.Action.OpenGameAction)
	bestName := ""
	bestScore := 0
	bestMatches := 0
	for _, eventName := range eventNames {
		score, matches := scoreMissionEvent(eventName, keywords)
		if matches == 0 {
			continue
		}
		if score > bestScore || (score == bestScore && matches > bestMatches) ||
			(score == bestScore && matches == bestMatches && (bestName == "" || eventName < bestName)) {
			bestName = eventName
			bestScore = score
			bestMatches = matches
		}
	}
	return bestName
}

func (rs *YaApiService) discoverGameEventNames(account *Account, game gamesHubGame) []string {
	gameURL := strings.TrimSpace(game.GameURL)
	if gameURL == "" {
		return nil
	}

	rs.init()
	rs.eventCacheLock.RLock()
	cached, exists := rs.eventCache[gameURL]
	rs.eventCacheLock.RUnlock()
	if exists && time.Now().Before(cached.ExpiresAt) {
		return append([]string(nil), cached.Names...)
	}

	pageURL, err := url.Parse(gameURL)
	if err != nil || !isAllowedGameAssetURL(pageURL) {
		rs.cacheGameEventNames(gameURL, nil)
		return nil
	}

	pageBody, err := rs.fetchGameAsset(account, pageURL.String(), "", 2<<20)
	if err != nil {
		rs.cacheGameEventNames(gameURL, nil)
		return nil
	}

	names := make(map[string]struct{})
	collectEventNames(names, pageBody)

	type gameScript struct {
		url      string
		priority int
	}
	scripts := make([]gameScript, 0)
	seenScripts := make(map[string]struct{})
	for _, match := range scriptSourceRegex.FindAllSubmatch(pageBody, -1) {
		if len(match) < 2 {
			continue
		}
		reference, parseErr := url.Parse(string(match[1]))
		if parseErr != nil {
			continue
		}
		scriptURL := pageURL.ResolveReference(reference)
		if !isAllowedGameAssetURL(scriptURL) {
			continue
		}
		absoluteURL := scriptURL.String()
		if _, duplicate := seenScripts[absoluteURL]; duplicate {
			continue
		}
		seenScripts[absoluteURL] = struct{}{}

		fileName := strings.ToLower(path.Base(scriptURL.Path))
		priority := 2
		switch {
		case strings.HasPrefix(fileName, "game."):
			priority = 0
		case strings.HasPrefix(fileName, "main."):
			priority = 1
		case strings.Contains(fileName, "vendor"):
			priority = 3
		}
		scripts = append(scripts, gameScript{url: absoluteURL, priority: priority})
	}

	sort.SliceStable(scripts, func(i, j int) bool {
		if scripts[i].priority != scripts[j].priority {
			return scripts[i].priority < scripts[j].priority
		}
		return scripts[i].url < scripts[j].url
	})
	if len(scripts) > 6 {
		scripts = scripts[:6]
	}
	for _, script := range scripts {
		body, fetchErr := rs.fetchGameAsset(account, script.url, pageURL.String(), 5<<20)
		if fetchErr == nil {
			collectEventNames(names, body)
		}
	}

	discovered := make([]string, 0, len(names))
	for name := range names {
		discovered = append(discovered, name)
	}
	sort.Strings(discovered)
	rs.cacheGameEventNames(gameURL, discovered)
	return discovered
}

func (rs *YaApiService) cacheGameEventNames(gameURL string, names []string) {
	cacheLifetime := 15 * time.Minute
	if len(names) == 0 {
		cacheLifetime = time.Minute
	}
	rs.eventCacheLock.Lock()
	rs.eventCache[gameURL] = gameEventCacheEntry{
		Names:     append([]string(nil), names...),
		ExpiresAt: time.Now().Add(cacheLifetime),
	}
	rs.eventCacheLock.Unlock()
}

func (rs *YaApiService) fetchGameAsset(account *Account, assetURL, referer string, maxBytes int64) ([]byte, error) {
	client, err := rs.getClient(account)
	if err != nil {
		return nil, err
	}
	req, err := fhttp.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req, map[string]string{
		"accept":             "*/*",
		"accept-language":    "en-US,en;q=0.9",
		"referer":            referer,
		"sec-ch-ua":          browserClientHint,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"Windows"`,
		"sec-fetch-dest":     "script",
		"sec-fetch-mode":     "no-cors",
		"sec-fetch-site":     "cross-site",
		"user-agent":         browserUserAgent,
	})
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("game asset returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("game asset exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func isAllowedGameAssetURL(assetURL *url.URL) bool {
	if assetURL == nil || !strings.EqualFold(assetURL.Scheme, "https") {
		return false
	}
	host := strings.ToLower(assetURL.Hostname())
	return host == "yandex.ru" || strings.HasSuffix(host, ".yandex.ru") ||
		host == "yandex.net" || strings.HasSuffix(host, ".yandex.net")
}

func collectEventNames(destination map[string]struct{}, body []byte) {
	for _, match := range eventNameRegex.FindAll(body, -1) {
		name := string(match)
		if strings.ContainsRune(name, '_') || len(name) <= 24 {
			destination[name] = struct{}{}
		}
	}
}

func missionEventKeywords(mission gamesHubMission) []string {
	missionID := strings.ToLower(mission.MissionID)
	gameID := strings.ToLower(mission.Action.OpenGameAction.GameID)
	missionID = strings.TrimPrefix(missionID, gameID+"_")
	if index := strings.Index(missionID, "_wheel_"); index >= 0 {
		missionID = missionID[:index]
	}

	ignored := map[string]struct{}{
		"challenge": {}, "daily": {}, "event": {}, "game": {}, "mission": {}, "task": {}, "wheel": {},
	}
	keywords := make([]string, 0)
	seen := make(map[string]struct{})
	for _, part := range strings.FieldsFunc(missionID, func(r rune) bool { return r == '_' || r == '-' }) {
		part = normalizeEventToken(part)
		if part == "" || part == normalizeEventToken(gameID) || containsDigit(part) {
			continue
		}
		if _, skip := ignored[part]; skip {
			continue
		}
		if _, duplicate := seen[part]; duplicate {
			continue
		}
		seen[part] = struct{}{}
		keywords = append(keywords, part)
	}
	return keywords
}

func scoreMissionEvent(eventName string, keywords []string) (int, int) {
	eventTokens := make(map[string]struct{})
	for _, token := range strings.Split(strings.ToLower(eventName), "_") {
		token = normalizeEventToken(token)
		if token != "" {
			eventTokens[token] = struct{}{}
		}
	}

	score := 0
	matches := 0
	for _, keyword := range keywords {
		if _, exact := eventTokens[keyword]; exact {
			score += 20
			matches++
		}
	}
	if matches == len(keywords) {
		score += 10
	}
	if normalizeEventToken(eventName) == strings.Join(keywords, "_") {
		score += 15
	}
	// Prefer the candidate that explains the mission with the least unrelated text.
	score -= len(eventTokens) - matches
	return score, matches
}

func normalizeEventToken(value string) string {
	value = strings.ToLower(strings.Trim(value, "_"))
	if len(value) > 4 && strings.HasSuffix(value, "s") {
		value = strings.TrimSuffix(value, "s")
	}
	return value
}

func containsDigit(value string) bool {
	for _, r := range value {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func addGamesHubGame(games map[string]gamesHubGame, game gamesHubGame) {
	if game.GameID == "" {
		return
	}
	if existing, exists := games[game.GameID]; exists {
		if game.GameURL == "" {
			game.GameURL = existing.GameURL
		}
		if game.GameToken == "" {
			game.GameToken = existing.GameToken
		}
	}
	if game.GameToken == "" {
		return
	}
	games[game.GameID] = game
}

func (rs *YaApiService) processGamesHubGame(account *Account, game gamesHubGame) gameClaimResult {
	result := gameClaimResult{GameID: game.GameID}

	statusBody, err := rs.doLegacyResolver(account,
		"src/resolvers/gamesHub/resolveGamesHubGameStatusV2:resolveGamesHubGameStatusV2",
		"PROMOLOYALTY",
		map[string]any{"gameToken": game.GameToken},
	)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	levels, err := decodeGameLevels(statusBody)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	maxScore := 0
	needsResult := false
	for _, level := range levels {
		if level.RequiredScore > maxScore {
			maxScore = level.RequiredScore
		}
		if !level.IsAchieved {
			needsResult = true
		}
	}

	if needsResult && maxScore > 0 {
		processedBody, processErr := rs.doLegacyResolver(account,
			"src/resolvers/gamesHub/resolveGamesHubProcessGameResultV2:resolveGamesHubProcessGameResultV2",
			"PROMOLOYALTY",
			map[string]any{
				"gameToken": game.GameToken,
				"score":     maxScore,
			},
		)
		if processErr != nil {
			result.Error = processErr.Error()
			return result
		}
		levels, err = decodeGameLevels(processedBody)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Processed = true
	}

	claimableLevels := make([]string, 0, len(levels))
	tokenToLevel := make(map[string]string)
	for _, level := range levels {
		if level.AchievedToken == "" {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(level.Level, "RESULT_LEVEL_"))
		if name != "" {
			claimableLevels = append(claimableLevels, name)
			tokenToLevel[level.AchievedToken] = name
		}
	}
	if len(claimableLevels) == 0 {
		return result
	}

	claimBody, err := rs.claimLegacyGameRewards(account, game.GameID, claimableLevels)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.ClaimedLevels = decodeClaimedLevels(claimBody, tokenToLevel)
	return result
}

func decodeClaimedLevels(body []byte, tokenToLevel map[string]string) []string {
	var response struct {
		Results []struct {
			Data struct {
				Result map[string]any `json:"result"`
			} `json:"data"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Results) == 0 {
		return nil
	}

	claimed := make([]string, 0, len(tokenToLevel))
	for token, status := range response.Results[0].Data.Result {
		if !strings.EqualFold(fmt.Sprint(status), "success") {
			continue
		}
		if level := tokenToLevel[token]; level != "" {
			claimed = append(claimed, level)
		}
	}
	sort.Strings(claimed)
	return claimed
}

func decodeGameLevels(body []byte) ([]gameLevel, error) {
	var response gameStatusResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode game status: %w", err)
	}
	if len(response.Results) == 0 {
		return nil, fmt.Errorf("game status response has no results")
	}
	return response.Results[0].Data.GameStatus.Levels, nil
}

func (rs *YaApiService) claimLegacyGameRewards(account *Account, gameID string, levels []string) ([]byte, error) {
	params := map[string]any{"gameId": gameID}
	if len(levels) > 0 {
		params["levels"] = levels
	}
	return rs.doLegacyResolver(account,
		"src/resolvers/cashbackLevels/fortune/resolveFetchFortuneGameRewardBulk:resolveFortuneGameRewardBulk",
		"WEB",
		params,
	)
}

func (rs *YaApiService) doLegacyResolver(account *Account, resolver, target string, params map[string]any) ([]byte, error) {
	resolverURL := marketOrigin + "/api/resolve/?r=" + url.QueryEscape(resolver)
	return rs.doJSON(account, http.MethodPost, resolverURL, target, map[string]any{
		"params": []any{params},
		"path":   fortunePath,
	})
}

func (rs *YaApiService) ensureAuth(account *Account) error {
	rs.init()
	if account.TokenSK != "" && account.Login != "" && account.AppVersion != "" && account.FrontGlue != "" && time.Since(account.LastAuth) < time.Hour {
		return nil
	}

	cacheKey := account.Proxy + account.Cookies
	rs.authCacheLock.RLock()
	cached, exists := rs.authCache[cacheKey]
	rs.authCacheLock.RUnlock()
	if exists && time.Since(cached.FetchedAt) < time.Hour {
		account.TokenSK = cached.TokenSK
		account.Login = cached.Login
		account.CoinBalance = cached.CoinBalance
		account.AppVersion = cached.AppVersion
		account.FrontGlue = cached.FrontGlue
		account.Language = cached.Language
		account.LastAuth = cached.FetchedAt
		return nil
	}

	if err := rs.getProfileInfo(account); err != nil {
		return fmt.Errorf("failed to get profile info: %w", err)
	}
	account.LastAuth = time.Now()
	rs.authCacheLock.Lock()
	rs.authCache[cacheKey] = marketAuthContext{
		TokenSK:     account.TokenSK,
		Login:       account.Login,
		CoinBalance: account.CoinBalance,
		AppVersion:  account.AppVersion,
		FrontGlue:   account.FrontGlue,
		Language:    account.Language,
		FetchedAt:   account.LastAuth,
	}
	rs.authCacheLock.Unlock()
	return nil
}

func (rs *YaApiService) getProfileInfo(account *Account) error {
	client, err := rs.getClient(account)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	req, err := rs.createNavigationRequest(fortuneURL)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return errIPBlocked
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	account.TokenSK = extractStringValue(body, skRegex)
	account.Login = extractStringValue(body, loginRegex)
	account.CoinBalance = extractStringValue(body, coinBalanceRegex)
	account.AppVersion = extractStringValue(body, appVersionRegex)
	account.FrontGlue = extractStringValue(body, frontGlueRegex)
	account.Language = extractStringValue(body, languageRegex)
	if account.Language == "" {
		account.Language = "ru"
	}

	if account.TokenSK == "" || account.Login == "" {
		return fmt.Errorf("authenticated profile data is missing")
	}
	if account.AppVersion == "" || account.FrontGlue == "" {
		return fmt.Errorf("market request context is missing")
	}
	return nil
}

func (rs *YaApiService) doJSON(account *Account, method, requestURL, target string, payload any) ([]byte, error) {
	client, err := rs.getClient(account)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}
	req, err := rs.createAPIRequest(account, method, requestURL, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response: %w", readErr)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, errIPBlocked
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	return body, nil
}

func (rs *YaApiService) createNavigationRequest(requestURL string) (*fhttp.Request, error) {
	req, err := fhttp.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"accept-language":           "en-US,en;q=0.9",
		"cache-control":             "no-cache",
		"pragma":                    "no-cache",
		"priority":                  "u=0, i",
		"referer":                   marketOrigin + "/",
		"sec-ch-ua":                 browserClientHint,
		"sec-ch-ua-mobile":          "?0",
		"sec-ch-ua-platform":        `"Windows"`,
		"sec-fetch-dest":            "document",
		"sec-fetch-mode":            "navigate",
		"sec-fetch-site":            "same-origin",
		"sec-fetch-user":            "?1",
		"upgrade-insecure-requests": "1",
		"user-agent":                browserUserAgent,
	}
	setHeaders(req, headers)
	return req, nil
}

func (rs *YaApiService) createAPIRequest(account *Account, method, requestURL, target string, body io.Reader) (*fhttp.Request, error) {
	req, err := fhttp.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, err
	}

	acceptLanguage := "en-US,en;q=0.9"
	if strings.Contains(requestURL, "/api/web/") {
		acceptLanguage = account.Language
	}
	headers := map[string]string{
		"accept":                  "*/*",
		"accept-language":         acceptLanguage,
		"cache-control":           "no-cache",
		"content-type":            "application/json",
		"origin":                  marketOrigin,
		"pragma":                  "no-cache",
		"priority":                "u=1, i",
		"referer":                 fortuneURL,
		"sec-ch-ua":               browserClientHint,
		"sec-ch-ua-mobile":        "?0",
		"sec-ch-ua-platform":      `"Windows"`,
		"sec-fetch-dest":          "empty",
		"sec-fetch-mode":          "cors",
		"sec-fetch-site":          "same-origin",
		"sk":                      account.TokenSK,
		"user-agent":              browserUserAgent,
		"x-market-app-version":    account.AppVersion,
		"x-market-apphost-target": target,
		"x-market-core-service":   "<UNKNOWN>",
		"x-market-front-glue":     account.FrontGlue,
		"x-market-page-id":        fortunePageID,
		"x-requested-with":        "XMLHttpRequest",
		"x-retpath-y":             fortuneURL,
	}
	setHeaders(req, headers)
	return req, nil
}

func setHeaders(req *fhttp.Request, headers map[string]string) {
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
}

func (rs *YaApiService) getClient(account *Account) (tls_client.HttpClient, error) {
	rs.init()
	cacheKey := account.Proxy + account.Cookies

	rs.clientCacheLock.RLock()
	cachedClient, exists := rs.clientCache[cacheKey]
	rs.clientCacheLock.RUnlock()
	if exists {
		return cachedClient, nil
	}

	netscapeCookies, err := base64.StdEncoding.DecodeString(account.Cookies)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cookies: %w", err)
	}

	jar := tls_client.NewCookieJar()
	if err := loadCookiesFromNetscape(jar, string(netscapeCookies)); err != nil {
		return nil, fmt.Errorf("failed to load cookies: %w", err)
	}

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithProxyUrl(account.Proxy),
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithCookieJar(jar),
	}
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	rs.clientCacheLock.Lock()
	rs.clientCache[cacheKey] = client
	rs.clientCacheLock.Unlock()
	return client, nil
}

func loadCookiesFromNetscape(jar tls_client.CookieJar, cookieData string) error {
	cookieReader := strings.NewReader(cookieData)
	cookies, err := cookiestxt.Parse(cookieReader)
	if err != nil {
		return fmt.Errorf("failed to parse cookies: %w", err)
	}

	for _, cookie := range cookies {
		domain := strings.TrimPrefix(cookie.Domain, ".")
		u := &url.URL{
			Scheme: "https",
			Host:   domain,
			Path:   cookie.Path,
		}
		httpCookie := &fhttp.Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Path:     cookie.Path,
			Domain:   cookie.Domain,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HttpOnly,
		}
		jar.SetCookies(u, []*fhttp.Cookie{httpCookie})
	}
	return nil
}

func extractStringValue(body []byte, re *regexp.Regexp) string {
	matches := re.FindSubmatch(body)
	if len(matches) > 1 {
		return string(matches[1])
	}
	return ""
}
