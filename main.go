package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var site = strings.TrimRight(setting("WSM_SITE", "https://watchsomuchtorrents.com"), "/")
var secret = os.Getenv("TORZNAB_API_KEY")
var httpc = &http.Client{Timeout: 25 * time.Second}
var rx = regexp.MustCompile("\\[(\\d+),\\\"((?:\\\\.|[^\\\"\\\\])*)\\\",\\\"((?:\\\\.|[^\\\"\\\\])*)\\\",(\\d{4}),\\\"((?:\\\\.|[^\\\"\\\\])*)\\\"")
var hrefRX = regexp.MustCompile("(?i)href=\\\"(/Movie/[^\\\"]+/Download-[^\\\"]+)\\\"")
var magnetRX = regexp.MustCompile("(?i)(?:value|href)=\\\"(magnet:\\?[^\\\"]+)\\\"")
var sizeRX = regexp.MustCompile("(?i)(\\d+(?:\\.\\d+)?)\\s*(GiB|MiB|GB|MB)")
var yrRX = regexp.MustCompile("\\b(?:19|20)\\d{2}\\b")
var seasonRX = regexp.MustCompile("(?i)\\bS\\d\\dE\\d\\d\\b")

type Movie struct {
	ID     int
	Name   string
	Year   int
	Source string
}
type Release struct {
	Title, Link string
	Size        int64
	Category    int
}

var mu sync.RWMutex
var movies []Movie
var fetched time.Time
var fetching bool

