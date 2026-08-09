package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
)

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

// ClaimGameRewards discovers current GamesHub games, submits their current
// maximum result, and claims any achieved legacy reward levels. New games are
// picked up from the server response without code changes.
func (rs *YaApiService) ClaimGameRewards(ctx context.Context, account *Account) (string, error) {
	if err := rs.ensureAuth(ctx, account); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	screenBody, err := rs.doJSON(ctx, account, http.MethodPost, gamesHubURL, "PROMOLOYALTY", appHostRequest{
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
			if _, err := rs.doLegacyResolver(ctx, account,
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
			eventType := rs.challengeEventType(ctx, account, mission)
			if eventType == "" {
				summary.UnsupportedChallenges = append(summary.UnsupportedChallenges, mission.MissionID)
				continue
			}
			if _, err := rs.doJSON(ctx, account, http.MethodPost, gamesHubEventURL, "PROMOLOYALTY", appHostRequest{
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
		result := rs.processGamesHubGame(ctx, account, games[gameID])
		summary.ClaimedLevels += len(result.ClaimedLevels)
		summary.Games = append(summary.Games, result)
	}

	// Market Rush remains exposed through the legacy bulk reward resolver and
	// is not part of the GamesHub V2 game list in the reference capture.
	marketRushResult := gameClaimResult{GameID: "market_rush"}
	if _, err := rs.claimLegacyGameRewards(ctx, account, "market_rush", nil); err != nil {
		marketRushResult.Error = err.Error()
	} else {
		marketRushResult.Processed = true
	}
	summary.Games = append(summary.Games, marketRushResult)

	refreshedBody, refreshErr := rs.doJSON(ctx, account, http.MethodPost, gamesHubURL, "PROMOLOYALTY", appHostRequest{
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

func (rs *YaApiService) challengeEventType(ctx context.Context, account *Account, mission gamesHubMission) string {
	keywords := missionEventKeywords(mission)
	if len(keywords) == 0 {
		return ""
	}

	eventNames := rs.discoverGameEventNames(ctx, account, mission.Action.OpenGameAction)
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

func (rs *YaApiService) discoverGameEventNames(ctx context.Context, account *Account, game gamesHubGame) []string {
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

	pageBody, err := rs.fetchGameAsset(ctx, account, pageURL.String(), "", 2<<20)
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
		body, fetchErr := rs.fetchGameAsset(ctx, account, script.url, pageURL.String(), 5<<20)
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

func (rs *YaApiService) fetchGameAsset(ctx context.Context, account *Account, assetURL, referer string, maxBytes int64) ([]byte, error) {
	client, err := rs.getClient(account)
	if err != nil {
		return nil, err
	}
	req, err := fhttp.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
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

func (rs *YaApiService) processGamesHubGame(ctx context.Context, account *Account, game gamesHubGame) gameClaimResult {
	result := gameClaimResult{GameID: game.GameID}

	statusBody, err := rs.doLegacyResolver(ctx, account,
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
		processedBody, processErr := rs.doLegacyResolver(ctx, account,
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

	claimBody, err := rs.claimLegacyGameRewards(ctx, account, game.GameID, claimableLevels)
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

func (rs *YaApiService) claimLegacyGameRewards(ctx context.Context, account *Account, gameID string, levels []string) ([]byte, error) {
	params := map[string]any{"gameId": gameID}
	if len(levels) > 0 {
		params["levels"] = levels
	}
	return rs.doLegacyResolver(ctx, account,
		"src/resolvers/cashbackLevels/fortune/resolveFetchFortuneGameRewardBulk:resolveFortuneGameRewardBulk",
		"WEB",
		params,
	)
}

func (rs *YaApiService) doLegacyResolver(ctx context.Context, account *Account, resolver, target string, params map[string]any) ([]byte, error) {
	resolverURL := marketOrigin + "/api/resolve/?r=" + url.QueryEscape(resolver)
	return rs.doJSON(ctx, account, http.MethodPost, resolverURL, target, map[string]any{
		"params": []any{params},
		"path":   fortunePath,
	})
}
