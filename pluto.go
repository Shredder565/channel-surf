package main

// Fetches Pluto TV's public channel lineup + schedule at runtime and
// normalizes it into one small JSON shape for the guide page.
// Tries the current guide service first, then the legacy endpoint.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type Program struct {
	Title   string `json:"title"`
	Episode string `json:"episode,omitempty"`
	Desc    string `json:"desc,omitempty"`
	Rating  string `json:"rating,omitempty"`
	Genre   string `json:"genre,omitempty"`
	Image   string `json:"image,omitempty"`
	Start   string `json:"start"`
	Stop    string `json:"stop"`
}

type Channel struct {
	ID       string    `json:"id"`
	Number   int       `json:"number"`
	Name     string    `json:"name"`
	Slug     string    `json:"slug"`
	Category string    `json:"category"`
	Logo     string    `json:"logo,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Programs []Program `json:"programs"`
}

type Guide struct {
	Source   string    `json:"source"`
	Fetched  string    `json:"fetched"`
	Channels []Channel `json:"channels"`
}

const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

var (
	httpc     = &http.Client{Timeout: 20 * time.Second}
	cacheMu   sync.Mutex
	cached    *Guide
	cachedAt  time.Time
	clientID  = newUUID()
	lastError string
)

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func getGuide(force bool) (*Guide, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if !force && cached != nil && time.Since(cachedAt) < 10*time.Minute {
		return cached, nil
	}
	g, err1 := fetchModern()
	if err1 != nil {
		logf("modern guide failed: %v", err1)
		var err2 error
		g, err2 = fetchLegacy()
		if err2 != nil {
			logf("legacy guide failed: %v", err2)
			lastError = fmt.Sprintf("current service: %v | legacy service: %v", err1, err2)
			if cached != nil {
				return cached, nil
			}
			return nil, errors.New(lastError)
		}
	}
	sort.SliceStable(g.Channels, func(i, j int) bool { return g.Channels[i].Number < g.Channels[j].Number })
	g.Fetched = time.Now().UTC().Format(time.RFC3339)
	cached, cachedAt = g, time.Now()
	logf("guide ok from %s: %d channels", g.Source, len(g.Channels))
	return g, nil
}

func getJSON(u string, headers map[string]string, out any) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Origin", "https://pluto.tv")
	req.Header.Set("Referer", "https://pluto.tv/")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != 200 {
		s := string(body)
		if len(s) > 200 {
			s = s[:200]
		}
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, hostOf(u), s)
	}
	return json.Unmarshal(body, out)
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	return p.Host
}

// ---------- current guide service ----------

func fetchModern() (*Guide, error) {
	q := url.Values{}
	q.Set("appName", "web")
	q.Set("appVersion", "9.0.0")
	q.Set("deviceVersion", "130.0.0")
	q.Set("deviceModel", "web")
	q.Set("deviceMake", "chrome")
	q.Set("deviceType", "web")
	q.Set("clientID", clientID)
	q.Set("clientModelNumber", "1.0.0")
	q.Set("serverSideAds", "false")
	q.Set("drmCapabilities", "widevine:L3")
	var boot map[string]any
	if err := getJSON("https://boot.pluto.tv/v4/start?"+q.Encode(), nil, &boot); err != nil {
		return nil, fmt.Errorf("boot: %w", err)
	}
	token := str(boot["sessionToken"])
	if token == "" {
		return nil, errors.New("boot: no session token")
	}
	auth := map[string]string{"Authorization": "Bearer " + token}
	base := "https://service-channels.clusters.pluto.tv/v2/guide"

	var chResp map[string]any
	if err := getJSON(base+"/channels?channelIds=&offset=0&limit=1000&sort=number%3Aasc", auth, &chResp); err != nil {
		return nil, fmt.Errorf("channels: %w", err)
	}
	rawCh := arr(chResp["data"])
	if len(rawCh) == 0 {
		return nil, errors.New("channels: empty list")
	}

	catOf := map[string]string{}
	var catResp map[string]any
	if err := getJSON(base+"/categories", auth, &catResp); err == nil {
		for _, c := range arr(catResp["data"]) {
			cm := obj(c)
			name := str(cm["name"])
			for _, id := range arr(cm["channelIDs"]) {
				if _, seen := catOf[str(id)]; !seen {
					catOf[str(id)] = name
				}
			}
		}
	}

	g := &Guide{Source: "pluto-guide-v2"}
	idx := map[string]int{}
	var ids []string
	for _, c := range rawCh {
		cm := obj(c)
		id := str(cm["id"])
		if id == "" {
			continue
		}
		ch := Channel{
			ID: id, Name: str(cm["name"]), Slug: str(cm["slug"]), Number: num(cm["number"]),
			Summary: str(cm["summary"]), Category: catOf[id], Logo: pickLogo(cm),
		}
		if ch.Category == "" {
			ch.Category = str(cm["category"])
		}
		idx[id] = len(g.Channels)
		g.Channels = append(g.Channels, ch)
		ids = append(ids, id)
	}

	start := time.Now().UTC().Truncate(30 * time.Minute).Add(-30 * time.Minute).Format("2006-01-02T15:04:05.000Z")
	for i := 0; i < len(ids); i += 100 {
		j := i + 100
		if j > len(ids) {
			j = len(ids)
		}
		tq := url.Values{}
		tq.Set("start", start)
		tq.Set("channelIds", strings.Join(ids[i:j], ","))
		tq.Set("duration", "360")
		var tl map[string]any
		if err := getJSON(base+"/timelines?"+tq.Encode(), auth, &tl); err != nil {
			logf("timelines batch %d failed: %v", i/100, err)
			continue
		}
		for _, row := range arr(tl["data"]) {
			rm := obj(row)
			k, ok := idx[str(rm["channelId"])]
			if !ok {
				continue
			}
			for _, t := range arr(rm["timelines"]) {
				g.Channels[k].Programs = append(g.Channels[k].Programs, toProgram(obj(t)))
			}
		}
	}
	return g, nil
}

// ---------- legacy endpoint ----------

func fetchLegacy() (*Guide, error) {
	now := time.Now().UTC().Truncate(30 * time.Minute).Add(-30 * time.Minute)
	q := url.Values{}
	q.Set("start", now.Format("2006-01-02T15:04:05.000Z"))
	q.Set("stop", now.Add(6*time.Hour).Format("2006-01-02T15:04:05.000Z"))
	var raw []any
	if err := getJSON("https://api.pluto.tv/v2/channels?"+q.Encode(), nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("empty list")
	}
	g := &Guide{Source: "pluto-legacy-v2"}
	for _, c := range raw {
		cm := obj(c)
		if b, ok := cm["isStitched"].(bool); ok && !b {
			continue
		}
		ch := Channel{
			ID: str(cm["_id"]), Name: str(cm["name"]), Slug: str(cm["slug"]), Number: num(cm["number"]),
			Summary: str(cm["summary"]), Category: str(cm["category"]), Logo: pickLogo(cm),
		}
		for _, t := range arr(cm["timelines"]) {
			ch.Programs = append(ch.Programs, toProgram(obj(t)))
		}
		g.Channels = append(g.Channels, ch)
	}
	return g, nil
}

// ---------- helpers ----------

func toProgram(t map[string]any) Program {
	ep := obj(t["episode"])
	p := Program{
		Title: str(t["title"]), Start: str(t["start"]), Stop: str(t["stop"]),
		Episode: str(ep["name"]), Desc: str(ep["description"]), Rating: str(ep["rating"]), Genre: str(ep["genre"]),
	}
	if p.Title == "" {
		p.Title = str(obj(ep["series"])["name"])
	}
	if p.Title == "" {
		p.Title = p.Episode
	}
	return p
}

func pickLogo(cm map[string]any) string {
	for _, im := range arr(cm["images"]) {
		m := obj(im)
		t := str(m["type"])
		if t == "colorLogoPNG" || t == "logo" || t == "solidLogoPNG" {
			if u := str(m["url"]); u != "" {
				return u
			}
		}
	}
	for _, k := range []string{"colorLogoPNG", "solidLogoPNG", "logo"} {
		if u := str(obj(cm[k])["path"]); u != "" {
			return u
		}
	}
	return ""
}

func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func arr(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%g", x)
	}
	return ""
}
func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		var n int
		fmt.Sscanf(x, "%d", &n)
		return n
	}
	return 0
}
