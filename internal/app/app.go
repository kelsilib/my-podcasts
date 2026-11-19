package app

import (
	"context"
	"math"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"

	"apple-podcasts/internal/apple"
	"apple-podcasts/internal/player"
)

type pageKind int

const (
	pageDiscover pageKind = iota
	pageListenNow
	pageSubscriptions
	pageSaved
	pageSearch
	pageShow
	pageEpisode
)

type route struct {
	page       pageKind
	showID     string
	epID       string
	query      string
	transcript bool
}

type episodeDetailState struct {
	episode    *apple.Episode
	chapters   []apple.TranscriptLine
	transcript *apple.EpisodeTranscript
	fetched    bool
	loading    bool
	err        string
}

type storefrontOption struct {
	code  string
	label string
}

var storefrontOptions = []storefrontOption{
	{code: "us", label: "US 美区"},
	{code: "cn", label: "CN 国区"},
	{code: "jp", label: "JP 日区"},
	{code: "gb", label: "GB 英区"},
}

type showDetailState struct {
	detail      *apple.ShowDetailResult
	loading     bool
	loadingMore bool
	err         string
}

// App coordinates Apple Podcasts state, playback, WebView2 auth, and native UI.
type App struct {
	win     *mygo.Window
	dataDir string
	client  *apple.Client
	player  *player.Player
	state   AppState

	route        route
	backStack    []route
	forwardStack []route
	pageScroll   ui.ScrollState

	searchQuery   string
	searchLast    string
	searchRes     *apple.SearchResult
	searchLoading bool
	searchErr     string
	searchGen     uint64

	charts          *apple.ChartsResult
	chartsLoading   bool
	chartsErr       string
	chartsGen       uint64
	chartShowLatest []apple.Episode
	featuredEps     []apple.Episode

	shows             map[string]*showDetailState
	episodes          map[string]*episodeDetailState
	transcriptScroll  ui.ScrollState
	lastSyncedLine    int
	hideImmersiveText bool
	immRun            uint64

	cloudUpNext          []apple.Episode
	cloudNewEps          []apple.Episode
	cloudRecentPlayed    []apple.Episode
	cloudLoading         bool
	cloudWriting         bool
	cloudErr             string
	cloudPositions       map[string]apple.PlaybackPosition
	lastCloudLibrarySync time.Time

	signingIn bool
	signInWin *mygo.Window

	barScrub scrubState
	trScrub  scrubState
	epMenu   episodeMenuState

	pendingToast     string
	stateMu          sync.Mutex
	lastProgressSave time.Time
	lastCloudSync    time.Time
}

type scrubState struct {
	held   bool
	active bool
	val    float64
}

type episodeMenuState struct {
	hoverID          string
	hoverSince       time.Time
	dismissedHoverID string

	openID     string
	pinned     bool
	leaveSince time.Time

	seenHover bool
	seenOpen  bool
}

// Run starts the Apple Podcasts native Windows desktop application.
func Run() error {
	mygo.App.SetName("Apple Podcasts")
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}

	a := &App{
		shows:          map[string]*showDetailState{},
		episodes:       map[string]*episodeDetailState{},
		route:          route{page: pageDiscover},
		lastSyncedLine: -1,
	}

	mygo.App.WhenReady(func() {
		netimage.SetClient(&http.Client{
			Transport: netproxy.Transport(),
			Timeout:   60 * time.Second,
		})

		dir, _ := mygo.App.Path(mygo.PathUserData)
		a.dataDir = resolveDataDir(dir)
		a.state = loadAppState(a.dataDir)
		creds := loadCredentials(a.dataDir)

		a.client = apple.NewClient()
		a.client.SetStorefront(a.state.Storefront)
		if creds.MediaUserToken != "" {
			a.client.SetAuth(creds.MediaUserToken, creds.CookieHeader)
		}
		a.selectLibrary(creds.MediaUserToken, true)

		a.player = player.New(
			func() { a.update(func() {}) },
			a.handleProgressUpdate,
		)
		a.player.SetInitialPrefs(a.state.Volume, a.state.Speed)
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Apple Podcasts",
			Width:           1180,
			Height:          780,
			MinWidth:        860,
			MinHeight:       560,
			StateKey:        "main",
			TitleBarStyle:   mygo.TitleBarHidden,
			TitleBarHeight:  36,
			Vibrancy:        mygo.VibrancySidebar,
			BackgroundColor: "light-dark(#f6f6f9, #131316)",
			Content:         ui.View(a.view),
		})
		a.win.OnClose(func(*mygo.CloseEvent) {
			a.persistSessionNow()
		})
		_ = a.win.SetIcon(appIconPNG)
		a.restoreSession()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = a.client.RefreshBearerToken(ctx)
		}()

		a.loadCharts()
		if a.client.HasAuth() {
			a.syncCloudLibrary()
		}
	})

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.persistSessionNow()
		if a.player != nil {
			a.player.Stop()
		}
	})

	mygo.App.OnSecondInstance(func([]string, string) {
		if a.win != nil {
			a.win.Show()
			a.win.Focus()
		}
	})

	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})

	return mygo.App.Run()
}

