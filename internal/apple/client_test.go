package apple

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func TestContinuePlayingReadsCloudPosition(t *testing.T) {
	client := NewClient()
	client.SetAuth("fictional-session", "")
	client.httpClient.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		if include := req.URL.Query().Get("include[podcast-episodes]"); !strings.Contains(include, "playback-position") {
			t.Errorf("continue playing page %q did not preserve playback-position: %q", req.URL.RawQuery, include)
		}
		if req.URL.Query().Get("offset") == "1" {
			return jsonResponse(`{"data":[{"id":"second","type":"podcast-episodes","relationships":{
				"playback-position":{"data":[{"id":"second","type":"playback-positions","attributes":{"completed":true}}]}
			}}]}`), nil
		}
		return jsonResponse(`{"data":[{"id":"first","type":"podcast-episodes","relationships":{
			"playback-position":{"data":[{"id":"first","type":"playback-positions"}]}
		}}],"included":[{"id":"first","type":"playback-positions","attributes":{
			"positionInMilliseconds":123456.25,"completed":false,"recordedAtTimestamp":"2026-11-18T03:00:00Z"
		}}],"next":"https://amp-api.podcasts.apple.com/v1/me/continue-playing/podcast-episodes?offset=1"}`), nil
	})
	episodes, err := client.ContinuePlaying(client.AuthContext())
	if err != nil || len(episodes) != 2 {
		t.Fatalf("continue playing returned %d episodes: %v", len(episodes), err)
	}
	wantTime := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	if got := episodes[0].PlaybackPosition; !got.Known || got.PositionMS != 123456.25 || got.Completed || !got.RecordedAt.Equal(wantTime) {
		t.Errorf("linked playback position decoded as %+v", got)
	}
	if got := episodes[1].PlaybackPosition; !got.Known || !got.Completed {
		t.Errorf("inline playback position decoded as %+v", got)
	}
}

func TestRetiredSessionCannotSendNewAccountCredentials(t *testing.T) {
	client := NewClient()
	client.SetAuth("old-session", "")
	old := client.AuthContext()
	client.SetAuth("new-session", "")
	client.httpClient.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		t.Error("a request for the old session reached the network")
		return jsonResponse(`{"data":[]}`), nil
	})
	if _, err := client.LibraryShows(old); !errors.Is(err, context.Canceled) {
		t.Errorf("retired session error = %v, want context.Canceled", err)
	}
}

func TestFormatArtwork(t *testing.T) {
	tpl := "https://is1-ssl.mzstatic.com/image/thumb/Podcasts116/v4/2e/cf/99/cover.png/{w}x{h}bb.{f}"
	got := FormatArtwork(tpl, 300)
	want := "https://is1-ssl.mzstatic.com/image/thumb/Podcasts116/v4/2e/cf/99/cover.png/300x300bb.jpg"
	if got != want {
		t.Fatalf("FormatArtwork() = %q, want %q", got, want)
	}
	if FormatArtwork("", 300) != "" {
		t.Fatal("expected empty string for empty template")
	}

	heroTpl := "https://is1-ssl.mzstatic.com/image/thumb/Features116/v4/b0/55/c5/b055c5bd-b65d-ff6e-ba2c-96c9a4a41d6e/4c22c378-8b73-4573-ad59-919526effa14.png/{w}x{h}{c}.{f}"
	wantHero := "https://is1-ssl.mzstatic.com/image/thumb/Features116/v4/b0/55/c5/b055c5bd-b65d-ff6e-ba2c-96c9a4a41d6e/4c22c378-8b73-4573-ad59-919526effa14.png/450x600bb.webp"
	if gotHero := FormatHeroArtwork(heroTpl); gotHero != wantHero {
		t.Fatalf("FormatHeroArtwork() = %q, want %q", gotHero, wantHero)
	}
}

