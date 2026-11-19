package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"apple-podcasts/internal/apple"
	"apple-podcasts/internal/player"
)

func TestPlaybackProgressCanUpdateDuringEpisodeRender(t *testing.T) {
	a := newTestApp()
	a.state = defaultAppState()
	ep := apple.Episode{ID: "progress", Name: "Episode", DurationMS: 100000}
	uiTest := ui.NewTester(func(c *ui.Context) {
		a.episodeRow(c, paletteFor(c.Theme()), ep, nil, player.Snapshot{})
	}, 900, 120)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 10000 {
			changed := ep
			changed.ID = strconv.Itoa(i)
			a.handleProgressUpdate(changed, 100, true)
		}
	}()
	for range 200 {
		uiTest.Frame()
	}
	<-done
}

func newTestApp() *App {
	return &App{
		client:         apple.NewClient(),
		player:         player.New(nil, nil),
		shows:          map[string]*showDetailState{},
		episodes:       map[string]*episodeDetailState{},
		lastSyncedLine: -1,
		state: AppState{
			Volume: 0.8,
			Speed:  1.0,
		},
	}
}

func TestEpisodeHeroCardShowsTextOverSpecialHero(t *testing.T) {
	a := newTestApp()
	ep := apple.Episode{
		ID:               "1000801206203",
		ShowID:           "1615939013",
		ShowName:         "半拿铁 | 商业沉浮录",
		Name:             "No.226 文曲星的三十年往事",
		Description:      "主播 | 刘飞 潇磊 在很多 80 后、90 后的记忆里，有一种神奇的电子产品。",
		ShortDescription: "主播 | 刘飞 潇磊 在很多 80 后、90 后的记忆里，有一种神奇的电子产品。",
		ArtworkURL:       "https://is1-ssl.mzstatic.com/image/thumb/Podcasts112/v4/95/33/4d/95334de1-1492-1ab0-aa7b-8f246bf89bde/mza_10728614257690800956.jpeg/{w}x{h}bb.{f}",
		BgColor:          "66574e",
		HeroArtworkURL:   "https://is1-ssl.mzstatic.com/image/thumb/Features116/v4/b0/55/c5/b055c5bd-b65d-ff6e-ba2c-96c9a4a41d6e/4c22c378-8b73-4573-ad59-919526effa14.png/{w}x{h}{c}.{f}",
		HeroBgColor:      "130c09",
		DurationMS:       6367000,
		ReleaseDate:      "2026-11-11T12:00:00Z",
	}

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.Hex("#ffffff")).Padding(10).Children(func() {
			a.episodeHeroCard(c.Key("hero"), ep, []apple.Episode{ep}, player.Snapshot{Speed: 1.0, Volume: 0.8})
		})
	}, 240, 290)
	tt.Frame()

	titleRect, ok := tt.Find("No.226 文曲星的三十年往事")
	if !ok {
		t.Fatalf("hero card missing title; texts=%v", tt.Texts())
	}
	if _, ok := tt.Find("播放 No.226 文曲星的三十年往事"); !ok {
		t.Fatalf("hero card missing play pill button")
	}

	img := tt.Image()
	bright := 0
	for y := int(titleRect.Y); y < int(titleRect.Y+titleRect.H); y++ {
		for x := int(titleRect.X); x < int(titleRect.X+titleRect.W); x++ {
			px := img.RGBAAt(x, y)
			if px.R > 200 && px.G > 200 && px.B > 200 {
				bright++
			}
		}
	}
	if bright == 0 {
		t.Fatalf("title rect %+v has 0 bright foreground pixels — covered by background/gradient!", titleRect)
	}
}