func (a *App) update(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(fn)
}

func (a *App) updateState(change func()) {
	a.stateMu.Lock()
	change()
	a.captureRouteLocked()
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()
}

func (a *App) hasAuth() bool {
	return a.client != nil && a.client.HasAuth()
}

func (a *App) progressMapLocked(signedIn bool) map[string]EpisodeProgress {
	if signedIn && a.state.CloudLibrary != nil {
		if a.state.CloudLibrary.Progress == nil {
			a.state.CloudLibrary.Progress = map[string]EpisodeProgress{}
		}
		return a.state.CloudLibrary.Progress
	}
	if a.state.Progress == nil {
		a.state.Progress = map[string]EpisodeProgress{}
	}
	return a.state.Progress
}

func (a *App) episodeProgress(id string) (EpisodeProgress, bool) {
	progress, ok := a.localEpisodeProgress(id)
	if cloud, newer := a.newerCloudPosition(id, progress, ok); newer {
		return EpisodeProgress{
			PositionSec: max(0, cloud.PositionMS/1000),
			Completed:   cloud.Completed,
			UpdatedAt:   cloud.RecordedAt.Unix(),
		}, true
	}
	return progress, ok
}

func (a *App) localEpisodeProgress(id string) (EpisodeProgress, bool) {
	signedIn := a.hasAuth()
	a.stateMu.Lock()
	progMap := a.progressMapLocked(signedIn)
	progress, ok := progMap[id]
	a.stateMu.Unlock()
	return progress, ok
}

func compactPlaybackQueue(queue []apple.Episode, idx int) ([]apple.Episode, int) {
	if len(queue) == 0 {
		return nil, 0
	}
	if idx < 0 || idx >= len(queue) {
		idx = 0
	}
	const maxWindow = 40
	start := 0
	end := len(queue)
	if len(queue) > maxWindow {
		start = max(0, idx-maxWindow/2)
		end = min(len(queue), start+maxWindow)
		if end-start < maxWindow {
			start = max(0, end-maxWindow)
		}
	}
	out := make([]apple.Episode, 0, end-start)
	for i := start; i < end; i++ {
		item := queue[i]
		if i != idx {
			// Trim bulky show HTML from non-active queue items to keep state.json fast and compact.
			item.FullDescription = ""
		}
		out = append(out, item)
	}
	return out, idx - start
}

