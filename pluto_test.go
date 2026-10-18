package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeRT map[string]string // host+path prefix -> body

func (f fakeRT) RoundTrip(r *http.Request) (*http.Response, error) {
	key := r.URL.Host + r.URL.Path
	for k, body := range f {
		if strings.HasPrefix(key, k) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		}
	}
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("nope")), Header: http.Header{}}, nil
}

func TestModern(t *testing.T) {
	httpc.Transport = fakeRT{
		"boot.pluto.tv/v4/start":                                 `{"sessionToken":"abc"}`,
		"service-channels.clusters.pluto.tv/v2/guide/channels":   `{"data":[{"id":"c1","slug":"pluto-tv-westerns","name":"Westerns","number":515,"summary":"Yee-haw","images":[{"type":"colorLogoPNG","url":"https://img/x.png"}]},{"id":"c2","slug":"news","name":"News Now","number":100}]}`,
		"service-channels.clusters.pluto.tv/v2/guide/categories": `{"data":[{"name":"Movies","channelIDs":["c1"]},{"name":"News","channelIDs":["c2"]}]}`,
		"service-channels.clusters.pluto.tv/v2/guide/timelines":  `{"data":[{"channelId":"c1","timelines":[{"title":"Gunsmoke","start":"2026-10-18T18:00:00.000Z","stop":"2026-10-18T19:00:00.000Z","episode":{"name":"Ep 4","description":"Matt rides.","rating":"TV-PG","genre":"Western"}}]}]}`,
	}
	g, err := fetchModern()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Channels) != 2 || g.Channels[0].Category != "Movies" || g.Channels[0].Logo != "https://img/x.png" ||
		len(g.Channels[0].Programs) != 1 || g.Channels[0].Programs[0].Desc != "Matt rides." || g.Channels[1].Number != 100 {
		t.Fatalf("bad parse: %+v", g)
	}
}

func TestLegacy(t *testing.T) {
	httpc.Transport = fakeRT{
		"api.pluto.tv/v2/channels": `[{"_id":"x1","slug":"comedy","name":"Comedy Central Pluto","number":"465","category":"Comedy","isStitched":true,"colorLogoPNG":{"path":"https://img/c.png"},"timelines":[{"title":"Stand-Up","start":"2026-10-18T18:00:00.000Z","stop":"2026-10-18T18:30:00.000Z","episode":{"description":"Jokes."}}]}]`,
	}
	g, err := fetchLegacy()
	if err != nil {
		t.Fatal(err)
	}
	c := g.Channels[0]
	if c.Number != 465 || c.Logo != "https://img/c.png" || c.Category != "Comedy" || c.Programs[0].Title != "Stand-Up" {
		t.Fatalf("bad parse: %+v", c)
	}
}

func TestFallback(t *testing.T) {
	cached = nil
	httpc.Transport = fakeRT{
		"api.pluto.tv/v2/channels": `[{"_id":"x1","slug":"s","name":"Only Legacy","number":1}]`,
	}
	g, err := getGuide(true)
	if err != nil || g.Source != "pluto-legacy-v2" {
		t.Fatalf("fallback failed: %v %+v", err, g)
	}
	cached = nil
	httpc.Transport = fakeRT{}
	if _, err := getGuide(true); err == nil {
		t.Fatal("expected error when both fail")
	}
}

func TestYTTV(t *testing.T) {
	httpc.Transport = fakeRT{
		"api.tvmaze.com/schedule": `[
		 {"name":"Pilot","season":2,"number":1,"airstamp":"2026-10-18T20:00:00-04:00","runtime":60,"summary":"<p>A <b>big</b> night.</p>","show":{"name":"Big Show","network":{"name":"NBC"},"genres":["Drama"],"type":"Scripted","image":{"medium":"http://img/x.jpg"}}},
		 {"name":"Dup","airstamp":"2026-10-18T20:30:00-04:00","runtime":30,"show":{"name":"Overlap","network":{"name":"NBC"}}},
		 {"name":"House","airstamp":"2026-10-18T21:00:00-04:00","runtime":null,"show":{"name":"Flip It","runtime":30,"network":{"name":"HGTV"}}},
		 {"name":"X","airstamp":"2026-10-18T21:00:00-04:00","runtime":30,"show":{"name":"Not Carried","network":{"name":"A&E"}}},
		 {"name":"Y","airstamp":"2026-10-18T21:00:00-04:00","runtime":30,"show":{"name":"News Hour","network":{"name":"Fox News Channel"}}}]`,
	}
	g, err := fetchYTTV(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Channel{}
	for _, c := range g.Channels {
		by[c.Name] = c
	}
	nbc := by["NBC"].Programs
	if len(nbc) != 1 || nbc[0].Desc != "A big night." || nbc[0].Image != "https://img/x.jpg" || nbc[0].Episode != "S2 E1 · Pilot" || nbc[0].Stop != "2026-10-19T01:00:00Z" {
		t.Fatalf("nbc: %+v", nbc)
	}
	if len(by["HGTV"].Programs) != 1 || by["HGTV"].Programs[0].Stop != "2026-10-19T01:30:00Z" {
		t.Fatalf("hgtv: %+v", by["HGTV"].Programs)
	}
	if len(by["Fox News"].Programs) != 1 {
		t.Fatal("alias match failed")
	}
}
