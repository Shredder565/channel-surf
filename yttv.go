package main

// YouTube TV guide: YouTube TV's channel lineup, filled with schedules from
// TVmaze's free public API (api.tvmaze.com, CC BY-SA 4.0). YouTube TV has no
// public guide API, so live sports/news that TVmaze doesn't list show as gaps.

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type ytChan struct {
	Name, Cat string
	Aliases   []string // TVmaze network names that map to this channel
}

// Core of the YouTube TV base plan (lineup varies by market).
var ytLineup = []ytChan{
	{"ABC", "Local", nil}, {"CBS", "Local", nil}, {"NBC", "Local", nil}, {"FOX", "Local", nil},
	{"The CW", "Local", []string{"CW"}}, {"PBS", "Local", nil}, {"MyNetworkTV", "Local", []string{"MyNetwork TV"}}, {"ION", "Local", []string{"ION Television"}},
	{"ESPN", "Sports", nil}, {"ESPN2", "Sports", nil}, {"FS1", "Sports", []string{"Fox Sports 1"}}, {"CBS Sports Network", "Sports", nil},
	{"NFL Network", "Sports", nil}, {"NBA TV", "Sports", nil}, {"Golf Channel", "Sports", []string{"GOLF Channel"}},
	{"Big Ten Network", "Sports", []string{"BTN"}}, {"SEC Network", "Sports", nil}, {"ACC Network", "Sports", nil},
	{"CNN", "News", nil}, {"Fox News", "News", []string{"Fox News Channel", "FOX News"}}, {"MSNBC", "News", []string{"MS NOW"}},
	{"CNBC", "News", nil}, {"Fox Business", "News", []string{"FOX Business Network", "Fox Business Network"}}, {"HLN", "News", nil},
	{"NewsNation", "News", nil}, {"The Weather Channel", "News", nil}, {"BBC America", "News", []string{"BBC"}},
	{"AMC", "Entertainment", nil}, {"Bravo", "Entertainment", nil}, {"Comedy Central", "Entertainment", nil}, {"E!", "Entertainment", []string{"E! Entertainment"}},
	{"Freeform", "Entertainment", nil}, {"FX", "Entertainment", nil}, {"FXX", "Entertainment", nil}, {"Hallmark Channel", "Entertainment", nil},
	{"Hallmark Mystery", "Entertainment", []string{"Hallmark Movies & Mysteries"}}, {"IFC", "Entertainment", nil}, {"Paramount Network", "Entertainment", nil},
	{"Syfy", "Entertainment", nil}, {"TBS", "Entertainment", nil}, {"TNT", "Entertainment", nil}, {"truTV", "Entertainment", nil},
	{"TV Land", "Entertainment", nil}, {"USA Network", "Entertainment", nil}, {"VH1", "Entertainment", nil}, {"WE tv", "Entertainment", nil},
	{"MTV", "Entertainment", nil}, {"BET", "Entertainment", nil}, {"CMT", "Entertainment", nil}, {"OWN", "Entertainment", []string{"Oprah Winfrey Network"}},
	{"SundanceTV", "Entertainment", []string{"Sundance TV"}}, {"TCM", "Entertainment", []string{"Turner Classic Movies"}}, {"Game Show Network", "Entertainment", []string{"GSN"}},
	{"Food Network", "Lifestyle", nil}, {"HGTV", "Lifestyle", nil}, {"TLC", "Lifestyle", nil}, {"Travel Channel", "Lifestyle", nil},
	{"Magnolia Network", "Lifestyle", nil}, {"Discovery", "Lifestyle", []string{"Discovery Channel"}}, {"Animal Planet", "Lifestyle", nil},
	{"National Geographic", "Lifestyle", []string{"Nat Geo", "National Geographic Channel"}}, {"Nat Geo Wild", "Lifestyle", nil},
	{"Smithsonian Channel", "Lifestyle", nil}, {"Investigation Discovery", "Lifestyle", []string{"ID"}}, {"Oxygen", "Lifestyle", []string{"Oxygen True Crime"}},
	{"MotorTrend", "Lifestyle", []string{"MotorTrend TV", "Motor Trend"}},
	{"Disney Channel", "Kids", nil}, {"Disney Junior", "Kids", nil}, {"Disney XD", "Kids", nil}, {"Nickelodeon", "Kids", nil},
	{"Nick Jr.", "Kids", []string{"Nick Jr"}}, {"Cartoon Network", "Kids", nil}, {"Adult Swim", "Kids", []string{"[adult swim]"}}, {"PBS Kids", "Kids", []string{"PBS KIDS"}},
}