func (a *App) capturePlaybackLocked(snap player.Snapshot, signedIn bool) {
	if snap.Volume > 0.01 {
		a.state.Volume = snap.Volume
	}
	if snap.Speed >= 0.5 && snap.Speed <= 4.0 {
		a.state.Speed = snap.Speed
	}
	if snap.Episode == nil || snap.Episode.ID == "" {
		return
	}
	ep := *snap.Episode
	posSec := snap.Position.Seconds()
	durSec := snap.Duration.Seconds()
	if durSec <= 0 && ep.DurationMS > 0 {
		durSec = float64(ep.DurationMS) / 1000.0
	}
	queue, qIdx := compactPlaybackQueue(snap.Queue, snap.QueueIndex)
	now := time.Now().Unix()

	completed := false
	progMap := a.progressMapLocked(signedIn)
	prev, prevExists := progMap[ep.ID]
	if prevExists {
		completed = prev.Completed
		if completed && posSec > 1 && durSec > 0 && posSec < durSec-10 {
			completed = false
		}
	}
	shouldRecordProgress := posSec >= 1 || completed
	if shouldRecordProgress {
		updatedAt := now
		if !snap.Playing {
			if prevExists && math.Abs(prev.PositionSec-posSec) < 1 && prev.Completed == completed {
				// Keep the previous timestamp for an unchanged paused episode so we never
				// mask a newer iCloud position fetched right after startup.
				shouldRecordProgress = false
			} else if !prevExists && a.state.Playback != nil && a.state.Playback.Episode.ID == ep.ID && a.state.Playback.UpdatedAt > 0 {
				updatedAt = a.state.Playback.UpdatedAt
			}
		}
		if shouldRecordProgress {
			progMap[ep.ID] = EpisodeProgress{
				PositionSec: posSec,
				DurationSec: durSec,
				Completed:   completed,
				UpdatedAt:   updatedAt,
			}
		}
	}
	pbUpdatedAt := now
	if !snap.Playing && a.state.Playback != nil && a.state.Playback.Episode.ID == ep.ID &&
		math.Abs(a.state.Playback.PositionSec-posSec) < 1 && a.state.Playback.UpdatedAt > 0 {
		pbUpdatedAt = a.state.Playback.UpdatedAt
	}
	a.state.Playback = &SavedPlayback{
		Episode:     ep,
		Queue:       queue,
		QueueIndex:  qIdx,
		PositionSec: posSec,
		DurationSec: durSec,
		WasPlaying:  snap.Playing,
		UpdatedAt:   pbUpdatedAt,
	}
}

func (a *App) captureRouteLocked() {
	r := a.route
	if r.page == pageSearch {
		r = route{page: pageDiscover}
	}
	a.state.LastRoute = &SavedRoute{
		Page:       r.page,
		ShowID:     r.showID,
		EpID:       r.epID,
		Transcript: r.transcript,
	}
}

func (a *App) persistSessionNow() {
	var snap player.Snapshot
	if a.player != nil {
		snap = a.player.Snapshot()
	}
	signedIn := a.hasAuth()
	a.stateMu.Lock()
	a.capturePlaybackLocked(snap, signedIn)
	a.captureRouteLocked()
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()
}

func (a *App) restoreSession() {
	saved := a.savedEpisodes()
	recent := a.recentEpisodes()
	signedIn := a.hasAuth()
	a.stateMu.Lock()
	pb := a.state.Playback
	lastRoute := a.state.LastRoute
	var progress EpisodeProgress
	var hasLocal bool
	if pb != nil {
		progress, hasLocal = a.progressMapLocked(signedIn)[pb.Episode.ID]
	}
	a.stateMu.Unlock()

	if pb != nil && pb.Episode.ID != "" {
		posSec := pb.PositionSec
		if hasLocal {
			if progress.Completed {
				posSec = 0
			} else if progress.UpdatedAt > pb.UpdatedAt && progress.PositionSec > 0 {
				posSec = progress.PositionSec
			}
		}
		if cloud, newer := a.newerCloudPosition(pb.Episode.ID, progress, hasLocal); newer {
			if cloud.RecordedAt.Unix() > pb.UpdatedAt || !hasLocal {
				if cloud.Completed {
					posSec = 0
				} else if cloud.PositionMS > 0 {
					posSec = cloud.PositionMS / 1000.0
				}
			}
		}
		epCp := pb.Episode
		a.episodes[epCp.ID] = &episodeDetailState{
			episode:  &epCp,
			chapters: extractChapters(epCp),
		}
		if a.player != nil {
			a.player.Restore(pb.Episode, pb.Queue, time.Duration(posSec*float64(time.Second)))
		}
	}

	if lastRoute == nil {
		return
	}
	switch lastRoute.Page {
	case pageDiscover, pageListenNow, pageSubscriptions, pageSaved:
		a.route = route{page: lastRoute.Page}
	case pageShow:
		if lastRoute.ShowID != "" {
			a.route = route{page: pageShow, showID: lastRoute.ShowID}
			if a.client != nil {
				a.ensureShowLoaded(lastRoute.ShowID)
			}
		}
	case pageEpisode:
		if lastRoute.EpID != "" {
			a.route = route{
				page:       pageEpisode,
				epID:       lastRoute.EpID,
				showID:     lastRoute.ShowID,
				transcript: lastRoute.Transcript,
			}
			ep := apple.Episode{ID: lastRoute.EpID, ShowID: lastRoute.ShowID}
			if st := a.episodes[lastRoute.EpID]; st != nil && st.episode != nil {
				ep = *st.episode
			} else {
				for _, list := range [][]apple.Episode{recent, saved} {
					for _, candidate := range list {
						if candidate.ID == lastRoute.EpID {
							ep = candidate
							break
						}
					}
				}
			}
			if a.client != nil {
				a.ensureEpisodeLoaded(ep)
			}
		}
	}
}