func TestLiveCatalogAPI(t *testing.T) {
	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	charts, err := client.Charts(ctx, 5)
	if err != nil {
		t.Fatalf("Charts() error: %v", err)
	}
	if len(charts.TopShows) == 0 {
		t.Fatal("Charts() returned 0 TopShows")
	}
	if len(charts.TrendingEpisodes) == 0 {
		t.Fatal("Charts() returned 0 TrendingEpisodes")
	}

	searchRes, err := client.Search(ctx, "ted", 5)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(searchRes.Shows) == 0 || len(searchRes.Episodes) == 0 {
		t.Fatalf("Search() expected non-empty shows (%d) and episodes (%d)", len(searchRes.Shows), len(searchRes.Episodes))
	}

	detail, err := client.GetShow(ctx, "160904630")
	if err != nil {
		t.Fatalf("GetShow() error: %v", err)
	}
	if detail.Show.Name == "" {
		t.Fatal("GetShow() returned empty show name")
	}
	if len(detail.Episodes) == 0 {
		t.Fatal("GetShow() returned 0 episodes")
	}
	first := detail.Episodes[0]
	if first.AssetURL == "" && first.HLSURL == "" {
		t.Fatalf("expected non-empty AssetURL or HLSURL on episode %q", first.Name)
	}
	if first.DurationMS <= 0 {
		t.Fatalf("expected positive DurationMS on episode %q, got %d", first.Name, first.DurationMS)
	}

	epDetail, err := client.GetEpisodeDetail(ctx, "1000801206203")
	if err != nil {
		t.Fatalf("GetEpisodeDetail() error: %v", err)
	}
	if epDetail.HeroArtworkURL == "" {
		t.Fatal("GetEpisodeDetail() returned empty HeroArtworkURL")
	}
	if epDetail.HeroBgColor != "130c09" {
		t.Fatalf("GetEpisodeDetail() HeroBgColor = %q, want %q", epDetail.HeroBgColor, "130c09")
	}
	tr := ParseTranscriptFromText(epDetail.FullDescription, epDetail.Description, epDetail.DurationMS)
	if tr == nil || !tr.HasSyncedTime || len(tr.Lines) == 0 {
		t.Fatalf("ParseTranscriptFromText() failed to extract timestamped lines (lines=%d synced=%v)", len(tr.Lines), tr.HasSyncedTime)
	}
}

func TestPickBestTranscriptTag(t *testing.T) {
	tags := []rssTranscriptTag{
		{URL: "/tr/ep1.html", Type: "text/html"},
		{URL: "/tr/ep1.vtt", Type: "text/vtt"},
		{URL: "/tr/ep1.json", Type: "application/json"},
	}
	u, typ := pickBestTranscriptTag(tags, "https://example.com/feed.xml")
	if u != "https://example.com/tr/ep1.json" || typ != "application/json" {
		t.Fatalf("pickBestTranscriptTag() = (%q, %q), want JSON resolved URL", u, typ)
	}
}