func TestImmersiveViewTranscriptAndPlayerBarBehavior(t *testing.T) {
	ep := apple.Episode{
		ID:              "ep-1",
		ShowID:          "show-1",
		ShowName:        "半拿铁 | 商业沉浮录",
		Name:            "No.226 文曲星的三十年往事",
		Description:     "这是普通的单集简介文本，不应充当逐字文稿。",
		FullDescription: "<p>这是普通的单集简介文本，不应充当逐字文稿。</p>",
		BgColor:         "66574e",
		HeroBgColor:     "130c09",
		DurationMS:      3600 * 1000,
	}

	// Case 1: Episode without real transcript -> hides player bar, enlarges play button, centers standalone cover, NO description fake lyrics
	a := newTestApp()
	a.episodes[ep.ID] = &episodeDetailState{episode: &ep}
	a.route = route{page: pageEpisode, epID: ep.ID, showID: ep.ShowID, transcript: true}

	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()

	if _, hasSlider := tt.Find("音量"); hasSlider {
		t.Fatal("bottom player bar volume slider should be hidden when in immersive view")
	}
	if tt.HasText("这是普通的单集简介文本，不应充当逐字文稿。") {
		t.Fatal("immersive view should NOT display episode description as fake transcript when no real transcript exists")
	}
	playRect, ok := tt.Find("播放/暂停")
	if !ok {
		t.Fatal("immersive view missing play/pause control")
	}
	if playRect.W < 56 || playRect.H < 56 {
		t.Fatalf("immersive play/pause control too small: %.0fx%.0f, want >= 56x56", playRect.W, playRect.H)
	}
	if playRect.X < 400 {
		t.Fatalf("standalone cover controls should be centered horizontally, got playRect.X=%.1f", playRect.X)
	}

	// Case 2: Episode WITH real transcript -> renders two-column layout with real transcript on the right
	a.episodes[ep.ID].transcript = &apple.EpisodeTranscript{
		Source:        "podcast:transcript",
		HasSyncedTime: true,
		Lines: []apple.TranscriptLine{
			{StartSec: 0, EndSec: 12, HasTimestamp: true, Text: "第一句正式逐字文稿"},
			{StartSec: 12, EndSec: 25, HasTimestamp: true, Text: "第二句正式逐字文稿"},
		},
	}
	tt.Frame()

	trRect, ok := tt.Find("第一句正式逐字文稿")
	if !ok {
		t.Fatal("real transcript line should be displayed when Source == podcast:transcript")
	}
	if trRect.X < 500 {
		t.Fatalf("expected right-hand transcript column (X >= 500), got X=%.1f", trRect.X)
	}
	leftPlayRect, ok := tt.Find("播放/暂停")
	if !ok || leftPlayRect.X >= trRect.X {
		t.Fatalf("expected left player controls before right transcript column")
	}
}

func TestImmersiveViewOpenCloseAnimationChoreography(t *testing.T) {
	ep := apple.Episode{
		ID:              "ep-anim",
		ShowID:          "show-anim",
		ShowName:        "半拿铁 | 商业沉浮录",
		Name:            "动画编排测试单集",
		Description:     "单集简介说明。",
		FullDescription: "<p>单集简介说明。</p>",
		BgColor:         "66574e",
		DurationMS:      1800 * 1000,
	}

	a := newTestApp()
	a.episodes[ep.ID] = &episodeDetailState{episode: &ep}
	a.route = route{page: pageEpisode, epID: ep.ID, showID: ep.ShowID, transcript: false}

	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()

	if _, ok := tt.Find("音量"); !ok {
		t.Fatal("expected bottom player bar visible on episode detail page")
	}

	// Open immersive view from episode detail page
	if err := tt.Click("沉浸播放视图"); err != nil {
		t.Fatalf("failed to click 沉浸播放视图: %v", err)
	}
	if a.immRun == 0 || !a.route.transcript {
		t.Fatalf("expected immersive route + incremented immRun, got route=%+v immRun=%d", a.route, a.immRun)
	}

	// Mid-flight: stage, player bar exit, timeline stagger, and hero flight should be animating
	tt.Advance(90 * time.Millisecond)
	if !tt.FramePending() {
		t.Fatal("expected active animation frames mid-transition when opening immersive view")
	}
	if _, ok := tt.Find("音量"); ok {
		t.Fatal("expected bottom player bar hidden from interactive tree mid-exit")
	}

	// Settle
	tt.Advance(900 * time.Millisecond)
	if tt.FramePending() {
		t.Fatal("expected open immersive animation to settle after 900ms")
	}
	centerPlay, ok := tt.Find("播放/暂停")
	if !ok || centerPlay.X < 400 || centerPlay.W < 56 {
		t.Fatalf("expected settled centered immersive play control, got %+v", centerPlay)
	}

	// Toggle into split transcript layout and verify animated shift
	a.episodes[ep.ID].transcript = &apple.EpisodeTranscript{
		Source:        "podcast:transcript",
		HasSyncedTime: true,
		Lines: []apple.TranscriptLine{
			{StartSec: 0, EndSec: 10, HasTimestamp: true, Text: "动画分栏文稿首句"},
		},
	}
	tt.Frame()
	tt.Advance(80 * time.Millisecond)
	if !tt.FramePending() {
		t.Fatal("expected active animation frames when expanding transcript split view")
	}
	tt.Advance(900 * time.Millisecond)
	if tt.FramePending() {
		t.Fatal("expected split transcript transition to settle")
	}
	splitPlay, ok := tt.Find("播放/暂停")
	if !ok || splitPlay.X >= centerPlay.X-100 {
		t.Fatalf("expected player controls to glide leftward into split column: center=%.1f split=%.1f", centerPlay.X, splitPlay.X)
	}

	// Return back to detail page and confirm close transition settles with bar restored
	if err := tt.Click("返回详情"); err != nil {
		t.Fatalf("failed to click 返回详情: %v", err)
	}
	tt.Advance(80 * time.Millisecond)
	if !tt.FramePending() {
		t.Fatal("expected active close animation frames when returning from immersive view")
	}
	tt.Advance(900 * time.Millisecond)
	if tt.FramePending() {
		t.Fatal("expected return transition to settle")
	}
	if _, ok := tt.Find("音量"); !ok {
		t.Fatal("expected bottom player bar restored after exiting immersive view")
	}
}