func (a *App) navigate(r route) {
	if a.route == r {
		return
	}
	a.backStack = append(a.backStack, a.route)
	if len(a.backStack) > 32 {
		a.backStack = a.backStack[len(a.backStack)-32:]
	}
	a.forwardStack = nil
	a.route = r
	a.pageScroll.Y = 0
}

func (a *App) goBack() {
	if len(a.backStack) == 0 {
		return
	}
	a.forwardStack = append(a.forwardStack, a.route)
	last := a.backStack[len(a.backStack)-1]
	a.backStack = a.backStack[:len(a.backStack)-1]
	a.route = last
	a.pageScroll.Y = 0
	if last.page == pageEpisode && last.transcript {
		a.immRun++
	}
}

func (a *App) goForward() {
	if len(a.forwardStack) == 0 {
		return
	}
	a.backStack = append(a.backStack, a.route)
	next := a.forwardStack[len(a.forwardStack)-1]
	a.forwardStack = a.forwardStack[:len(a.forwardStack)-1]
	a.route = next
	a.pageScroll.Y = 0
	if next.page == pageEpisode && next.transcript {
		a.immRun++
	}
}

func (a *App) cycleStorefront() {
	cur := a.state.Storefront
	for i, opt := range storefrontOptions {
		if opt.code == cur {
			next := storefrontOptions[(i+1)%len(storefrontOptions)]
			a.setStorefront(next.code)
			return
		}
	}
	a.setStorefront(storefrontOptions[0].code)
}

func (a *App) openShow(showID string) {
	if showID == "" {
		return
	}
	a.navigate(route{page: pageShow, showID: showID})
	if st := a.shows[showID]; st != nil && st.detail != nil {
		a.refreshActivePagePositions()
		return
	}
	a.ensureShowLoaded(showID)
}

func (a *App) openEpisode(ep apple.Episode, immersive bool) {
	if ep.ID == "" {
		return
	}
	a.lastSyncedLine = -1
	a.transcriptScroll.Y = 0
	a.hideImmersiveText = false
	if immersive && !(a.route.page == pageEpisode && a.route.epID == ep.ID && a.route.transcript) {
		a.immRun++
	}
	if a.route.page == pageEpisode && a.route.epID == ep.ID {
		a.route.transcript = immersive
		a.ensureEpisodeLoaded(ep)
		if st := a.episodes[ep.ID]; st != nil && st.fetched && !st.loading {
			a.refreshActivePagePositions()
		}
		return
	}
	a.navigate(route{
		page:       pageEpisode,
		epID:       ep.ID,
		showID:     ep.ShowID,
		transcript: immersive,
	})
	a.ensureEpisodeLoaded(ep)
	if st := a.episodes[ep.ID]; st != nil && st.fetched && !st.loading {
		a.refreshActivePagePositions()
	}
}

func (a *App) setTranscriptMode(on bool) {
	if a.route.page != pageEpisode {
		return
	}
	if on && !a.route.transcript {
		a.immRun++
	}
	a.route.transcript = on
	a.lastSyncedLine = -1
}