var (
	ytMu       sync.Mutex
	ytCached   *Guide
	ytCachedAt time.Time
	tagRe      = regexp.MustCompile(`<[^>]*>`)
)

func normNet(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func getYTTV(force bool) (*Guide, error) {
	ytMu.Lock()
	defer ytMu.Unlock()
	if !force && ytCached != nil && time.Since(ytCachedAt) < 10*time.Minute {
		return ytCached, nil
	}
	g, err := fetchYTTV(time.Now())
	if err != nil {
		logf("youtube tv guide failed: %v", err)
		if ytCached != nil {
			return ytCached, nil
		}
		return nil, err
	}
	ytCached, ytCachedAt = g, time.Now()
	logf("youtube tv guide ok: %d channels", len(g.Channels))
	return g, nil
}

func fetchYTTV(now time.Time) (*Guide, error) {
	idx := map[string]int{}
	g := &Guide{Source: "tvmaze"}
	for i, c := range ytLineup {
		g.Channels = append(g.Channels, Channel{ID: "yt" + fmt.Sprint(i), Number: i + 1, Name: c.Name, Category: c.Cat, Slug: c.Name})
		idx[normNet(c.Name)] = i
		for _, a := range c.Aliases {
			idx[normNet(a)] = i
		}
	}

	var errs []string
	got := 0
	// Yesterday covers shows still running past midnight; tomorrow fills the late-night grid.
	for _, d := range []time.Time{now.AddDate(0, 0, -1), now, now.AddDate(0, 0, 1)} {
		var eps []any
		if err := getJSON("https://api.tvmaze.com/schedule?country=US&date="+d.Format("2006-01-02"), nil, &eps); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		got++
		for _, e := range eps {
			em := obj(e)
			show := obj(em["show"])
			net := str(obj(show["network"])["name"])
			k, ok := idx[normNet(net)]
			if !ok {
				continue
			}
			start, err := time.Parse(time.RFC3339, str(em["airstamp"]))
			if err != nil {
				continue
			}
			rt := num(em["runtime"])
			if rt <= 0 {
				rt = num(show["runtime"])
			}
			if rt <= 0 {
				rt = 30
			}
			epName := str(em["name"])
			if s, n := num(em["season"]), num(em["number"]); s > 0 && n > 0 {
				epName = fmt.Sprintf("S%d E%d · %s", s, n, epName)
			}
			desc := str(em["summary"])
			if desc == "" {
				desc = str(show["summary"])
			}
			genres := []string{}
			for _, gname := range arr(show["genres"]) {
				genres = append(genres, str(gname))
			}
			img := str(obj(show["image"])["medium"])
			img = strings.Replace(img, "http://", "https://", 1)
			g.Channels[k].Programs = append(g.Channels[k].Programs, Program{
				Title: str(show["name"]), Episode: epName, Desc: strings.TrimSpace(tagRe.ReplaceAllString(desc, "")),
				Genre: strings.Join(genres, " · "), Rating: str(show["type"]), Image: img,
				Start: start.UTC().Format(time.RFC3339), Stop: start.Add(time.Duration(rt) * time.Minute).UTC().Format(time.RFC3339),
			})
		}
	}
	if got == 0 {
		return nil, errors.New("TVmaze: " + strings.Join(errs, " | "))
	}
	// Sort and drop overlapping duplicates (TVmaze sometimes lists two feeds for one slot).
	for i := range g.Channels {
		p := g.Channels[i].Programs
		sort.SliceStable(p, func(a, b int) bool { return p[a].Start < p[b].Start })
		out := p[:0]
		var lastStop string
		for _, x := range p {
			if lastStop != "" && x.Start < lastStop {
				continue
			}
			out = append(out, x)
			lastStop = x.Stop
		}
		g.Channels[i].Programs = out
	}
	g.Fetched = time.Now().UTC().Format(time.RFC3339)
	return g, nil
}