func TestPlaybackSessionPersistenceAndRestore(t *testing.T) {
	dir := t.TempDir()
	queue := []apple.Episode{
		{ID: "ep-1", ShowID: "show-1", ShowName: "测试播客", Name: "记忆测试单集 1", DurationMS: 1800 * 1000},
		{ID: "ep-2", ShowID: "show-1", ShowName: "测试播客", Name: "记忆测试单集 2", DurationMS: 3600 * 1000, Description: "测试简介"},
		{ID: "ep-3", ShowID: "show-1", ShowName: "测试播客", Name: "记忆测试单集 3", DurationMS: 2400 * 1000},
	}

	// Session 1: restore active paused playback at 2:05 (125s), 1.5x speed, 0.65 volume, on Discover page, and persist on exit
	a1 := newTestApp()
	a1.dataDir = dir
	a1.player = player.New(func() {}, a1.handleProgressUpdate)
	a1.player.SetInitialPrefs(0.65, 1.5)
	a1.player.Restore(queue[1], queue, 125*time.Second)
	a1.route = route{page: pageEpisode, epID: "ep-2", showID: "show-1", transcript: false}
	a1.persistSessionNow()

	// Session 2: cold restart from disk
	a2 := newTestApp()
	a2.dataDir = dir
	a2.state = loadAppState(dir)
	a2.player = player.New(func() {}, a2.handleProgressUpdate)
	a2.player.SetInitialPrefs(a2.state.Volume, a2.state.Speed)
	a2.restoreSession()

	snap2 := a2.player.Snapshot()
	if snap2.Episode == nil || snap2.Episode.ID != "ep-2" {
		t.Fatalf("expected restored episode ep-2, got %+v", snap2.Episode)
	}
	if len(snap2.Queue) != 3 || snap2.QueueIndex != 1 {
		t.Fatalf("expected restored queue len=3 idx=1, got len=%d idx=%d", len(snap2.Queue), snap2.QueueIndex)
	}
	if snap2.Position != 125*time.Second {
		t.Fatalf("expected restored position 125s, got %v", snap2.Position)
	}
	if snap2.Playing {
		t.Fatal("expected restored session to start paused without auto-blasting audio")
	}
	if snap2.Speed != 1.5 || snap2.Volume != 0.65 {
		t.Fatalf("expected speed=1.5 volume=0.65, got speed=%v volume=%v", snap2.Speed, snap2.Volume)
	}
	if a2.route.page != pageEpisode || a2.route.epID != "ep-2" || a2.route.transcript {
		t.Fatalf("expected restored route pageEpisode ep-2, got %+v", a2.route)
	}

	tt := ui.NewTester(a2.view, 1180, 780)
	tt.Frame()
	if tt.HasText("未在播放") {
		t.Fatal("restored player bar should display remembered episode instead of 未在播放")
	}
	if !tt.HasText("记忆测试单集 2") {
		t.Fatal("restored UI should show remembered episode title")
	}
	if !tt.HasText("2:05") {
		t.Fatalf("expected restored timestamp 2:05 in UI, texts=%v", tt.Texts())
	}

	// Skipping +30s while still paused before streaming resumes should advance restoredPos to 155s (2:35) and persist
	if err := tt.Click("快进 30 秒"); err != nil {
		t.Fatalf("failed to click skip forward on restored paused session: %v", err)
	}
	tt.Frame()
	if got := a2.player.Snapshot().Position; got != 155*time.Second {
		t.Fatalf("expected pre-play skip position 155s, got %v", got)
	}
	if !tt.HasText("2:35") {
		t.Fatalf("expected updated timestamp 2:35 in UI after skip")
	}

	// Switch to immersive view and persist again without ever pressing Play -> verify 155s is preserved (never reset to 0)
	a2.setTranscriptMode(true)
	a2.persistSessionNow()

	a3 := newTestApp()
	a3.dataDir = dir
	a3.state = loadAppState(dir)
	a3.player = player.New(func() {}, a3.handleProgressUpdate)
	a3.player.SetInitialPrefs(a3.state.Volume, a3.state.Speed)
	a3.restoreSession()

	if got := a3.player.Snapshot().Position; got != 155*time.Second {
		t.Fatalf("expected second reopen to preserve 155s without reset, got %v", got)
	}
	if a3.route.page != pageEpisode || a3.route.epID != "ep-2" || !a3.route.transcript {
		t.Fatalf("expected reopened route in immersive view for ep-2, got %+v", a3.route)
	}
	tt3 := ui.NewTester(a3.view, 1180, 780)
	tt3.Frame()
	if !tt3.HasText("2:35") {
		t.Fatalf("expected reopened immersive view to display 2:35 timestamp")
	}
	if _, hasVol := tt3.Find("音量"); hasVol {
		t.Fatal("reopened immersive view should keep bottom player bar hidden")
	}
}