func extractChapters(ep apple.Episode) []apple.TranscriptLine {
	parsed := apple.ParseTranscriptFromText(ep.FullDescription, ep.Description, ep.DurationMS)
	if parsed == nil || !parsed.HasSyncedTime {
		return nil
	}
	var out []apple.TranscriptLine
	for _, ln := range parsed.Lines {
		if ln.HasTimestamp {
			out = append(out, ln)
		}
	}
	return out
}

func (a *App) ensureEpisodeLoaded(ep apple.Episode) {
	if ep.ID == "" {
		return
	}
	st := a.episodes[ep.ID]
	if st == nil {
		cp := ep
		st = &episodeDetailState{
			episode:  &cp,
			chapters: extractChapters(cp),
		}
		a.episodes[ep.ID] = st
	}
	if st.loading || (st.fetched && st.episode != nil && st.episode.FullDescription != "") {
		return
	}
	st.loading = true
	st.err = ""
	fallback := ep
	session := a.client.AuthContext()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		detailed, err := a.client.GetEpisodeDetail(ctx, fallback.ID)
		merged := fallback
		if err == nil && detailed != nil {
			merged = mergeEpisodeDetail(*detailed, fallback)
		}
		tr, _ := a.client.FetchEpisodeTranscript(ctx, merged)
		chapters := extractChapters(merged)

		if a.player != nil {
			a.player.UpdateCurrentEpisode(merged)
		}

		a.update(func() {
			cur := a.episodes[fallback.ID]
			if cur == nil {
				return
			}
			cur.loading = false
			cur.fetched = true
			if err != nil && cur.episode == nil {
				cur.err = err.Error()
				return
			}
			cur.episode = &merged
			cur.chapters = chapters
			if tr != nil && tr.HasTranscript() {
				cur.transcript = tr
			}
			if session != nil && session.Err() == nil && merged.PlaybackPosition.Known {
				a.mergeCloudPositions([]apple.Episode{merged})
			}
		})
	}()
}

func mergeEpisodeDetail(primary, fallback apple.Episode) apple.Episode {
	if primary.ShowID == "" {
		primary.ShowID = fallback.ShowID
	}
	if primary.ShowName == "" {
		primary.ShowName = fallback.ShowName
	}
	if primary.AssetURL == "" {
		primary.AssetURL = fallback.AssetURL
	}
	if primary.HLSURL == "" {
		primary.HLSURL = fallback.HLSURL
	}
	if primary.ArtworkURL == "" {
		primary.ArtworkURL = fallback.ArtworkURL
	}
	if primary.ShowArtworkURL == "" {
		primary.ShowArtworkURL = fallback.ShowArtworkURL
	}
	if primary.HeroArtworkURL == "" {
		primary.HeroArtworkURL = fallback.HeroArtworkURL
		primary.HeroBgColor = fallback.HeroBgColor
	}
	if primary.DurationMS == 0 {
		primary.DurationMS = fallback.DurationMS
	}
	if !primary.PlaybackPosition.Known && fallback.PlaybackPosition.Known {
		primary.PlaybackPosition = fallback.PlaybackPosition
	}
	return primary
}

func (a *App) setStorefront(code string) {
	if code == "" || code == a.state.Storefront {
		return
	}
	a.stateMu.Lock()
	a.state.Storefront = code
	a.captureRouteLocked()
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()
	a.client.SetStorefront(code)
	a.shows = map[string]*showDetailState{}
	a.episodes = map[string]*episodeDetailState{}
	a.charts = nil
	a.chartShowLatest = nil
	a.featuredEps = nil
	a.loadCharts()
	if a.route.page == pageSearch && strings.TrimSpace(a.searchLast) != "" {
		a.runSearch(a.searchLast)
	}
}