func TestLibraryEndpointsRequestPlaybackPositionsAndHandle404(t *testing.T) {
	client := NewClient()
	client.SetAuth("session-tok", "")
	client.httpClient.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("media-user-token") != "session-tok" {
			t.Errorf("missing media-user-token on %s", req.URL.Path)
		}
		switch req.URL.Path {
		case "/v1/me/library/podcasts":
			if got := req.URL.Query().Get("limit"); got != "30" {
				t.Errorf("LibraryShows limit = %q, want 30", got)
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"errors":[{"code":"40400"}]}`))}, nil
		case "/v1/me/library/podcast-episodes":
			if got := req.URL.Query().Get("limit"); got != "30" {
				t.Errorf("SavedEpisodes limit = %q, want 30", got)
			}
			if inc := req.URL.Query().Get("include[podcast-episodes]"); !strings.Contains(inc, "playback-position") {
				t.Errorf("SavedEpisodes missing playback-position on %q: %q", req.URL.RawQuery, inc)
			}
			if req.URL.Query().Get("offset") == "1" {
				return jsonResponse(`{"data":[{"id":"saved-2","type":"podcast-episodes","relationships":{
					"playback-position":{"data":[{"id":"saved-2","type":"playback-positions","attributes":{"completed":true,"recordedAtTimestamp":"2026-11-19T05:00:00Z"}}]}
				}}]}`), nil
			}
			return jsonResponse(`{"data":[{"id":"saved-1","type":"podcast-episodes","relationships":{
				"playback-position":{"data":[{"id":"saved-1","type":"playback-positions","attributes":{"positionInMilliseconds":90000,"completed":false,"recordedAtTimestamp":"2026-11-19T04:00:00Z"}}]}
			}}],"next":"/v1/me/library/podcast-episodes?offset=1"}`), nil
		case "/v1/me/library/new-episodes":
			if inc := req.URL.Query().Get("include[podcast-episodes]"); !strings.Contains(inc, "playback-position") {
				t.Errorf("NewEpisodes missing playback-position: %q", inc)
			}
			return jsonResponse(`{"data":[{"id":"new-1","type":"podcast-episodes","relationships":{
				"playback-position":{"data":[{"id":"new-1","type":"playback-positions","attributes":{"positionInMilliseconds":45000,"completed":false,"recordedAtTimestamp":"2026-11-19T06:00:00Z"}}]}
			}}]}`), nil
		case "/v1/me/recent/played/podcast-episodes":
			if inc := req.URL.Query().Get("include[podcast-episodes]"); !strings.Contains(inc, "playback-position") {
				t.Errorf("RecentPlayed missing playback-position: %q", inc)
			}
			return jsonResponse(`{"data":[{"id":"rec-1","type":"podcast-episodes","relationships":{
				"playback-position":{"data":[{"id":"rec-1","type":"playback-positions","attributes":{"completed":true,"recordedAtTimestamp":"2026-11-19T07:00:00Z"}}]}
			}}]}`), nil
		case "/v1/me/storefront":
			return jsonResponse(`{"data":[{"id":"cn","type":"storefronts"}]}`), nil
		default:
			t.Fatalf("unexpected request: %s?%s", req.URL.Path, req.URL.RawQuery)
			return nil, nil
		}
	})

	ctx := client.AuthContext()
	shows, err := client.LibraryShows(ctx)
	if err != nil || len(shows) != 0 {
		t.Fatalf("LibraryShows 404 = (%d, %v), want empty nil error", len(shows), err)
	}
	saved, err := client.SavedEpisodes(ctx)
	if err != nil || len(saved) != 2 {
		t.Fatalf("SavedEpisodes = (%d, %v), want 2", len(saved), err)
	}
	if !saved[0].PlaybackPosition.Known || saved[0].PlaybackPosition.PositionMS != 90000 {
		t.Errorf("saved[0] position = %+v", saved[0].PlaybackPosition)
	}
	if !saved[1].PlaybackPosition.Known || !saved[1].PlaybackPosition.Completed {
		t.Errorf("saved[1] page-2 position = %+v", saved[1].PlaybackPosition)
	}
	newEps, err := client.NewEpisodes(ctx)
	if err != nil || len(newEps) != 1 || newEps[0].PlaybackPosition.PositionMS != 45000 {
		t.Fatalf("NewEpisodes = (%+v, %v)", newEps, err)
	}
	recent, err := client.RecentPlayed(ctx)
	if err != nil || len(recent) != 1 || !recent[0].PlaybackPosition.Completed {
		t.Fatalf("RecentPlayed = (%+v, %v)", recent, err)
	}
	sf, err := client.UserStorefront(ctx)
	if err != nil || sf != "cn" || client.AccountStorefront() != "cn" {
		t.Fatalf("UserStorefront = (%q, %v), AccountStorefront = %q", sf, err, client.AccountStorefront())
	}
}

func TestCatalogEndpointsSyncPlaybackPositionsAndFallback(t *testing.T) {
	client := NewClient()
	client.SetStorefront("us")
	client.SetAuth("session-tok", "")
	client.SetAccountStorefront("cn")

	var hydratedShow bool
	client.httpClient.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/v1/catalog/us/podcasts/show-1":
			// Simulate storefront mismatch rejecting personalized catalog call, forcing anon fallback + cn hydration.
			if req.Header.Get("media-user-token") != "" {
				return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{"errors":[{"status":"403"}]}`))}, nil
			}
			return jsonResponse(`{"data":[{"id":"show-1","type":"podcasts","attributes":{"name":"Show 1"},"relationships":{
				"episodes":{"data":[{"id":"ep-show-1","type":"podcast-episodes","attributes":{"name":"Ep 1","durationInMilliseconds":3600000}}],"next":"/v1/catalog/us/podcasts/show-1/episodes?offset=1"}
			}}]}`), nil
		case req.URL.Path == "/v1/catalog/cn/podcast-episodes" && req.URL.Query().Get("ids") == "ep-show-1":
			hydratedShow = true
			if req.Header.Get("media-user-token") != "session-tok" {
				t.Error("PlaybackPositions missing media-user-token")
			}
			return jsonResponse(`{"data":[{"id":"ep-show-1","type":"podcast-episodes","relationships":{
				"playback-position":{"data":[{"id":"ep-show-1","type":"playback-positions","attributes":{"positionInMilliseconds":600000,"completed":false,"recordedAtTimestamp":"2026-11-19T08:00:00Z"}}]}
			}}]}`), nil
		case req.URL.Path == "/v1/catalog/us/podcasts/show-1/episodes":
			if req.Header.Get("media-user-token") != "session-tok" {
				t.Error("GetEpisodesPage should attempt personalized request first")
			}
			if !strings.Contains(req.URL.Query().Get("include[podcast-episodes]"), "playback-position") {
				t.Errorf("GetEpisodesPage missing playback-position: %q", req.URL.RawQuery)
			}
			return jsonResponse(`{"data":[{"id":"ep-show-2","type":"podcast-episodes","attributes":{"name":"Ep 2"},"relationships":{
				"playback-position":{"data":[{"id":"ep-show-2","type":"playback-positions","attributes":{"completed":true,"recordedAtTimestamp":"2026-11-19T08:30:00Z"}}]}
			}}]}`), nil
		case req.URL.Path == "/v1/catalog/us/podcast-episodes/ep-detail-1":
			if !strings.Contains(req.URL.Query().Get("include"), "playback-position") {
				t.Errorf("GetEpisodeDetail missing playback-position: %q", req.URL.RawQuery)
			}
			return jsonResponse(`{"data":[{"id":"ep-detail-1","type":"podcast-episodes","attributes":{"name":"Detail Ep"},"relationships":{
				"playback-position":{"data":[{"id":"ep-detail-1","type":"playback-positions","attributes":{"positionInMilliseconds":300000,"completed":false,"recordedAtTimestamp":"2026-11-19T09:00:00Z"}}]}
			}}]}`), nil
		case req.URL.Path == "/v1/catalog/us/search":
			return jsonResponse(`{"results":{"podcasts":{"data":[]},"podcast-episodes":{"data":[
				{"id":"ep-search-1","type":"podcast-episodes","attributes":{"name":"Search Ep"},"relationships":{
					"playback-position":{"data":[{"id":"ep-search-1","type":"playback-positions"}]}
				}}
			]}},"included":[{"id":"ep-search-1","type":"playback-positions","attributes":{"positionInMilliseconds":180000,"completed":false,"recordedAtTimestamp":"2026-11-19T09:30:00Z"}}]}`), nil
		case req.URL.Path == "/v1/catalog/us/charts":
			return jsonResponse(`{"results":{"podcasts":[{"data":[]}],"podcast-episodes":[{"data":[
				{"id":"ep-chart-1","type":"podcast-episodes","attributes":{"name":"Chart Ep"},"relationships":{
					"playback-position":{"data":[{"id":"ep-chart-1","type":"playback-positions","attributes":{"completed":true,"recordedAtTimestamp":"2026-11-19T10:00:00Z"}}]}
				}}
			]}]}}`), nil
		default:
			t.Fatalf("unexpected request: %s?%s", req.URL.Path, req.URL.RawQuery)
			return nil, nil
		}
	})

	ctx := client.AuthContext()
	showRes, err := client.GetShow(ctx, "show-1")
	if err != nil || len(showRes.Episodes) != 1 || !hydratedShow {
		t.Fatalf("GetShow = (%+v, %v, hydrated=%v)", showRes, err, hydratedShow)
	}
	if pos := showRes.Episodes[0].PlaybackPosition; !pos.Known || pos.PositionMS != 600000 {
		t.Errorf("GetShow hydrated position = %+v, want 600000ms", pos)
	}
	moreEps, _, err := client.GetEpisodesPage(ctx, showRes.NextEpisodesPath, &showRes.Show)
	if err != nil || len(moreEps) != 1 || !moreEps[0].PlaybackPosition.Completed {
		t.Fatalf("GetEpisodesPage = (%+v, %v)", moreEps, err)
	}
	epDetail, err := client.GetEpisodeDetail(ctx, "ep-detail-1")
	if err != nil || epDetail.PlaybackPosition.PositionMS != 300000 {
		t.Fatalf("GetEpisodeDetail = (%+v, %v)", epDetail, err)
	}
	searchRes, err := client.Search(ctx, "test", 10)
	if err != nil || len(searchRes.Episodes) != 1 || searchRes.Episodes[0].PlaybackPosition.PositionMS != 180000 {
		t.Fatalf("Search = (%+v, %v)", searchRes, err)
	}
	chartsRes, err := client.Charts(ctx, 10)
	if err != nil || len(chartsRes.TrendingEpisodes) != 1 || !chartsRes.TrendingEpisodes[0].PlaybackPosition.Completed {
		t.Fatalf("Charts = (%+v, %v)", chartsRes, err)
	}
}