func TestSyncedCloudProgressRendersAcrossAllPages(t *testing.T) {
	a := newTestApp()
	a.state = defaultAppState()
	a.client.SetAuth("session-ui", "")
	a.selectLibrary("session-ui", false)

	now := time.Now().Truncate(time.Second)
	epInProgress := apple.Episode{
		ID:         "ep-prog",
		ShowID:     "show-sync",
		ShowName:   "一个名字非常非常长的中文播客节目用于测试单行截断保护",
		Name:       "跨页进度同步测试单集",
		DurationMS: 3600 * 1000, // 60m
		PlaybackPosition: apple.PlaybackPosition{
			Known: true, PositionMS: 900 * 1000, RecordedAt: now, // 15m played -> 45m remaining
		},
	}
	epCompleted := apple.Episode{
		ID:         "ep-done",
		ShowID:     "show-sync",
		ShowName:   "一个名字非常非常长的中文播客节目用于测试单行截断保护",
		Name:       "云端已播完测试单集",
		DurationMS: 1800 * 1000,
		PlaybackPosition: apple.PlaybackPosition{
			Known: true, Completed: true, RecordedAt: now,
		},
	}

	a.applyCloudLibrary(cloudLibraryResult{
		saved:        []apple.Episode{epInProgress},
		recentPlayed: []apple.Episode{epCompleted},
	})
	a.mergeCloudPositions([]apple.Episode{epInProgress, epCompleted})
	a.shows["show-sync"] = &showDetailState{
		detail: &apple.ShowDetailResult{
			Show:     apple.Show{ID: "show-sync", Name: "测试同步节目"},
			Episodes: []apple.Episode{epInProgress, epCompleted},
		},
	}
	a.searchRes = &apple.SearchResult{Episodes: []apple.Episode{epInProgress, epCompleted}}
	a.episodes[epInProgress.ID] = &episodeDetailState{episode: &epInProgress, fetched: true}
	a.episodes[epCompleted.ID] = &episodeDetailState{episode: &epCompleted, fetched: true}

	tt := ui.NewTester(a.view, 1000, 760)

	// 1. Show Detail page
	a.route = route{page: pageShow, showID: "show-sync"}
	tt.Frame()
	if !tt.HasText("剩 45m") || !tt.HasText("已播完") {
		t.Fatalf("show detail missing synced progress indicators; texts=%v", tt.Texts())
	}

	// 2. Saved Episodes page
	a.route = route{page: pageSaved}
	tt.Frame()
	if !tt.HasText("剩 45m") || !tt.HasText("同步云端") {
		t.Fatalf("saved page missing synced progress or sync button; texts=%v", tt.Texts())
	}

	// 3. Listen Now page (recent played from iCloud)
	a.route = route{page: pageListenNow}
	tt.Frame()
	if !tt.HasText("云端已播完测试单集") || !tt.HasText("已播完") {
		t.Fatalf("listen now missing cloud recent played episode or completed badge; texts=%v", tt.Texts())
	}

	// 4. Search Results page
	a.route = route{page: pageSearch, query: "测试"}
	tt.Frame()
	if !tt.HasText("剩 45m") || !tt.HasText("已播完") {
		t.Fatalf("search page missing synced progress indicators; texts=%v", tt.Texts())
	}

	// 5. Episode Detail page (in-progress + completed)
	a.route = route{page: pageEpisode, epID: epInProgress.ID, showID: epInProgress.ShowID}
	tt.Frame()
	if !tt.HasText("继续播放 15:00") || !tt.HasText("剩 45m") {
		t.Fatalf("episode detail missing synced resume label; texts=%v", tt.Texts())
	}
	a.route = route{page: pageEpisode, epID: epCompleted.ID, showID: epCompleted.ShowID}
	tt.Frame()
	if !tt.HasText("重新播放") || !tt.HasText("已播完") {
		t.Fatalf("completed episode detail missing replay/completed label; texts=%v", tt.Texts())
	}

	// 6. Immersive View (even when episode is not active in player yet)
	a.route = route{page: pageEpisode, epID: epInProgress.ID, showID: epInProgress.ShowID, transcript: true}
	tt.Frame()
	if !tt.HasText("15:00") || !tt.HasText("-45:00") {
		t.Fatalf("immersive view missing synced cloud timestamps; texts=%v", tt.Texts())
	}
}