func (a *App) loadCharts() {
	a.chartsGen++
	gen := a.chartsGen
	a.chartsLoading = true
	a.chartsErr = ""

	var candidateShowIDs []string
	seenShow := map[string]bool{}
	addShowID := func(id string) {
		if id != "" && !seenShow[id] && len(candidateShowIDs) < 6 {
			seenShow[id] = true
			candidateShowIDs = append(candidateShowIDs, id)
		}
	}
	for _, s := range a.subscriptions() {
		addShowID(s.ID)
	}
	for _, ep := range a.recentEpisodes() {
		addShowID(ep.ShowID)
	}
	session := a.client.AuthContext()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		var (
			res        *apple.ChartsResult
			err        error
			showLatest = make([]apple.Episode, len(candidateShowIDs))
			wg         sync.WaitGroup
		)

		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err = a.client.Charts(ctx, 24)
		}()

		for i, sid := range candidateShowIDs {
			wg.Add(1)
			go func(idx int, id string) {
				defer wg.Done()
				if d, derr := a.client.GetShow(ctx, id); derr == nil && len(d.Episodes) > 0 {
					showLatest[idx] = d.Episodes[0]
				}
			}(i, sid)
		}
		wg.Wait()

		a.update(func() {
			if a.chartsGen != gen {
				return
			}
			a.chartsLoading = false
			if err != nil {
				a.chartsErr = err.Error()
				return
			}
			a.charts = res
			var extra []apple.Episode
			for _, ep := range showLatest {
				if ep.ID != "" {
					extra = append(extra, ep)
				}
			}
			a.chartShowLatest = extra
			a.updateFeaturedEpisodes()
			if session != nil && session.Err() == nil {
				a.mergeCloudPositions(res.TrendingEpisodes)
				a.mergeCloudPositions(extra)
			}
		})
	}()
}

func buildFeaturedEpisodes(cloudNew, userLatest, trending []apple.Episode, maxCount int) []apple.Episode {
	seenEp := map[string]bool{}
	seenShow := map[string]bool{}
	var out []apple.Episode

	tryAdd := func(ep apple.Episode, uniqueShow bool) {
		if ep.ID == "" || seenEp[ep.ID] || len(out) >= maxCount {
			return
		}
		if uniqueShow && ep.ShowID != "" && seenShow[ep.ShowID] {
			return
		}
		seenEp[ep.ID] = true
		if ep.ShowID != "" {
			seenShow[ep.ShowID] = true
		}
		out = append(out, ep)
	}

	for _, ep := range cloudNew {
		tryAdd(ep, true)
	}
	for _, ep := range userLatest {
		tryAdd(ep, true)
	}
	// Prioritize trending episodes that carry a special cover or episode artwork first.
	for _, ep := range trending {
		if ep.HeroArtworkURL != "" || ep.ArtworkOrigin == "episode" {
			tryAdd(ep, true)
		}
	}
	for _, ep := range trending {
		tryAdd(ep, true)
	}
	for _, ep := range trending {
		tryAdd(ep, false)
	}
	return out
}

func (a *App) runSearch(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	a.searchLast = query
	a.navigate(route{page: pageSearch, query: query})
	a.searchGen++
	gen := a.searchGen
	a.searchLoading = true
	a.searchErr = ""
	session := a.client.AuthContext()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := a.client.Search(ctx, query, 24)
		a.update(func() {
			if a.searchGen != gen {
				return
			}
			a.searchLoading = false
			if err != nil {
				a.searchErr = err.Error()
				return
			}
			a.searchRes = res
			if session != nil && session.Err() == nil && res != nil {
				a.mergeCloudPositions(res.Episodes)
			}
		})
	}()
}

func (a *App) ensureShowLoaded(showID string) {
	st := a.shows[showID]
	if st == nil {
		st = &showDetailState{}
		a.shows[showID] = st
	}
	if st.detail != nil || st.loading {
		return
	}
	st.loading = true
	st.err = ""
	session := a.client.AuthContext()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		detail, err := a.client.GetShow(ctx, showID)
		a.update(func() {
			cur := a.shows[showID]
			if cur == nil {
				return
			}
			cur.loading = false
			if err != nil {
				cur.err = err.Error()
				return
			}
			cur.detail = detail
			if session != nil && session.Err() == nil && detail != nil {
				a.mergeCloudPositions(detail.Episodes)
			}
		})
	}()
}