func setting(n, d string) string {
	if v := os.Getenv(n); v != "" {
		return v
	}
	return d
}
func fetch(ctx context.Context, p string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", site+p, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, e := httpc.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 24<<20))
}
func decode(s string) string {
	var r string
	if json.Unmarshal([]byte("\""+s+"\""), &r) == nil {
		return r
	}
	return s
}
func normalize(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		} else {
			b.WriteByte(32)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func slug(s string) string { return strings.ReplaceAll(normalize(s), " ", "-") }
func populate() {
	mu.Lock()
	if fetching {
		mu.Unlock()
		return
	}
	fetching = true
	mu.Unlock()
	defer func() { mu.Lock(); fetching = false; mu.Unlock() }()
	var acc []Movie
	seen := map[int]bool{}
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		body, e := fetch(ctx, "/Includes/MovieData.aspx?t="+strconv.Itoa(i))
		cancel()
		if e != nil {
			log.Println("catalog segment", i, e)
			continue
		}
		for _, x := range rx.FindAllSubmatch(body, -1) {
			id, _ := strconv.Atoi(string(x[1]))
			y, _ := strconv.Atoi(string(x[4]))
			if id > 0 && !seen[id] {
				seen[id] = true
				acc = append(acc, Movie{id, decode(string(x[2])), y, decode(string(x[5]))})
			}
		}
	}
	mu.RLock()
	oldCount := len(movies)
	mu.RUnlock()
	if len(acc) >= 10000 && (oldCount == 0 || len(acc)*10 >= oldCount*8) {
		mu.Lock()
		movies = acc
		fetched = time.Now()
		mu.Unlock()
		log.Printf("Catalog contains %d titles", len(acc))
		saveCache(acc)
	}
}
func current() []Movie {
	mu.RLock()
	a := movies
	old := time.Since(fetched) > 6*time.Hour
	busy := fetching
	mu.RUnlock()
	if old && !busy {
		go populate()
	}
	return a
}
func moviePath(m Movie) string {
	src := m.Source
	if src == "" {
		src = "HQ-WebRip"
	}
	return "/Movie/" + strconv.Itoa(m.ID) + "-" + url.PathEscape(src) + "/" + slug(fmt.Sprintf("%s %d", m.Name, m.Year))
}
func details(ctx context.Context, m Movie, kind, wantedEpisode string) []Release {
	body, e := fetch(ctx, moviePath(m))
	if e != nil {
		return nil
	}
	matches := hrefRX.FindAllStringSubmatch(string(body), 140)
	var out []Release
	seen := map[string]bool{}
	for _, match := range matches {
		p := html.UnescapeString(match[1])
		readable, _ := url.PathUnescape(p)
		ep := seasonRX.FindString(readable)
		if kind == "tvsearch" || (kind == "search" && wantedEpisode != "") {
			if wantedEpisode != "" && !strings.EqualFold(wantedEpisode, ep) {
				continue
			}
			if ep == "" {
				continue
			}
		} else if kind == "movie" && ep != "" {
			continue
		}
		if seen[p] || !strings.HasPrefix(p, "/Movie/") {
			continue
		}
		seen[p] = true
		c, cancel := context.WithTimeout(ctx, 16*time.Second)
		data, e := fetch(c, p)
		cancel()
		if e != nil {
			continue
		}
		mag := magnetRX.FindStringSubmatch(string(data))
		if len(mag) < 2 {
			continue
		}
		link := html.UnescapeString(mag[1])
		u, e := url.Parse(link)
		if e != nil || u.Scheme != "magnet" || !strings.HasPrefix(u.Query().Get("xt"), "urn:btih:") {
			continue
		}
		title := u.Query().Get("dn")
		low := strings.ToLower(strings.TrimSpace(title))
		if strings.HasSuffix(low, ".exe") || strings.HasSuffix(low, ".scr") || strings.HasSuffix(low, ".msi") || strings.HasSuffix(low, ".bat") || strings.HasSuffix(low, ".cmd") || strings.HasSuffix(low, ".ps1") {
			continue
		}
		if title == "" {
			title = m.Name + " (" + strconv.Itoa(m.Year) + ")"
		}
		if kind == "tvsearch" || (kind == "search" && wantedEpisode != "") {
			seriesName := normalize(m.Name)
			releaseTitle := normalize(title)
			if releaseTitle != seriesName && !strings.HasPrefix(releaseTitle, seriesName+" ") {
				continue
			}
		}
		var size int64
		v := sizeRX.FindStringSubmatch(string(data))
		if len(v) > 2 {
			n, _ := strconv.ParseFloat(v[1], 64)
			mult := float64(1000000)
			if strings.HasPrefix(strings.ToLower(v[2]), "g") {
				mult = 1e9
			}
			if strings.Contains(strings.ToLower(v[2]), "ib") {
				mult *= 1.073741824
			}
			size = int64(n * mult)
		}
		cat := 2000
		if kind == "tvsearch" || seasonRX.MatchString(title) {
			cat = 5000
		}
		out = append(out, Release{title, link, size, cat})
		if len(out) >= 6 {
			break
		}
	}
	return out
}
func esc(s string) string { var b strings.Builder; xml.EscapeText(&b, []byte(s)); return b.String() }
func rss(w http.ResponseWriter, rels []Release) {
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	io.WriteString(w, "<?xml version=\"1.0\" encoding=\"utf-8\"?><rss version=\"2.0\" xmlns:torznab=\"http://torznab.com/schemas/2015/feed\"><channel><title>WatchSoMuch Torznab</title><description>Public catalog adapter</description>")
	for _, r := range rels {
		fmt.Fprintf(w, "<item><title>%s</title><guid isPermaLink=\"false\">%s</guid><link>%s</link><pubDate>%s</pubDate><size>%d</size><category>%d</category><enclosure url=\"%s\" length=\"%d\" type=\"application/x-bittorrent\"/><torznab:attr name=\"category\" value=\"%d\"/></item>", esc(r.Title), esc(r.Link), esc(r.Link), time.Now().UTC().Format(time.RFC1123Z), r.Size, r.Category, esc(r.Link), r.Size, r.Category)
	}
	io.WriteString(w, "</channel></rss>")
}
func query(q url.Values) []Movie {
	all := current()
	s := q.Get("q")
	season := seasonRX.MatchString(s)
	if season {
		s = seasonRX.ReplaceAllString(s, "")
	}
	s = yrRX.ReplaceAllString(s, "")
	norm := normalize(s)
	imdb, _ := strconv.Atoi(strings.TrimPrefix(q.Get("imdbid"), "tt"))
	if norm == "" && imdb == 0 {
		// Do not publish arbitrary catalog entries as a misleading RSS feed.
		return nil
	}
	type item struct {
		m     Movie
		score int
	}
	var hits []item
	for _, m := range all {
		if year := yrRX.FindString(q.Get("q")); year != "" && imdb == 0 && strconv.Itoa(m.Year) != year {
			continue
		}
		if imdb != 0 && imdb != m.ID {
			continue
		}
		name := normalize(m.Name)
		noArticle := strings.TrimPrefix(name, "the ")
		if imdb == 0 && !(name == norm || strings.HasPrefix(name, norm+" ") || noArticle == norm || strings.HasPrefix(noArticle, norm+" ")) {
			continue
		}
		score := 0
		if normalize(m.Name) == norm {
			score += 100
		}
		if strings.Contains(q.Get("q"), strconv.Itoa(m.Year)) {
			score += 30
		}
		if season && seasonRX.MatchString(m.Source) {
			score += 25
		}
		hits = append(hits, item{m, score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > 4 {
		hits = hits[:4]
	}
	out := make([]Movie, 0, len(hits))
	for _, x := range hits {
		out = append(out, x.m)
	}
	return out
}
func serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
		return
	}
	if r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if secret != "" && q.Get("apikey") != secret {
		http.Error(w, "unauthorized", 401)
		return
	}
	switch q.Get("t") {
	case "caps":
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, "<?xml version=\"1.0\"?><caps><server title=\"WatchSoMuch\" version=\"0.1\"/><limits max=\"50\" default=\"20\"/><searching><search available=\"yes\" supportedParams=\"q\"/><tv-search available=\"yes\" supportedParams=\"q,imdbid,season,ep\"/><movie-search available=\"yes\" supportedParams=\"q,imdbid\"/></searching><categories><category id=\"2000\" name=\"Movies\"/><category id=\"5000\" name=\"TV\"/></categories></caps>")
	case "search", "tvsearch", "movie":
		if len(current()) == 0 {
			http.Error(w, "catalog initializing", 503)
			return
		}
		var result []Release
		kind := q.Get("t")
		wantedEpisode := strings.ToUpper(seasonRX.FindString(q.Get("q")))
		if kind == "tvsearch" && q.Get("season") != "" && q.Get("ep") != "" {
			seasonNumber, se := strconv.Atoi(q.Get("season"))
			episodeNumber, ee := strconv.Atoi(q.Get("ep"))
			if se == nil && ee == nil {
				wantedEpisode = fmt.Sprintf("S%02dE%02d", seasonNumber, episodeNumber)
			}
		}
		for _, m := range query(q) {
			ctx, cancel := context.WithTimeout(r.Context(), 33*time.Second)
			a := details(ctx, m, kind, wantedEpisode)
			cancel()
			result = append(result, a...)
		}
		rss(w, result)
	default:
		http.Error(w, "unsupported torznab command", 400)
	}
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--health" {
		res, e := httpc.Get("http://127.0.0.1:8787/health")
		if e != nil {
			os.Exit(1)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	loadCache()
	go populate()
	http.HandleFunc("/", serve)
	addr := setting("LISTEN_ADDR", ":8787")
	log.Println("WatchSoMuch Torznab Go service", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