func TestEpisodeMoreMenuHoverDwellAndClickPin(t *testing.T) {
	a := newTestApp()
	a.state = defaultAppState()
	ep := apple.Episode{
		ID:         "ep-menu-1",
		ShowID:     "show-menu-1",
		ShowName:   "半拿铁 | 商业沉浮录",
		Name:       "长虹往事：彩电大王今何在",
		DurationMS: 3600 * 1000,
		URL:        "https://podcasts.apple.com/us/podcast/id1615939013?i=ep-menu-1",
	}
	other := apple.Episode{
		ID:         "ep-playing",
		ShowID:     "show-menu-1",
		ShowName:   "半拿铁 | 商业沉浮录",
		Name:       "正在播放单集",
		DurationMS: 1800 * 1000,
	}
	a.player.Restore(other, []apple.Episode{other}, 30*time.Second)
	a.charts = &apple.ChartsResult{
		TrendingEpisodes: []apple.Episode{ep},
	}
	a.featuredEps = []apple.Episode{ep}
	a.route = route{page: pageDiscover}

	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()

	moreRect, ok := tt.Find("更多操作 " + ep.Name)
	if !ok {
		t.Fatalf("missing '...' more button on hero card; texts=%v", tt.Texts())
	}
	cx, cy := moreRect.X+moreRect.W/2, moreRect.Y+moreRect.H/2

	// 1. Brief hover (< epMenuHoverDelay) then move away must NOT open the menu.
	tt.Move(cx, cy)
	tt.Advance(200 * time.Millisecond)
	if tt.HasText("添加到队列") {
		t.Fatal("menu opened too early on brief hover")
	}
	tt.Move(10, 10)
	tt.Advance(600 * time.Millisecond)
	if tt.HasText("添加到队列") {
		t.Fatal("menu should not open after cursor left before dwell threshold")
	}

	// 2. Hover-dwell (>= epMenuHoverDelay) opens transient menu.
	tt.Move(cx, cy)
	tt.Advance(520 * time.Millisecond)
	if !tt.HasText("插播") || !tt.HasText("添加到队列") || !tt.HasText("标记为已播完") {
		t.Fatalf("hover-dwell did not open episode menu; texts=%v", tt.Texts())
	}
	if a.epMenu.pinned {
		t.Fatal("hover-dwell menu should be transient (unpinned)")
	}

	// 3. Moving pointer onto the floating menu panel keeps transient menu open.
	itemRect, ok := tt.Find("添加到队列")
	if !ok {
		t.Fatal("could not locate menu item rect")
	}
	tt.Move(itemRect.X+itemRect.W/2, itemRect.Y+itemRect.H/2)
	tt.Advance(900 * time.Millisecond)
	if !tt.HasText("添加到队列") {
		t.Fatal("transient menu disappeared while cursor was hovering over the menu panel")
	}

	// 4. Moving pointer away from both trigger and menu dismisses transient menu automatically.
	tt.Move(10, 10)
	tt.Advance(400 * time.Millisecond)
	if tt.HasText("添加到队列") {
		t.Fatal("transient menu did not auto-dismiss after cursor moved away")
	}

	// 5. Left-clicking "..." opens menu immediately and pins it even after moving pointer away.
	tt.ClickAt(cx, cy)
	tt.Frame()
	if !tt.HasText("插播") || !a.epMenu.pinned {
		t.Fatalf("left-click did not open pinned menu; pinned=%v texts=%v", a.epMenu.pinned, tt.Texts())
	}
	tt.Move(10, 10)
	tt.Advance(2 * time.Second)
	if !tt.HasText("插播") || !a.epMenu.pinned {
		t.Fatal("pinned menu closed prematurely after cursor moved away")
	}
	if a.route.page != pageDiscover {
		t.Fatalf("clicking '...' accidentally navigated away from Discover to %+v", a.route)
	}

	// 6. Clicking outside closes the pinned menu.
	tt.ClickAt(10, 10)
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("插播") || a.epMenu.openID != "" {
		t.Fatalf("clicking outside did not dismiss pinned menu; openID=%q", a.epMenu.openID)
	}

	// 7. Hover-dwell opens transient menu -> left-clicking "..." upgrades it to pinned!
	tt.Move(cx, cy)
	tt.Advance(520 * time.Millisecond)
	if !tt.HasText("插播") || a.epMenu.pinned {
		t.Fatalf("expected transient hover menu before click-upgrade; pinned=%v", a.epMenu.pinned)
	}
	tt.ClickAt(cx, cy)
	tt.Frame()
	if !tt.HasText("插播") || !a.epMenu.pinned {
		t.Fatalf("clicking '...' on open hover menu should pin it instead of closing; pinned=%v", a.epMenu.pinned)
	}
	tt.Move(10, 10)
	tt.Advance(1500 * time.Millisecond)
	if !tt.HasText("插播") {
		t.Fatal("upgraded pinned menu should stay open after moving mouse away")
	}

	// 8. Clicking a real action item ("插播") executes the action and closes the menu.
	if err := tt.Click("插播"); err != nil {
		t.Fatalf("failed to click 插播 menu item: %v", err)
	}
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("添加到队列") || a.epMenu.openID != "" {
		t.Fatalf("menu should close after clicking an item; openID=%q", a.epMenu.openID)
	}
	snap := a.player.Snapshot()
	if len(snap.Queue) != 2 || snap.Queue[1].ID != ep.ID {
		t.Fatalf("expected 插播 to insert %q as next track, got queue=%+v", ep.ID, snap.Queue)
	}

	// 9. Left-clicking "..." while pinned toggles it closed without immediate hover-reopen.
	tt.ClickAt(cx, cy)
	tt.Frame()
	if !tt.HasText("收藏单集") {
		t.Fatal("expected menu reopened on click")
	}
	tt.ClickAt(cx, cy)
	tt.Advance(800 * time.Millisecond)
	if tt.HasText("收藏单集") || a.epMenu.openID != "" {
		t.Fatalf("second click on pinned '...' should close and suppress immediate hover reopen; openID=%q", a.epMenu.openID)
	}
}