func (a *App) loadMoreShowEpisodes(showID string) {
	st := a.shows[showID]
	if st == nil || st.detail == nil || st.loadingMore || st.detail.NextEpisodesPath == "" {
		return
	}
	st.loadingMore = true
	nextPath := st.detail.NextEpisodesPath
	showCopy := st.detail.Show
	session := a.client.AuthContext()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		eps, next, err := a.client.GetEpisodesPage(ctx, nextPath, &showCopy)
		a.update(func() {
			cur := a.shows[showID]
			if cur == nil || cur.detail == nil {
				return
			}
			cur.loadingMore = false
			if err != nil {
				a.pendingToast = "加载更多单集失败: " + err.Error()
				return
			}
			cur.detail.Episodes = append(cur.detail.Episodes, eps...)
			cur.detail.NextEpisodesPath = next
			if session != nil && session.Err() == nil {
				a.mergeCloudPositions(eps)
			}
		})
	}()
}

func (a *App) recordRecentEpisode(ep apple.Episode) {
	signedIn := a.hasAuth()
	a.stateMu.Lock()
	if signedIn && a.state.CloudLibrary != nil {
		list := append([]apple.Episode{ep}, slices.DeleteFunc(slices.Clone(a.state.CloudLibrary.RecentPlayed), func(e apple.Episode) bool {
			return e.ID == ep.ID
		})...)
		if len(list) > 50 {
			list = list[:50]
		}
		a.state.CloudLibrary.RecentPlayed = list
		a.cloudRecentPlayed = slices.Clone(list)
	} else {
		a.state.RecentEpisodes = append([]apple.Episode{ep}, slices.DeleteFunc(slices.Clone(a.state.RecentEpisodes), func(e apple.Episode) bool {
			return e.ID == ep.ID
		})...)
		if len(a.state.RecentEpisodes) > 50 {
			a.state.RecentEpisodes = a.state.RecentEpisodes[:50]
		}
	}
	a.captureRouteLocked()
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()
}

func (a *App) playEpisode(ep apple.Episode, queue []apple.Episode) {
	var startPos time.Duration
	if prog, ok := a.episodeProgress(ep.ID); ok && !prog.Completed && prog.PositionSec > 0 {
		durSec := float64(ep.DurationMS) / 1000.0
		if durSec == 0 || prog.PositionSec < durSec {
			startPos = time.Duration(prog.PositionSec * float64(time.Second))
		}
	}
	a.recordRecentEpisode(ep)
	a.player.PlayEpisode(ep, queue, startPos)
	a.persistSessionNow()
}

func (a *App) playEpisodeAt(ep apple.Episode, queue []apple.Episode, startSec float64) {
	pos := time.Duration(max(0.0, startSec) * float64(time.Second))
	snap := a.player.Snapshot()
	if snap.Episode != nil && snap.Episode.ID == ep.ID && snap.Err == "" {
		a.player.Seek(pos)
		if !snap.Playing {
			a.player.Play()
		}
		a.persistSessionNow()
		return
	}
	a.recordRecentEpisode(ep)
	a.player.PlayEpisode(ep, queue, pos)
	a.persistSessionNow()
}

func (a *App) enqueueEpisodeNext(ep apple.Episode) {
	if ep.ID == "" || a.player == nil {
		return
	}
	snap := a.player.Snapshot()
	if snap.Episode == nil {
		a.playEpisode(ep, []apple.Episode{ep})
		return
	}
	a.player.EnqueueNext(ep)
	a.persistSessionNow()
	a.pendingToast = "已插播，将作为下一集播放"
}

func (a *App) enqueueEpisodeLast(ep apple.Episode) {
	if ep.ID == "" || a.player == nil {
		return
	}
	snap := a.player.Snapshot()
	if snap.Episode == nil {
		a.playEpisode(ep, []apple.Episode{ep})
		return
	}
	a.player.EnqueueLast(ep)
	a.persistSessionNow()
	a.pendingToast = "已添加到播放队列"
}

