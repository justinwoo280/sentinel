package quality

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// MediaResult holds the results of all media/AI unlock tests.
type MediaResult struct {
	TikTok     MediaEntry
	DisneyPlus MediaEntry
	Netflix    MediaEntry
	YouTube    MediaEntry
	Amazon     MediaEntry
	Reddit     MediaEntry
	ChatGPT    MediaEntry
}

func (q *Quality) queryMedia(ctx context.Context) MediaResult {
	var result MediaResult

	// Run all media tests concurrently.
	type mediaTask struct {
		name string
		fn   func(context.Context) MediaEntry
	}
	tasks := []mediaTask{
		{"tiktok", q.testTikTok},
		{"disney", q.testDisneyPlus},
		{"netflix", q.testNetflix},
		{"youtube", q.testYouTube},
		{"amazon", q.testAmazon},
		{"reddit", q.testReddit},
		{"chatgpt", q.testChatGPT},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(task mediaTask) {
			defer wg.Done()
			entry := task.fn(ctx)
			mu.Lock()
			switch task.name {
			case "tiktok":
				result.TikTok = entry
			case "disney":
				result.DisneyPlus = entry
			case "netflix":
				result.Netflix = entry
			case "youtube":
				result.YouTube = entry
			case "amazon":
				result.Amazon = entry
			case "reddit":
				result.Reddit = entry
			case "chatgpt":
				result.ChatGPT = entry
			}
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	return result
}

func (r MediaResult) ToJSON() Media {
	return Media{
		TikTok:           r.TikTok,
		DisneyPlus:       r.DisneyPlus,
		Netflix:          r.Netflix,
		Youtube:          r.YouTube,
		AmazonPrimeVideo: r.Amazon,
		Reddit:           r.Reddit,
		ChatGPT:          r.ChatGPT,
	}
}

// --- Individual media tests ---

var regionRe = regexp.MustCompile(`"region"\s*:\s*"([^"]+)"`)

func (q *Quality) testTikTok(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, err := q.httpGetBody(ctx, urlTikTok)
	if err != nil {
		return failedEntry()
	}

	if match := regionRe.FindSubmatch(body); match != nil {
		region := string(match[1])
		return okEntry(region, "DNS")
	}
	return failedEntry()
}

func (q *Quality) testDisneyPlus(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Step 1: Register device.
	deviceReq, _ := http.NewRequestWithContext(ctx, "POST", urlDisneyDevices, strings.NewReader(`{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}`))
	deviceReq.Header.Set("authorization", "Bearer "+disneyBearer)
	deviceReq.Header.Set("content-type", "application/json; charset=UTF-8")
	deviceReq.Header.Set("User-Agent", q.ua)
	resp, err := q.http.Do(deviceReq)
	if err != nil {
		return failedEntry()
	}
	defer resp.Body.Close()
	deviceBody, _ := io.ReadAll(resp.Body)

	var deviceData struct {
		Assertion string `json:"assertion"`
	}
	if err := json.Unmarshal(deviceBody, &deviceData); err != nil || deviceData.Assertion == "" {
		return failedEntry()
	}

	// Step 2: Get token.
	tokenReq, _ := http.NewRequestWithContext(ctx, "POST", urlDisneyToken, strings.NewReader("grant_type=urn:ietf:params:oauth:grant-type:token-exchange&assertion="+deviceData.Assertion))
	tokenReq.Header.Set("authorization", "Bearer "+disneyBearer)
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.Header.Set("User-Agent", q.ua)
	tokenResp, err := q.http.Do(tokenReq)
	if err != nil {
		return failedEntry()
	}
	defer tokenResp.Body.Close()
	tokenBody, _ := io.ReadAll(tokenResp.Body)

	var tokenData struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(tokenBody, &tokenData); err != nil || tokenData.RefreshToken == "" {
		return failedEntry()
	}

	// Step 3: GraphQL query to get region.
	graphqlReq, _ := http.NewRequestWithContext(ctx, "POST", urlDisneyGraphQL,
		strings.NewReader(`{"query":"mutation refreshToken($refreshToken:String!){refreshToken(refreshToken:$refreshToken){activeSession{sessionId{value}}}}","variables":{"refreshToken":"`+tokenData.RefreshToken+`"}}`))
	graphqlReq.Header.Set("authorization", disneyBearer)
	graphqlReq.Header.Set("Content-Type", "application/json")
	graphqlReq.Header.Set("User-Agent", q.ua)
	gqlResp, err := q.http.Do(graphqlReq)
	if err != nil {
		return failedEntry()
	}
	defer gqlResp.Body.Close()
	gqlBody, _ := io.ReadAll(gqlResp.Body)

	var gqlData struct {
		Extensions struct {
			SDK struct {
				Session struct {
					Location struct {
						CountryCode string `json:"countryCode"`
					} `json:"location"`
					InSupportedLocation bool `json:"inSupportedLocation"`
				} `json:"session"`
			} `json:"sdk"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(gqlBody, &gqlData); err != nil {
		return failedEntry()
	}

	region := gqlData.Extensions.SDK.Session.Location.CountryCode
	if region == "" {
		return noEntry()
	}
	if gqlData.Extensions.SDK.Session.InSupportedLocation {
		return okEntry(region, "DNS")
	}
	return pendingEntry(region)
}

func (q *Quality) testNetflix(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Try two titles: one custom (81280792), one standard (70143836).
	body1, err1 := q.httpGetBody(ctx, urlNetflixTitle1)
	body2, err2 := q.httpGetBody(ctx, urlNetflixTitle2)
	if err1 != nil && err2 != nil {
		return failedEntry()
	}

	// Check for "Oh no!" (blocked message).
	ohno1 := strings.Contains(string(body1), "Oh no!")
	ohno2 := strings.Contains(string(body2), "Oh no!")

	// Extract region from page content.
	region := extractNetflixRegion(body1)
	if region == "" {
		region = extractNetflixRegion(body2)
	}

	if ohno1 && ohno2 {
		// Both blocked → only Original content available.
		if region != "" {
			return entry("Originals Only", region, "DNS")
		}
		return entry("Originals Only", "", "")
	}
	if !ohno1 || !ohno2 {
		// At least one available.
		return okEntry(region, "DNS")
	}
	return noEntry()
}

func (q *Quality) testYouTube(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Use cookies like xykt does.
	req, _ := http.NewRequestWithContext(ctx, "GET", urlYouTubePremium, nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("User-Agent", q.ua)
	req.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+cb.20220301-11-p0.en+FX+700"})
	resp, err := q.http.Do(req)
	if err != nil {
		return failedEntry()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Check for CN redirect.
	if strings.Contains(string(body), "www.google.cn") {
		return entry("CN", "", "")
	}

	// Check "Premium is not available in your country".
	if strings.Contains(string(body), "Premium is not available in your country") {
		return entry("No Premium", "", "")
	}

	// Extract contentRegion.
	regionRe := regexp.MustCompile(`"contentRegion"\s*:\s*"([^"]+)"`)
	if match := regionRe.FindSubmatch(body); match != nil {
		return okEntry(string(match[1]), "DNS")
	}

	// Check if ad-free is available.
	if strings.Contains(string(body), "ad-free") {
		return okEntry("", "DNS")
	}

	return failedEntry()
}

func (q *Quality) testAmazon(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, err := q.httpGetBody(ctx, urlPrimeVideo)
	if err != nil {
		return failedEntry()
	}

	re := regexp.MustCompile(`"currentTerritory"\s*:\s*"([^"]+)"`)
	if match := re.FindSubmatch(body); match != nil {
		return okEntry(string(match[1]), "DNS")
	}
	return noEntry()
}

func (q *Quality) testReddit(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", urlReddit, nil)
	req.Header.Set("User-Agent", q.ua)
	resp, err := q.http.Do(req)
	if err != nil {
		return failedEntry()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 200 {
		re := regexp.MustCompile(`country="([^"]+)"`)
		if match := re.FindSubmatch(body); match != nil {
			return okEntry(string(match[1]), "DNS")
		}
		return okEntry("", "DNS")
	}
	if resp.StatusCode == 403 {
		return noEntry()
	}
	return failedEntry()
}

func (q *Quality) testChatGPT(ctx context.Context) MediaEntry {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Check API compliance endpoint.
	apiResp, apiErr := q.httpGetBody(ctx, urlOpenAICompliance)
	apiBlocked := apiErr != nil || strings.Contains(string(apiResp), "unsupported_country")

	// Check iOS chat endpoint.
	iosResp, iosErr := q.httpGetBody(ctx, urlIOSChatOpenAI)
	iosBlocked := iosErr != nil || strings.Contains(string(iosResp), "VPN")

	// Get country code from trace.
	region := ""
	if traceBody, err := q.httpGetBody(ctx, urlChatOpenAITrace); err == nil {
		for _, line := range strings.Split(string(traceBody), "\n") {
			if strings.HasPrefix(line, "loc=") {
				region = strings.TrimPrefix(line, "loc=")
				break
			}
		}
	}

	if !apiBlocked && !iosBlocked {
		return okEntry(region, "DNS")
	}
	if apiBlocked && iosBlocked {
		return noEntry()
	}
	if !apiBlocked && iosBlocked {
		return entry("Web Only", region, "DNS")
	}
	if apiBlocked && !iosBlocked {
		return entry("App Only", region, "DNS")
	}
	return failedEntry()
}

// --- Entry constructors ---

func okEntry(region, typ string) MediaEntry {
	status := "Yes"
	return MediaEntry{
		Status: &status,
		Region: &region,
		Type:   &typ,
	}
}

func failedEntry() MediaEntry {
	status := "Failed"
	nd := "N/A"
	return MediaEntry{Status: &status, Region: &nd, Type: &nd}
}

func noEntry() MediaEntry {
	status := "No"
	nd := "N/A"
	return MediaEntry{Status: &status, Region: &nd, Type: &nd}
}

func pendingEntry(region string) MediaEntry {
	status := "Pending"
	return MediaEntry{Status: &status, Region: &region, Type: ptrStr("DNS")}
}

func entry(status, region, typ string) MediaEntry {
	nd := "N/A"
	e := MediaEntry{Status: &status}
	if region != "" {
		e.Region = &region
	} else {
		e.Region = &nd
	}
	if typ != "" {
		e.Type = &typ
	} else {
		e.Type = &nd
	}
	return e
}

func extractNetflixRegion(body []byte) string {
	// Try to extract country code from Netflix page JSON.
	re := regexp.MustCompile(`"id"\s*:\s*"([A-Z]{2})"`)
	if match := re.FindSubmatch(body); match != nil {
		return string(match[1])
	}
	return ""
}

// --- HTTP body helper ---

func (q *Quality) httpGetBody(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", q.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en")
	resp, err := q.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