func (a *App) setEpisodePlayed(ep apple.Episode, completed bool) {
	if ep.ID == "" {
		return
	}
	a.handleProgressUpdate(ep, 0, completed)
	if a.player != nil {
		snap := a.player.Snapshot()
		if snap.Episode != nil && snap.Episode.ID == ep.ID && !snap.Playing {
			a.player.ApplyResumePosition(ep.ID, 0)
		}
	}
	if completed {
		a.pendingToast = "已标记为已播完"
	} else {
		a.pendingToast = "已标记为未播"
	}
}

func (a *App) copyEpisodeLink(ep apple.Episode) {
	link := strings.TrimSpace(ep.URL)
	if link == "" {
		link = strings.TrimSpace(ep.WebsiteURL)
	}
	if link == "" && ep.ID != "" {
		sf := "us"
		if a.client != nil && a.client.Storefront() != "" {
			sf = a.client.Storefront()
		}
		if ep.ShowID != "" {
			link = "https://podcasts.apple.com/" + sf + "/podcast/id" + ep.ShowID + "?i=" + ep.ID
		} else {
			link = "https://podcasts.apple.com/" + sf + "/podcast-episode/id" + ep.ID
		}
	}
	if link == "" {
		return
	}
	if a.win != nil {
		mygo.Clipboard.WriteText(link)
	}
	a.pendingToast = "已复制单集链接"
}

func (a *App) handleProgressUpdate(ep apple.Episode, posSec float64, completed bool) {
	recordedAt := time.Now()
	var session context.Context
	if a.client != nil {
		session = a.client.AuthContext()
	}
	durSec := float64(ep.DurationMS) / 1000.0
	var snap player.Snapshot
	if a.player != nil {
		snap = a.player.Snapshot()
	}

	signedIn := session != nil
	a.stateMu.Lock()
	progMap := a.progressMapLocked(signedIn)
	now := recordedAt.Unix()
	progMap[ep.ID] = EpisodeProgress{
		PositionSec: posSec,
		DurationSec: durSec,
		Completed:   completed,
		UpdatedAt:   now,
	}
	if snap.Episode != nil && snap.Episode.ID == ep.ID {
		queue, qIdx := compactPlaybackQueue(snap.Queue, snap.QueueIndex)
		a.state.Playback = &SavedPlayback{
			Episode:     ep,
			Queue:       queue,
			QueueIndex:  qIdx,
			PositionSec: posSec,
			DurationSec: durSec,
			WasPlaying:  snap.Playing,
			UpdatedAt:   now,
		}
	}
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()

	a.update(func() {})

	if session != nil {
		go func() {
			ctx, cancel := context.WithTimeout(session, 10*time.Second)
			defer cancel()
			_ = a.client.UpdatePlaybackPosition(ctx, ep.ID, posSec, completed, recordedAt)
		}()
	}
}

func (a *App) maybeSyncPeriodicProgress() {
	if time.Since(a.lastProgressSave) < 3*time.Second {
		return
	}
	signedIn := a.hasAuth()
	a.stateMu.Lock()
	recordedAt := time.Now()
	a.lastProgressSave = recordedAt
	snap := a.player.Snapshot()
	posSec := snap.Position.Seconds()
	if !snap.Playing || snap.Episode == nil || posSec < 1 {
		a.stateMu.Unlock()
		return
	}
	a.capturePlaybackLocked(snap, signedIn)
	a.captureRouteLocked()
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()

	if a.client != nil && time.Since(a.lastCloudSync) >= 15*time.Second {
		session := a.client.AuthContext()
		if session == nil {
			return
		}
		a.lastCloudSync = recordedAt
		epID := snap.Episode.ID
		go func() {
			ctx, cancel := context.WithTimeout(session, 10*time.Second)
			defer cancel()
			_ = a.client.UpdatePlaybackPosition(ctx, epID, posSec, false, recordedAt)
		}()
	}
}

func mergeEpisodes(primary, secondary []apple.Episode) []apple.Episode {
	seen := make(map[string]bool, len(primary))
	out := make([]apple.Episode, 0, len(primary)+len(secondary))
	for _, e := range primary {
		seen[e.ID] = true
		out = append(out, e)
	}
	for _, e := range secondary {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out
}
