package app

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"

	"apple-podcasts/internal/apple"
	"apple-podcasts/internal/player"
)

const (
	sidebarWidth     = 224
	topBarH          = 36
	playerBarH       = 64
	epMenuHoverDelay = 480 * time.Millisecond
	epMenuLeaveGrace = 260 * time.Millisecond
)

// artworkHeroMatch shares an episode artwork identity across the player bar,
// episode detail header, and immersive Now Playing stage for hero flights.
type artworkHeroMatch struct {
	epID string
}

var (
	animImmersiveSpring = ui.Animation{
		Spring: ui.SpringOptions{Duration: 460 * time.Millisecond, Bounce: 0.08},
		Oklab:  true,
	}
	animHeroFlight = ui.Animation{
		Spring: ui.SpringOptions{Duration: 480 * time.Millisecond, Bounce: 0.12},
		Oklab:  true,
	}
	animLayoutSpring = ui.Animation{
		Spring: ui.SpringOptions{Duration: 360 * time.Millisecond, Bounce: 0.05},
		Oklab:  true,
	}
	animPageTransition = ui.Animation{
		Duration: 220 * time.Millisecond,
		Ease:     ui.EaseOut,
		Oklab:    true,
	}
	animControlSpring = ui.Animation{
		Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.16},
	}
	animMenuPop = ui.Animation{
		Spring: ui.SpringOptions{Duration: 210 * time.Millisecond, Bounce: 0.08},
		Oklab:  true,
	}
	animUIHover = ui.ElementTransition{
		Colors:   true,
		Shadows:  true,
		Duration: 150 * time.Millisecond,
	}
)

func applyTapPose(e ui.Element, hoverScale float32, liftY float32) {
	p := ui.Pose{}
	switch {
	case e.Pressed():
		p = ui.Pose{Scale: 0.94}
	case e.Hovered():
		p = ui.Pose{Y: liftY, Scale: hoverScale}
	}
	e.AnimatePose(p, animControlSpring)
}

type palette struct {
	dark     bool
	bg       ui.Color
	sidebar  ui.Color
	card     ui.Color
	hover    ui.Color
	border   ui.Color
	text     ui.Color
	muted    ui.Color
	faint    ui.Color
	accent   ui.Color
	onAccent ui.Color
	shade    ui.Color
}

func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			dark:     true,
			bg:       ui.Hex("#141416"),
			sidebar:  ui.Hex("#19191d"),
			card:     ui.Hex("#202026"),
			hover:    ui.RGBA(255, 255, 255, 0.06),
			border:   ui.RGBA(255, 255, 255, 0.08),
			text:     ui.Hex("#f5f5f7"),
			muted:    ui.RGBA(235, 235, 245, 0.60),
			faint:    ui.RGBA(235, 235, 245, 0.34),
			accent:   ui.Hex("#9b51e0"),
			onAccent: ui.Hex("#ffffff"),
			shade:    ui.RGBA(0, 0, 0, 0.45),
		}
	}
	return palette{
		bg:       ui.Hex("#ffffff"),
		sidebar:  ui.Hex("#f3f4f7"),
		card:     ui.Hex("#f7f7fa"),
		hover:    ui.RGBA(0, 0, 0, 0.045),
		border:   ui.RGBA(0, 0, 0, 0.08),
		text:     ui.Hex("#1d1d1f"),
		muted:    ui.RGBA(60, 60, 67, 0.66),
		faint:    ui.RGBA(60, 60, 67, 0.36),
		accent:   ui.Hex("#8e2de2"),
		onAccent: ui.Hex("#ffffff"),
		shade:    ui.RGBA(0, 0, 0, 0.14),
	}
}

func (a *App) view(c *ui.Context) {
	a.beginEpisodeMenu()
	p := paletteFor(c.Theme())
	c.SetTheme(c.Theme().WithAccent(p.accent))
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}

	if a.pendingToast != "" {
		msg := a.pendingToast
		a.pendingToast = ""
		c.AddToast(ui.Toast{Title: msg})
	}

	snap := a.player.Snapshot()
	if snap.Playing {
		a.maybeSyncPeriodicProgress()
		c.After(250 * time.Millisecond)
	}

	inImmersive := a.route.page == pageEpisode && a.route.transcript

	root := ui.Column(c).Fill().TextColor(p.text)
	if !c.Vibrancy() {
		root.Background(p.sidebar)
	}
	root.Children(func() {
		a.topTitleBar(c, p)
		ui.Row(c.Key("main-row")).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Transition(ui.ElementTransition{
			Height:    true,
			Animation: &animLayoutSpring,
		}).Children(func() {
			a.sidebarView(c, p)
			ui.Box(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).Padding(0, 8, 6, 0).Children(func() {
				ui.Column(c).Fill().Background(p.bg).Radius(10).Border(1, p.border).Clip().Children(func() {
					a.contentView(c, p, snap)
				})
			})
		})
		if !inImmersive {
			a.playerBarView(c, p, snap)
		}
	})
	a.finishEpisodeMenu()
}

func (a *App) topTitleBar(c *ui.Context, p palette) {
	bar := c.TitleBar()
	h := max(bar.Height, topBarH)
	rightPad := max(bar.Right, 138)
	leftPad := max(bar.Left, 14)

	row := ui.Row(c).Height(h).Shrink(0).Padding(0, rightPad, 0, leftPad).DragWindow().AlignItems(ui.Center).Gap(2)
	if !c.Vibrancy() {
		row.Background(p.sidebar)
	}
	row.Children(func() {
		ui.Text(c, "Podcasts").FontSize(12.5).FontWeight(500).TextColor(p.muted).Padding(4, 8).SingleLine().DragWindow()

		if a.topMenuButton(c.Key("top-discover"), p, "发现", a.route.page == pageDiscover).Clicked() {
			a.navigate(route{page: pageDiscover})
			a.maybeSyncCloudLibrary()
		}
		if a.topMenuButton(c.Key("top-listen"), p, "待播", a.route.page == pageListenNow).Clicked() {
			a.navigate(route{page: pageListenNow})
			a.maybeSyncCloudLibrary()
		}
		if a.topMenuButton(c.Key("top-subs"), p, "订阅", a.route.page == pageSubscriptions).Clicked() {
			a.navigate(route{page: pageSubscriptions})
			a.maybeSyncCloudLibrary()
		}
		if a.topMenuButton(c.Key("top-saved"), p, "收藏", a.route.page == pageSaved).Clicked() {
			a.navigate(route{page: pageSaved})
			a.maybeSyncCloudLibrary()
		}

		sfLabel := "地区: " + strings.ToUpper(a.state.Storefront)
		if a.topMenuButton(c.Key("top-region"), p, sfLabel, false).Tooltip("点击切换商店地区 (US / CN / JP / GB)").Clicked() {
			a.cycleStorefront()
		}

		if len(a.backStack) > 0 || len(a.forwardStack) > 0 {
			ui.Row(c).Margin(0, 0, 0, 6).Gap(2).AlignItems(ui.Center).Children(func() {
				if a.topNavArrow(c.Key("top-back"), p, iconBack, "后退", len(a.backStack) > 0).Clicked() {
					a.goBack()
				}
				if a.topNavArrow(c.Key("top-fwd"), p, iconForward, "前进", len(a.forwardStack) > 0).Clicked() {
					a.goForward()
				}
			})
		}
	})
}

func (a *App) topMenuButton(c *ui.Context, p palette, label string, active bool) ui.Element {
	b := ui.ButtonBase(c).Height(26).PaddingX(8).Radius(6).Center().FocusRing(false).Label(label).Cursor(ui.CursorPointer)
	switch {
	case active:
		b.Background(p.hover).TextColor(p.text)
	case b.Hovered():
		b.Background(p.hover).TextColor(p.text)
	default:
		b.TextColor(p.muted)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12.5).FontWeight(500).SingleLine()
	})
	return b
}

func (a *App) topNavArrow(c *ui.Context, p palette, icon *ui.SVG, label string, enabled bool) ui.Element {
	b := ui.ButtonBase(c).Size(24, 24).Radius(6).Center().FocusRing(false).Label(label).Tooltip(label).Disabled(!enabled)
	switch {
	case !enabled:
		b.TextColor(p.faint)
	case b.Hovered():
		b.Background(p.hover).TextColor(p.text).Cursor(ui.CursorPointer)
	default:
		b.TextColor(p.muted).Cursor(ui.CursorPointer)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(13, 13)
	})
	return b
}

func (a *App) sidebarView(c *ui.Context, p palette) {
	side := ui.Column(c).Width(sidebarWidth).Shrink(0).Padding(8, 12, 12).Gap(12).DragWindow()
	if !c.Vibrancy() {
		side.Background(p.sidebar)
	}
	side.Children(func() {
		sf := ui.SearchField(c.Key("search"), &a.searchQuery).Placeholder("搜索播客或单集...")
		if sf.Submitted() && strings.TrimSpace(a.searchQuery) != "" {
			a.runSearch(a.searchQuery)
		}

		ui.Column(c).Gap(3).Children(func() {
			a.navButton(c.Key("nav-discover"), p, pageDiscover, iconCompass, "发现与榜单", 0)
			a.navButton(c.Key("nav-listen"), p, pageListenNow, iconClock, "待播与更新", 0)
			a.navButton(c.Key("nav-subs"), p, pageSubscriptions, iconLibrary, "我的订阅", len(a.subscriptions()))
			a.navButton(c.Key("nav-saved"), p, pageSaved, iconBookmark, "已存单集", len(a.savedEpisodes()))
		})

		ui.Spacer(c)

		ui.Column(c).Padding(10).Gap(8).Radius(10).Background(p.card).Border(1, p.border).Children(func() {
			if a.client.HasAuth() {
				name := a.accountName()
				if name == "" {
					name = "已登录 Apple ID"
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(28, 28).Radius(999).Background(p.accent.Alpha(0.15)).TextColor(p.accent).Center().Children(func() {
						ui.Icon(c, iconUser).Size(15, 15)
					})
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
						ui.Text(c, name).FontSize(12.5).FontWeight(600).SingleLine()
						ui.Text(c, "iCloud 同步已开启").FontSize(11).TextColor(p.muted).SingleLine()
					})
				})
				ui.Row(c).Gap(6).Children(func() {
					syncBtn := ui.Button(c.Key("sync-btn"), "").Grow(1).Disabled(a.cloudLoading).Label("同步云端").Children(func() {
						ui.Icon(c, iconRefresh).Size(13, 13)
						label := "同步"
						if a.cloudLoading {
							label = "同步中..."
						}
						ui.Text(c, label).FontSize(12).SingleLine()
					})
					if syncBtn.Clicked() {
						a.syncCloudLibrary()
					}
					if ui.Button(c.Key("signout-btn"), "").Label("退出登录").Tooltip("退出 Apple ID").Children(func() {
						ui.Icon(c, iconSignOut).Size(13, 13)
					}).Clicked() {
						a.signOut()
					}
				})
			} else {
				ui.Text(c, "登录 Apple ID 可同步跨设备订阅、待播队列和播放进度").FontSize(11.5).TextColor(p.muted).LineHeight(1.4)
				btnLabel := "登录 Apple ID"
				if a.signingIn {
					btnLabel = "正在登录窗口..."
				}
				if ui.PrimaryButton(c.Key("signin-btn"), btnLabel).FillWidth().Clicked() {
					a.startWebSignIn()
				}
			}
		})
	})
}

func (a *App) navButton(c *ui.Context, p palette, kind pageKind, icon *ui.SVG, label string, badge int) {
	active := a.route.page == kind
	btn := ui.ButtonBase(c).Height(36).PaddingX(10).Gap(10).Radius(8).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Label(label).Transition(animUIHover)
	switch {
	case active:
		btn.Background(p.accent.Alpha(0.14)).TextColor(p.accent)
	case btn.Hovered():
		btn.Background(p.hover).TextColor(p.text)
	default:
		btn.TextColor(p.text)
	}
	btn.Children(func() {
		ui.Icon(c, icon).Size(16, 16)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).Grow(1).SingleLine()
		if badge > 0 {
			ui.Badge(c, fmt.Sprint(badge))
		}
	})
	if btn.Clicked() {
		a.navigate(route{page: kind})
		if kind == pageDiscover || kind == pageListenNow || kind == pageSubscriptions || kind == pageSaved {
			a.maybeSyncCloudLibrary()
		}
	}
}

func (a *App) contentView(c *ui.Context, p palette, snap player.Snapshot) {
	w, _ := c.Size()
	availW := max(float32(320), w-sidebarWidth-56)
	cols := max(3, min(6, int((availW+14)/(134+14))))

	ui.Box(c.Key("content-stage")).Fill().Clip().Children(func() {
		if a.route.page == pageEpisode && a.route.transcript {
			immTr := ui.ElementTransition{
				Animation: &animImmersiveSpring,
				Enter:     &ui.Motion{Blur: 14},
				Exit:      &ui.Motion{Blur: 10},
			}
			ui.Box(c.Key("stage-immersive-" + a.route.epID)).Absolute().Fill().Transition(immTr).Children(func() {
				a.episodeTranscriptView(c, p, snap)
			})
			return
		}

		pageTr := ui.ElementTransition{
			Animation: &animPageTransition,
			Enter:     &ui.Motion{Y: 10},
			Exit:      &ui.Motion{Y: -6},
		}
		if a.route.page == pageEpisode {
			pageTr = ui.ElementTransition{
				Animation: &animImmersiveSpring,
				Enter:     &ui.Motion{Scale: 0.985},
				Exit:      &ui.Motion{Scale: 0.985, Blur: 6},
			}
		}

		ui.Scroll(c.Key(a.route)).TrackScroll(&a.pageScroll).Absolute().Fill().Padding(18, 24, 24).Transition(pageTr).Children(func() {
			switch a.route.page {
			case pageDiscover:
				a.discoverPage(c, p, availW, cols, snap)
			case pageListenNow:
				a.listenNowPage(c, p, snap)
			case pageSubscriptions:
				a.subscriptionsPage(c, p, cols)
			case pageSaved:
				a.savedPage(c, p, snap)
			case pageSearch:
				a.searchPage(c, p, cols, snap)
			case pageShow:
				a.showDetailPage(c, p, snap)
			case pageEpisode:
				a.episodeDetailPage(c, p, snap)
			}
		})
	})
}

func (a *App) sectionHeading(c *ui.Context, p palette, title string, onClick func()) {
	row := ui.ButtonBase(c).Height(28).Gap(4).Radius(6).Padding(0, 4, 0, 0).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Label(title)
	if onClick != nil {
		row.Cursor(ui.CursorPointer)
		if row.Hovered() {
			row.TextColor(p.accent)
		}
	}
	row.Children(func() {
		ui.Text(c, title).FontSize(17).FontWeight(700).SingleLine()
		if onClick != nil {
			ui.Icon(c, iconForward).Size(15, 15).TextColor(p.muted)
		}
	})
	if onClick != nil && row.Clicked() {
		onClick()
	}
}

func (a *App) discoverPage(c *ui.Context, p palette, availW float32, showCols int, snap player.Snapshot) {
	ui.Column(c).Gap(18).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Home").FontSize(26).FontWeight(800).Grow(1)
			if ui.Button(c.Key("refresh-charts"), "").Height(28).Disabled(a.chartsLoading).Children(func() {
				ui.Icon(c, iconRefresh).Size(13, 13)
				ui.Text(c, "刷新").FontSize(12.5)
			}).Clicked() {
				a.loadCharts()
				if a.client.HasAuth() {
					a.syncCloudLibrary()
				}
			}
		})

		if a.chartsLoading && a.charts == nil {
			ui.Row(c).Padding(40).Gap(10).Center().Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在加载 Apple Podcasts 精选与榜单...").FontSize(13.5).TextColor(p.muted)
			})
			return
		}
		if a.chartsErr != "" && a.charts == nil {
			a.errorBanner(c, p, "加载榜单失败: "+a.chartsErr, a.loadCharts)
			return
		}
		if a.charts == nil {
			return
		}

		// 1. New Episodes (Hero Cards with special covers, compact 4-col grid)
		heroEps := a.featuredEps
		if len(heroEps) == 0 {
			heroEps = a.charts.TrendingEpisodes
		}
		if len(heroEps) > 0 {
			heroCols := max(2, min(4, int((availW+14)/(195+14))))
			showLimit := min(len(heroEps), heroCols*2)
			if len(heroEps) >= 4 && heroCols >= 4 {
				showLimit = 4
			}
			a.sectionHeading(c.Key("sec-new-eps"), p, "最新单集", func() {
				a.navigate(route{page: pageListenNow})
			})
			ui.Grid(c).Columns(heroCols).GapX(14).GapY(14).Children(func() {
				for i := range showLimit {
					ep := heroEps[i]
					a.episodeHeroCard(c.Key("hero-"+ep.ID), ep, heroEps, snap)
				}
			})
		}

		// 2. Continue Playing (Compact 3-col horizontal cards)
		var continueEps []apple.Episode
		if a.client.HasAuth() {
			continueEps = a.cloudUpNext
			if len(continueEps) == 0 {
				continueEps = a.recentEpisodes()
			}
		} else {
			continueEps = a.state.RecentEpisodes
		}
		if len(continueEps) > 0 {
			cpCols := max(1, min(3, int((availW+12)/(260+12))))
			cpLimit := min(len(continueEps), cpCols*2)
			a.sectionHeading(c.Key("sec-continue"), p, "继续收听", func() {
				a.navigate(route{page: pageListenNow})
			})
			ui.Grid(c).Columns(cpCols).GapX(12).GapY(12).Children(func() {
				for i := range cpLimit {
					ep := continueEps[i]
					a.continuePlayingCard(c.Key("cp-"+ep.ID), p, ep, continueEps, snap)
				}
			})
		}

		// 3. Popular Shows (Compact 5-6 col grid)
		if len(a.charts.TopShows) > 0 {
			showCount := min(len(a.charts.TopShows), showCols*2)
			a.sectionHeading(c.Key("sec-pop-shows"), p, "热门节目", nil)
			ui.Grid(c).Columns(showCols).GapX(14).GapY(16).Children(func() {
				for i := range showCount {
					a.showCard(c.Key("pop-"+a.charts.TopShows[i].ID), p, a.charts.TopShows[i])
				}
			})
		}

		// 4. Trending Episodes (Compact 2-column ranked list)
		if len(a.charts.TrendingEpisodes) > 0 {
			rankCols := 1
			if availW >= 620 {
				rankCols = 2
			}
			topCount := min(len(a.charts.TrendingEpisodes), 10)
			a.sectionHeading(c.Key("sec-top-eps"), p, "热门单集榜", nil)
			ui.Grid(c).Columns(rankCols).GapX(16).GapY(6).Children(func() {
				for i := range topCount {
					ep := a.charts.TrendingEpisodes[i]
					a.compactRankedEpisodeRow(c.Key("rank-"+ep.ID), p, i+1, ep, a.charts.TrendingEpisodes, snap)
				}
			})
		}
	})
}

func (a *App) episodeHeroCard(c *ui.Context, ep apple.Episode, queue []apple.Episode, snap player.Snapshot) {
	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	hasSpecialHero := ep.HeroArtworkURL != "" && ep.ArtworkOrigin != "episode"
	hasEpisodeArt := ep.ArtworkOrigin == "episode" && ep.ArtworkURL != "" && ep.ShowArtworkURL != "" && ep.ArtworkURL != ep.ShowArtworkURL

	var cardBg ui.Color
	switch {
	case hasSpecialHero:
		cardBg = richHeroCardBg(parseHexColor(ep.HeroBgColor, parseHexColor(ep.BgColor, ui.Hex("#18161e"))))
	default:
		cardBg = richHeroCardBg(parseHexColor(ep.BgColor, parseHexColor(ep.ShowBgColor, ui.Hex("#264c7e"))))
	}

	card := ui.ButtonBase(c).Height(264).Radius(13).Clip().FocusRing(false).Shadow(0, 6, 18, -3, ui.RGBA(0, 0, 0, 0.22)).Label(ep.Name).Cursor(ui.CursorPointer).Transition(animUIHover)
	if hasSpecialHero {
		card.Background(cardBg)
	} else {
		card.LinearGradient(ui.LinearGradient{
			From:  cardBg.MixOklab(ui.Hex("#ffffff"), 0.06),
			To:    cardBg.MixOklab(ui.Hex("#000000"), 0.14),
			Angle: 180,
		})
	}
	if card.Hovered() {
		card.Shadow(0, 12, 28, -4, ui.RGBA(0, 0, 0, 0.34))
	}
	applyTapPose(card, 1.015, -3)

	var playClicked, moreClicked bool

	card.Children(func() {
		// In-flow background layer (z=0): artwork + gradient
		ui.Box(c).Fill().ZIndex(0).Children(func() {
			if hasSpecialHero {
				ui.Box(c).FillWidth().Height(208).Children(func() {
					netimage.Image(c, apple.FormatHeroArtwork(ep.HeroArtworkURL), netimage.Options{MaxSide: 600}).Fill()
				})
				ui.Box(c).Absolute().Fill().LinearGradient(ui.LinearGradient{
					From:  cardBg.Alpha(0),
					To:    cardBg,
					Angle: 180,
					Start: 0.20,
					End:   0.60,
					Oklab: true,
				})
			} else {
				ui.Box(c).Fill().Padding(14, 0, 0, 0).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(102, 102).Children(func() {
						ui.Box(c).Fill().Radius(10).Shadow(0, 8, 20, -4, ui.RGBA(0, 0, 0, 0.36)).Clip().Children(func() {
							netimage.Image(c, a.episodeArtworkURL(ep), netimage.Options{MaxSide: 400}).Fill()
						})
						if hasEpisodeArt {
							ui.Box(c).Absolute().Bottom(-4).Left(-6).Size(28, 28).Radius(6).Border(1, ui.RGBA(255, 255, 255, 0.25)).Shadow(0, 3, 8, 0, ui.RGBA(0, 0, 0, 0.4)).Clip().Children(func() {
								netimage.Image(c, apple.FormatArtwork(ep.ShowArtworkURL, 600), netimage.Options{MaxSide: 400}).Fill()
							})
						}
					})
				})
			}
		})

		// Foreground text and action controls painted strictly on top (z=1)
		ui.Column(c).Absolute().Fill().ZIndex(1).Justify(ui.End).AlignItems(ui.Start).Padding(12).Gap(3).Children(func() {
			relDate := apple.FormatRelativeDate(ep.ReleaseDate)
			if ep.ContentRating == "explicit" {
				relDate += " · E"
			}
			if relDate != "" {
				ui.Text(c, relDate).FontSize(11).FontWeight(500).TextColor(ui.RGBA(255, 255, 255, 0.72)).SingleLine()
			}

			ui.Text(c, ep.Name).FontSize(13.5).FontWeight(700).TextColor(ui.Hex("#ffffff")).MaxLines(2).LineHeight(1.22).TextAlign(ui.Start)

			if sum := compactEpisodeSummary(ep); sum != "" {
				ui.Text(c, sum).FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.76)).SingleLine().TextAlign(ui.Start)
			}

			ui.Row(c).FillWidth().Margin(6, 0, 0, 0).AlignItems(ui.Center).Children(func() {
				progLabel, progFrac, progCompleted, hasProg := a.episodeProgressDisplay(ep, snap)
				durText := apple.FormatShortDurationMS(ep.DurationMS)
				if durText == "" {
					durText = "播放"
				}
				switch {
				case isCurrent && snap.Playing:
					durText = "播放中"
				case hasProg && progLabel != "":
					durText = progLabel
				}
				playPill := ui.ButtonBase(c.Key("hero-play")).Height(26).Padding(0, 10).Gap(5).Radius(999).Center().FocusRing(false).Background(ui.RGBA(255, 255, 255, 0.20)).TextColor(ui.Hex("#ffffff")).Label("播放 " + ep.Name).Cursor(ui.CursorPointer)
				if playPill.Hovered() {
					playPill.Background(ui.RGBA(255, 255, 255, 0.30))
				}
				playPill.Children(func() {
					switch {
					case isCurrent && snap.Playing:
						ui.Icon(c, iconPause).Size(10, 10)
					case progCompleted:
						ui.Icon(c, iconCheck).Size(11, 11)
					default:
						ui.Icon(c, iconPlay).Size(10, 10)
					}
					if hasProg && !progCompleted && progFrac > 0.02 && !(isCurrent && snap.Playing) {
						ui.Box(c).Width(22).Height(4).Radius(2).Background(ui.RGBA(255, 255, 255, 0.28)).Clip().Children(func() {
							ui.Box(c).FillHeight().Width(22 * progFrac).Radius(2).Background(ui.Hex("#ffffff"))
						})
					}
					ui.Text(c, durText).FontSize(11.5).FontWeight(600).SingleLine()
				})
				if playPill.Clicked() {
					playClicked = true
				}

				ui.Spacer(c)

				moreClicked = a.episodeMoreMenuButton(c.Key("hero-more"), "hero-"+ep.ID, ep, queue, snap, episodeMoreStyle{
					size:     26,
					iconSize: 14,
					fg:       ui.RGBA(255, 255, 255, 0.88),
					idleBg:   ui.RGBA(255, 255, 255, 0.14),
					hoverBg:  ui.RGBA(255, 255, 255, 0.24),
					activeBg: ui.RGBA(255, 255, 255, 0.32),
					activeFg: ui.Hex("#ffffff"),
					label:    "更多操作 " + ep.Name,
				})
			})
		})
	})

	switch {
	case playClicked:
		if isCurrent {
			a.player.TogglePlay()
		} else {
			a.playEpisode(ep, queue)
		}
	case moreClicked:
		// Handled by episodeMoreMenuButton (opens/pins/toggles the floating menu)
	case card.Clicked():
		a.openEpisode(ep, false)
	}
}

func (a *App) continuePlayingCard(c *ui.Context, p palette, ep apple.Episode, queue []apple.Episode, snap player.Snapshot) {
	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	progLabel, frac, completed, hasProg := a.episodeProgressDisplay(ep, snap)
	durLabel := progLabel
	if !hasProg || durLabel == "" {
		durLabel = apple.FormatShortDurationMS(ep.DurationMS)
	}
	if durLabel == "" {
		durLabel = "播放"
	}

	card := ui.ButtonBase(c).Padding(10).Gap(10).Radius(12).AlignItems(ui.Center).Background(p.card).Border(1, p.border).FocusRing(false).Label(ep.Name).Cursor(ui.CursorPointer).Transition(animUIHover)
	if card.Hovered() {
		card.Background(p.hover)
	}

	var playClicked, moreClicked bool
	card.Children(func() {
		ui.Box(c).Size(70, 70).Shrink(0).Radius(8).Clip().Children(func() {
			netimage.Image(c, a.episodeArtworkURL(ep), netimage.Options{MaxSide: 400}).Fill()
		})

		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(2).Children(func() {
			if dt := apple.FormatDate(ep.ReleaseDate); dt != "" {
				ui.Text(c, dt).FontSize(10.5).TextColor(p.faint).SingleLine()
			}
			title := ui.Text(c, ep.Name).FontSize(13).FontWeight(700).SingleLine().TextAlign(ui.Start)
			if isCurrent {
				title.TextColor(p.accent)
			}
			if sum := compactEpisodeSummary(ep); sum != "" {
				ui.Text(c, sum).FontSize(11).TextColor(p.muted).SingleLine().TextAlign(ui.Start)
			}

			ui.Row(c).FillWidth().Margin(3, 0, 0, 0).Gap(6).AlignItems(ui.Center).Children(func() {
				pill := ui.ButtonBase(c.Key("cp-play")).Height(22).Padding(0, 8).Gap(5).Radius(999).Center().FocusRing(false).Background(p.accent.Alpha(0.14)).TextColor(p.accent).Label("播放").Cursor(ui.CursorPointer)
				pill.Children(func() {
					switch {
					case isCurrent && snap.Playing:
						ui.Icon(c, iconPause).Size(9, 9)
					case completed:
						ui.Icon(c, iconCheck).Size(10, 10)
					default:
						ui.Icon(c, iconPlay).Size(9, 9)
					}
					if hasProg && !completed && frac > 0.02 {
						ui.Box(c).Width(24).Height(4).Radius(2).Background(p.accent.Alpha(0.22)).Clip().Children(func() {
							ui.Box(c).FillHeight().Width(24 * frac).Radius(2).Background(p.accent)
						})
					}
					ui.Text(c, durLabel).FontSize(11).FontWeight(600).SingleLine()
				})
				if pill.Clicked() {
					playClicked = true
				}

				ui.Spacer(c)

				moreClicked = a.episodeMoreMenuButton(c.Key("cp-more"), "cp-"+ep.ID, ep, queue, snap, episodeMoreStyle{
					size:     24,
					iconSize: 13,
					fg:       p.muted,
					hoverBg:  p.hover,
					activeBg: p.accent.Alpha(0.18),
					activeFg: p.accent,
					label:    "更多操作 " + ep.Name,
				})
			})
		})
	})

	switch {
	case playClicked:
		if isCurrent {
			a.player.TogglePlay()
		} else {
			a.playEpisode(ep, queue)
		}
	case moreClicked:
		// Handled by episodeMoreMenuButton
	case card.Clicked():
		a.openEpisode(ep, false)
	}
}

func (a *App) compactRankedEpisodeRow(c *ui.Context, p palette, rank int, ep apple.Episode, queue []apple.Episode, snap player.Snapshot) {
	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	row := ui.ButtonBase(c).Padding(7, 10).Gap(10).Radius(9).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Label(ep.Name).Cursor(ui.CursorPointer).Transition(animUIHover)
	switch {
	case isCurrent:
		row.Background(p.accent.Alpha(0.10))
	case row.Hovered():
		row.Background(p.hover)
	}

	var playClicked, moreClicked bool
	row.Children(func() {
		ui.Textf(c, "%d", rank).Width(20).FontSize(12.5).FontWeight(700).FontFeatures("tnum").TextColor(p.muted).TextAlign(ui.Center)

		coverBtn := ui.ButtonBase(c.Key("rk-play")).Size(44, 44).Shrink(0).Radius(7).Clip().FocusRing(false).Label("播放").Cursor(ui.CursorPointer)
		coverBtn.Children(func() {
			netimage.Image(c, a.episodeArtworkURL(ep), netimage.Options{MaxSide: 400}).Fill()
			if isCurrent || coverBtn.Hovered() {
				ui.Box(c).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.42)).Center().TextColor(ui.Hex("#ffffff")).Children(func() {
					if isCurrent && snap.Playing {
						ui.Icon(c, iconPause).Size(15, 15)
					} else {
						ui.Icon(c, iconPlay).Size(15, 15)
					}
				})
			}
		})
		if coverBtn.Clicked() {
			playClicked = true
		}

		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(2).Children(func() {
			title := ui.Text(c, ep.Name).FontSize(13).FontWeight(600).SingleLine().TextAlign(ui.Start)
			if isCurrent {
				title.TextColor(p.accent)
			}
			var meta []string
			if ep.ShowName != "" {
				meta = append(meta, ep.ShowName)
			}
			if dur := apple.FormatShortDurationMS(ep.DurationMS); dur != "" {
				meta = append(meta, dur)
			}
			progLabel, progFrac, progCompleted, hasProg := a.episodeProgressDisplay(ep, snap)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if len(meta) > 0 {
					ui.Text(c, strings.Join(meta, " · ")).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0).TextAlign(ui.Start)
				}
				if hasProg {
					a.inlineProgressBadge(c, p, progLabel, progFrac, progCompleted)
				}
			})
		})

		moreClicked = a.episodeMoreMenuButton(c.Key("rk-info"), "rk-"+ep.ID, ep, queue, snap, episodeMoreStyle{
			size:     26,
			iconSize: 13,
			fg:       p.muted,
			hoverBg:  p.hover,
			activeBg: p.accent.Alpha(0.18),
			activeFg: p.accent,
			label:    "更多操作 " + ep.Name,
		})
	})

	switch {
	case playClicked:
		if isCurrent {
			a.player.TogglePlay()
		} else {
			a.playEpisode(ep, queue)
		}
	case moreClicked:
		// Handled by episodeMoreMenuButton
	case row.Clicked():
		a.openEpisode(ep, false)
	}
}

func (a *App) listenNowPage(c *ui.Context, p palette, snap player.Snapshot) {
	ui.Column(c).Gap(18).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Children(func() {
				ui.Text(c, "待播与更新").FontSize(24).FontWeight(800)
				ui.Text(c, "继续收听断点、云端最新单集与跨设备播放历史").FontSize(13).TextColor(p.muted)
			})
			if a.client.HasAuth() {
				if ui.Button(c.Key("refresh-cloud"), "").Disabled(a.cloudLoading || a.cloudWriting).Children(func() {
					ui.Icon(c, iconRefresh).Size(14, 14)
					ui.Text(c, "同步云端").FontSize(13)
				}).Clicked() {
					a.syncCloudLibrary()
				}
			}
		})

		if !a.client.HasAuth() {
			ui.Row(c).Padding(14).Gap(12).Radius(10).Background(p.card).Border(1, p.border).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "尚未登录 Apple ID。当前显示本地播放历史；登录后可同步 iPhone/Mac 上的继续收听队列和订阅更新。").FontSize(13).TextColor(p.muted).Grow(1)
				if ui.PrimaryButton(c.Key("listen-signin"), "登录 Apple ID").Clicked() {
					a.startWebSignIn()
				}
			})
		} else if a.cloudErr != "" {
			a.errorBanner(c, p, a.cloudErr, a.syncCloudLibrary)
		}

		if len(a.cloudUpNext) > 0 {
			ui.Text(c, "云端继续收听").FontSize(17).FontWeight(700)
			ui.Column(c).Gap(6).Children(func() {
				for _, ep := range a.cloudUpNext {
					a.episodeRowWithKey(c.Key("up-"+ep.ID), p, "up-"+ep.ID, ep, a.cloudUpNext, snap)
				}
			})
		}

		if len(a.cloudNewEps) > 0 {
			ui.Text(c, "订阅最新更新").FontSize(17).FontWeight(700)
			ui.Column(c).Gap(6).Children(func() {
				for _, ep := range a.cloudNewEps {
					a.episodeRowWithKey(c.Key("new-"+ep.ID), p, "new-"+ep.ID, ep, a.cloudNewEps, snap)
				}
			})
		}

		ui.Text(c, "最近收听").FontSize(17).FontWeight(700)
		recent := a.recentEpisodes()
		if len(recent) == 0 {
			ui.Text(c, "还没有播放记录，去发现页找一个喜欢的播客开始听吧。").FontSize(13.5).TextColor(p.muted).Padding(16, 0)
		} else {
			ui.Column(c).Gap(6).Children(func() {
				for _, ep := range recent {
					a.episodeRowWithKey(c.Key("rec-"+ep.ID), p, "rec-"+ep.ID, ep, recent, snap)
				}
			})
		}
	})
}

func (a *App) subscriptionsPage(c *ui.Context, p palette, cols int) {
	shows := a.subscriptions()
	ui.Column(c).Gap(18).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Children(func() {
				ui.Text(c, "我的订阅").FontSize(24).FontWeight(800)
				ui.Textf(c, "共 %d 个订阅节目", len(shows)).FontSize(13).TextColor(p.muted)
			})
			if a.client.HasAuth() {
				if ui.Button(c.Key("sync-subs"), "").Disabled(a.cloudLoading || a.cloudWriting).Children(func() {
					ui.Icon(c, iconRefresh).Size(14, 14)
					ui.Text(c, "同步云端订阅").FontSize(13)
				}).Clicked() {
					a.syncCloudLibrary()
				}
			}
		})

		if a.client.HasAuth() && a.cloudErr != "" {
			a.errorBanner(c, p, a.cloudErr, a.syncCloudLibrary)
		}

		if len(shows) == 0 {
			ui.Column(c).Padding(40).Gap(10).Center().Children(func() {
				ui.Text(c, "暂无订阅节目").FontSize(15).FontWeight(600)
				ui.Text(c, "打开任意播客详情页点击“订阅”，或登录 Apple ID 自动同步现有订阅。").FontSize(13).TextColor(p.muted)
			})
			return
		}

		ui.Grid(c).Columns(cols).GapX(14).GapY(16).Children(func() {
			for _, show := range shows {
				a.showCard(c.Key(show.ID), p, show)
			}
		})
	})
}

func (a *App) savedPage(c *ui.Context, p palette, snap player.Snapshot) {
	episodes := a.savedEpisodes()
	ui.Column(c).Gap(18).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(3).Grow(1).Children(func() {
				ui.Text(c, "已存单集").FontSize(24).FontWeight(800)
				ui.Textf(c, "共 %d 个收藏单集", len(episodes)).FontSize(13).TextColor(p.muted)
			})
			if a.client.HasAuth() {
				if ui.Button(c.Key("sync-saved"), "").Disabled(a.cloudLoading || a.cloudWriting).Children(func() {
					ui.Icon(c, iconRefresh).Size(14, 14)
					ui.Text(c, "同步云端").FontSize(13)
				}).Clicked() {
					a.syncCloudLibrary()
				}
			}
		})

		if a.client.HasAuth() && a.cloudErr != "" {
			a.errorBanner(c, p, a.cloudErr, a.syncCloudLibrary)
		}

		if len(episodes) == 0 {
			ui.Text(c, "点击任意单集右侧的书签图标即可加入收藏。").FontSize(13.5).TextColor(p.muted).Padding(24, 0)
			return
		}

		ui.Column(c).Gap(6).Children(func() {
			for _, ep := range episodes {
				a.episodeRowWithKey(c.Key("saved-"+ep.ID), p, "saved-"+ep.ID, ep, episodes, snap)
			}
		})
	})
}

func (a *App) searchPage(c *ui.Context, p palette, cols int, snap player.Snapshot) {
	ui.Column(c).Gap(18).Children(func() {
		ui.Textf(c, "搜索“%s”", a.route.query).FontSize(24).FontWeight(800)

		if a.searchLoading {
			ui.Row(c).Padding(40).Gap(10).Center().Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在搜索 Apple Podcasts...").FontSize(13.5).TextColor(p.muted)
			})
			return
		}
		if a.searchErr != "" {
			a.errorBanner(c, p, "搜索失败: "+a.searchErr, func() { a.runSearch(a.route.query) })
			return
		}
		if a.searchRes == nil || (len(a.searchRes.Shows) == 0 && len(a.searchRes.Episodes) == 0) {
			ui.Text(c, "未找到相关播客节目或单集。").FontSize(14).TextColor(p.muted).Padding(24, 0)
			return
		}

		if len(a.searchRes.Shows) > 0 {
			ui.Text(c, "节目").FontSize(18).FontWeight(700)
			ui.Grid(c).Columns(cols).GapX(14).GapY(16).Children(func() {
				for i := range a.searchRes.Shows {
					a.showCard(c.Key(a.searchRes.Shows[i].ID), p, a.searchRes.Shows[i])
				}
			})
		}

		if len(a.searchRes.Episodes) > 0 {
			ui.Text(c, "单集").FontSize(18).FontWeight(700).Margin(8, 0, 0, 0)
			ui.Column(c).Gap(6).Children(func() {
				for _, ep := range a.searchRes.Episodes {
					a.episodeRowWithKey(c.Key("sr-"+ep.ID), p, "sr-"+ep.ID, ep, a.searchRes.Episodes, snap)
				}
			})
		}
	})
}

func (a *App) showDetailPage(c *ui.Context, p palette, snap player.Snapshot) {
	showID := a.route.showID
	st := a.shows[showID]
	if st == nil {
		a.ensureShowLoaded(showID)
		st = a.shows[showID]
	}

	ui.Column(c).Gap(18).Children(func() {
		if len(a.backStack) > 0 {
			ui.Row(c).Children(func() {
				if ui.Button(c.Key("back-btn"), "").Children(func() {
					ui.Icon(c, iconBack).Size(14, 14)
					ui.Text(c, "返回").FontSize(13)
				}).Clicked() {
					a.goBack()
				}
			})
		}

		if st.loading && st.detail == nil {
			ui.Row(c).Padding(40).Gap(10).Center().Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在加载节目详情...").FontSize(13.5).TextColor(p.muted)
			})
			return
		}
		if st.err != "" && st.detail == nil {
			a.errorBanner(c, p, "加载节目失败: "+st.err, func() { a.ensureShowLoaded(showID) })
			return
		}
		if st.detail == nil {
			return
		}

		show := st.detail.Show
		eps := st.detail.Episodes

		ui.Row(c).Gap(22).AlignItems(ui.End).Children(func() {
			ui.Box(c).Size(168, 168).Shrink(0).Radius(14).Shadow(0, 10, 28, -4, p.shade).Clip().Children(func() {
				netimage.Image(c, apple.FormatArtwork(show.ArtworkURL, 600), netimage.Options{MaxSide: 400}).Fill()
			})

			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(7).Children(func() {
				if len(show.Genres) > 0 {
					ui.Text(c, strings.Join(show.Genres, " · ")).FontSize(12).FontWeight(600).TextColor(p.accent).SingleLine()
				}
				ui.Text(c, show.Name).FontSize(25).FontWeight(800).MaxLines(2).LineHeight(1.2)
				var meta []string
				if show.ArtistName != "" {
					meta = append(meta, show.ArtistName)
				}
				if show.TrackCount > 0 {
					meta = append(meta, fmt.Sprintf("共 %d 集", show.TrackCount))
				}
				if show.ReleaseFrequency != "" {
					meta = append(meta, show.ReleaseFrequency)
				}
				if len(meta) > 0 {
					ui.Text(c, strings.Join(meta, " · ")).FontSize(13).TextColor(p.muted).SingleLine()
				}
				if show.Description != "" {
					ui.Text(c, strings.TrimSpace(show.Description)).FontSize(13).TextColor(p.muted).MaxLines(3).LineHeight(1.45).Selectable()
				}

				ui.Row(c).Gap(10).Margin(6, 0, 0, 0).Children(func() {
					if len(eps) > 0 {
						if ui.PrimaryButton(c.Key("play-latest"), "").Padding(8, 16).Radius(999).Children(func() {
							ui.Icon(c, iconPlay).Size(13, 13)
							ui.Text(c, "播放最新单集").FontSize(13.5).FontWeight(600)
						}).Clicked() {
							a.playEpisode(eps[0], eps)
						}
					}

					sub := a.isSubscribed(show.ID)
					subBtn := ui.Button(c.Key("sub-btn"), "").Padding(8, 16).Radius(999).Children(func() {
						if sub {
							ui.Icon(c, iconCheck).Size(14, 14)
							ui.Text(c, "已订阅").FontSize(13.5).FontWeight(600)
						} else {
							ui.Icon(c, iconPlus).Size(14, 14)
							ui.Text(c, "订阅节目").FontSize(13.5).FontWeight(600)
						}
					})
					if subBtn.Clicked() {
						a.toggleSubscribe(show)
					}
				})
			})
		})

		ui.Divider(c)

		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "全部单集").FontSize(17).FontWeight(700).Grow(1)
			if a.client.HasAuth() {
				if ui.Button(c.Key("sync-show-pos"), "").Height(28).Children(func() {
					ui.Icon(c, iconRefresh).Size(13, 13)
					ui.Text(c, "同步进度").FontSize(12.5)
				}).Clicked() {
					a.refreshActivePagePositions()
				}
			}
		})
		if len(eps) == 0 {
			ui.Text(c, "该节目暂无可用单集").FontSize(13.5).TextColor(p.muted)
		} else {
			ui.Column(c).Gap(6).Children(func() {
				for _, ep := range eps {
					a.episodeRowWithKey(c.Key(ep.ID), p, "show-"+ep.ID, ep, eps, snap)
				}
			})
		}

		if st.detail.NextEpisodesPath != "" {
			ui.Row(c).Center().Padding(12, 0).Children(func() {
				if ui.Button(c.Key("load-more"), "").Disabled(st.loadingMore).Children(func() {
					if st.loadingMore {
						ui.Spinner(c)
						ui.Text(c, "正在加载...").FontSize(13)
					} else {
						ui.Text(c, "加载更多单集").FontSize(13)
					}
				}).Clicked() {
					a.loadMoreShowEpisodes(showID)
				}
			})
		}
	})
}

func (a *App) resolveEpisodeDetail(epID string, snap player.Snapshot) (apple.Episode, []apple.TranscriptLine, *apple.EpisodeTranscript, bool) {
	var ep apple.Episode
	var chapters []apple.TranscriptLine
	var tr *apple.EpisodeTranscript
	loading := false
	if st := a.episodes[epID]; st != nil {
		if st.episode != nil {
			ep = *st.episode
		}
		chapters = st.chapters
		tr = st.transcript
		loading = st.loading
	}
	if ep.ID == "" && snap.Episode != nil && snap.Episode.ID == epID {
		ep = *snap.Episode
		chapters = extractChapters(ep)
	}
	return ep, chapters, tr, loading
}

func (a *App) episodeDetailPage(c *ui.Context, p palette, snap player.Snapshot) {
	ep, chapters, tr, loading := a.resolveEpisodeDetail(a.route.epID, snap)
	if ep.ID == "" {
		ui.Row(c).Padding(40).Gap(10).Center().Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在加载单集详情...").FontSize(13.5).TextColor(p.muted)
		})
		return
	}

	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	hasTranscript := tr.HasTranscript()

	ui.Column(c).Gap(18).Children(func() {
		// Top navigation strip
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			if len(a.backStack) > 0 {
				if ui.Button(c.Key("ep-back"), "").Height(30).Children(func() {
					ui.Icon(c, iconBack).Size(14, 14)
					ui.Text(c, "返回").FontSize(13)
				}).Clicked() {
					a.goBack()
				}
			}
			ui.Spacer(c)
			if loading {
				ui.Row(c).Gap(6).Margin(0, 10, 0, 0).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, "同步完整详情与文稿...").FontSize(12).TextColor(p.muted)
				})
			}
			immLabel := "沉浸播放视图"
			if hasTranscript {
				immLabel = "沉浸文稿视图"
			}
			immBtn := ui.Button(c.Key("ep-open-lyrics"), "").Height(30).Radius(999).Transition(animUIHover)
			applyTapPose(immBtn, 1.03, -1)
			if immBtn.Children(func() {
				ui.Icon(c, iconQuote).Size(14, 14)
				ui.Text(c, immLabel).FontSize(13).FontWeight(500)
			}).Clicked() {
				a.setTranscriptMode(true)
			}
		})

		// Hero header card (Apple Podcasts Web + mygo style)
		ui.Row(c).Gap(22).AlignItems(ui.End).Children(func() {
			ui.Box(c.Key("ep-detail-cover-"+ep.ID)).Size(164, 164).Shrink(0).Radius(14).Shadow(0, 10, 28, -4, p.shade).Clip().Transition(ui.ElementTransition{
				Animation: &animHeroFlight,
				Match:     artworkHeroMatch{ep.ID},
				Fly:       true,
			}).Children(func() {
				netimage.Image(c, a.episodeArtworkURL(ep), netimage.Options{MaxSide: 400}).Fill()
			})

			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(7).Children(func() {
				var meta []string
				if d := apple.FormatRelativeDate(ep.ReleaseDate); d != "" {
					meta = append(meta, d)
				}
				if dur := apple.FormatShortDurationMS(ep.DurationMS); dur != "" {
					meta = append(meta, dur)
				}
				if ep.EpisodeNumber > 0 {
					meta = append(meta, fmt.Sprintf("第 %d 集", ep.EpisodeNumber))
				}
				if ep.IsSubscriberOnly {
					meta = append(meta, "订阅专享")
				}
				if len(meta) > 0 {
					ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, strings.Join(meta, " · ")).FontSize(12.5).FontWeight(600).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
						if progLabel, progFrac, progCompleted, hasProg := a.episodeProgressDisplay(ep, snap); hasProg {
							a.inlineProgressBadge(c, p, progLabel, progFrac, progCompleted)
						}
					})
				} else if progLabel, progFrac, progCompleted, hasProg := a.episodeProgressDisplay(ep, snap); hasProg {
					a.inlineProgressBadge(c, p, progLabel, progFrac, progCompleted)
				}

				ui.Text(c, ep.Name).FontSize(23).FontWeight(800).MaxLines(3).LineHeight(1.22).TextAlign(ui.Start).Selectable()

				if showLabel := cmpNonEmpty(ep.ShowName, ep.ArtistName); showLabel != "" {
					if ep.ShowID != "" {
						showBtn := ui.ButtonBase(c.Key("ep-show-link")).FocusRing(false).Justify(ui.Start).Cursor(ui.CursorPointer).Label(showLabel)
						showBtn.Children(func() {
							ui.Text(c, showLabel+" ›").FontSize(14).FontWeight(600).TextColor(p.accent).SingleLine()
						})
						if showBtn.Clicked() {
							a.openShow(ep.ShowID)
						}
					} else {
						ui.Text(c, showLabel).FontSize(14).FontWeight(600).TextColor(p.muted).SingleLine()
					}
				}

				ui.Row(c).Gap(8).Margin(6, 0, 0, 0).Wrap().Children(func() {
					playLabel := "播放单集"
					var playIcon = iconPlay
					switch {
					case isCurrent && snap.Playing:
						playLabel = "暂停播放"
						playIcon = iconPause
					case isCurrent && snap.Position > 5*time.Second:
						playLabel = "继续播放 " + formatSeconds(snap.Position.Seconds())
					default:
						if prog, ok := a.episodeProgress(ep.ID); ok {
							if prog.Completed {
								playLabel = "重新播放"
							} else if prog.PositionSec > 5 {
								playLabel = "继续播放 " + formatSeconds(prog.PositionSec)
							}
						}
					}
					if ui.PrimaryButton(c.Key("ep-play-btn"), "").Padding(8, 16).Radius(999).Children(func() {
						ui.Icon(c, playIcon).Size(13, 13)
						ui.Text(c, playLabel).FontSize(13.5).FontWeight(600)
					}).Clicked() {
						if isCurrent {
							a.player.TogglePlay()
						} else {
							queue := snap.Queue
							if len(queue) == 0 {
								queue = []apple.Episode{ep}
							}
							a.playEpisode(ep, queue)
						}
					}

					if hasTranscript {
						if ui.Button(c.Key("ep-tr-btn"), "").Padding(8, 14).Radius(999).Children(func() {
							ui.Icon(c, iconQuote).Size(14, 14)
							ui.Text(c, "显示文稿").FontSize(13.5).FontWeight(500)
						}).Clicked() {
							a.setTranscriptMode(true)
						}
					}

					saved := a.isSaved(ep.ID)
					saveLabel := "收藏"
					if saved {
						saveLabel = "已收藏"
					}
					if ui.Button(c.Key("ep-save-btn"), "").Padding(8, 14).Radius(999).Children(func() {
						if saved {
							ui.Icon(c, iconBookmarkFill).Size(14, 14).TextColor(p.accent)
						} else {
							ui.Icon(c, iconBookmark).Size(14, 14)
						}
						ui.Text(c, saveLabel).FontSize(13.5)
					}).Clicked() {
						a.toggleSaveEpisode(ep)
					}

					if ep.ShowID != "" {
						if ui.Button(c.Key("ep-view-show"), "查看节目").Padding(8, 14).Radius(999).Clicked() {
							a.openShow(ep.ShowID)
						}
					}
				})
			})
		})

		ui.Divider(c)

		// Transcript Snippet / Mac Lyrics Launcher Card (ONLY shown when real transcript exists)
		if hasTranscript {
			trCard := ui.ButtonBase(c.Key("ep-tr-card")).FillWidth().Padding(14, 16).Radius(12).Background(p.card).Border(1, p.border).Justify(ui.Start).AlignItems(ui.Stretch).FocusRing(false).Cursor(ui.CursorPointer).Label("查看沉浸文稿")
			if trCard.Hovered() {
				trCard.Background(p.hover)
			}
			trCard.Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, iconQuote).Size(15, 15).TextColor(p.accent)
							ui.Text(c, "节目逐字文稿 (Transcript)").FontSize(14).FontWeight(700)
						})
						ui.Spacer(c)
						ui.Text(c, "进入沉浸视图 ›").FontSize(12.5).FontWeight(600).TextColor(p.accent)
					})
					previewCount := min(3, len(tr.Lines))
					for i := range previewCount {
						ui.Text(c, tr.Lines[i].Text).FontSize(13).TextColor(p.muted).SingleLine().TextAlign(ui.Start)
					}
				})
			})
			if trCard.Clicked() {
				a.setTranscriptMode(true)
			}
		}

		// Chapters / Timeline Section (parsed from description timestamps)
		if len(chapters) > 0 {
			ui.Column(c).Gap(8).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "章节时间轴").FontSize(16.5).FontWeight(700)
					ui.Text(c, "点击直接从该时间点播放").FontSize(12).TextColor(p.faint)
				})
				activeIdx := activeChapterIndex(chapters, snap, ep.ID)
				ui.Column(c).Gap(4).Children(func() {
					for idx, ln := range chapters {
						active := isCurrent && idx == activeIdx
						chBtn := ui.ButtonBase(c.Key(fmt.Sprintf("ch-%d", idx))).Padding(8, 12).Gap(12).Radius(9).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Cursor(ui.CursorPointer).Label(ln.Text)
						switch {
						case active:
							chBtn.Background(p.accent.Alpha(0.12))
						case chBtn.Hovered():
							chBtn.Background(p.hover)
						default:
							chBtn.Background(p.card)
						}
						chBtn.Children(func() {
							ui.Text(c, formatSeconds(ln.StartSec)).Width(58).FontSize(12.5).FontWeight(600).FontFeatures("tnum").TextColor(p.accent).SingleLine()
							txt := trimLeadingTimestamp(ln.Text)
							t := ui.Text(c, txt).FontSize(13.5).FontWeight(500).Grow(1).Shrink(1).MinWidth(0).TextAlign(ui.Start)
							if active {
								t.FontWeight(700).TextColor(p.accent)
							}
							ui.Icon(c, iconPlay).Size(11, 11).TextColor(p.faint)
						})
						if chBtn.Clicked() {
							queue := snap.Queue
							if len(queue) == 0 {
								queue = []apple.Episode{ep}
							}
							a.playEpisodeAt(ep, queue, ln.StartSec)
						}
					}
				})
			})
		}

		// Detailed Show Notes
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, "单集详情与介绍").FontSize(16.5).FontWeight(700)
			paras := apple.ParseHTMLParagraphs(ep.FullDescription, ep.Description)
			if len(paras) == 0 {
				ui.Text(c, "暂无单集详细介绍").FontSize(13.5).TextColor(p.muted)
			} else {
				ui.Column(c).Gap(6).Selectable().Children(func() {
					for _, para := range paras {
						ui.Text(c, para).FontSize(13.5).LineHeight(1.58).TextColor(p.text).TextAlign(ui.Start)
					}
				})
			}
			if link := cmpNonEmpty(ep.WebsiteURL, ep.URL); link != "" {
				ui.Row(c).Margin(6, 0, 0, 0).Gap(6).Children(func() {
					ui.Text(c, "节目原文链接:").FontSize(12.5).TextColor(p.muted)
					ui.Link(c.Key("ep-ext-link"), link, link).FontSize(12.5)
				})
			}
		})
	})
}

type immIntroMotion struct {
	ambientFade float32
	backY       float32
	backFade    float32
	scrubY      float32
	scrubFade   float32
	ctrlY       float32
	ctrlFade    float32
	metaY       float32
	metaFade    float32
}

// episodeTranscriptView renders a macOS Apple Music-style immersive Now Playing / Lyrics screen.
// When the episode has no actual transcript, only the large centered artwork and controls render.
func (a *App) episodeTranscriptView(c *ui.Context, _ palette, snap player.Snapshot) {
	ep, _, tr, loading := a.resolveEpisodeDetail(a.route.epID, snap)
	if ep.ID == "" {
		ui.Row(c).Fill().Center().Background(ui.Hex("#1c1518")).TextColor(ui.Hex("#ffffff")).Children(func() {
			ui.Spinner(c)
		})
		return
	}

	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	hasTranscript := tr.HasTranscript()
	showTranscript := hasTranscript && !a.hideImmersiveText
	bgTopLeft, bgBotRight, glowColor := lyricsBackdropColors(cmpNonEmpty(ep.BgColor, ep.HeroBgColor, ep.ShowBgColor))

	root := ui.Box(c).Fill().LinearGradient(ui.LinearGradient{
		From:  bgTopLeft,
		To:    bgBotRight,
		Angle: 135,
		Oklab: true,
	})

	intro := immIntroMotion{
		ambientFade: 1,
		backFade:    1,
		scrubFade:   1,
		ctrlFade:    1,
		metaFade:    1,
	}
	if a.immRun > 0 {
		tl := root.Timeline("imm-intro", ui.TimelineOptions{Run: a.immRun})
		intro.ambientFade = tl.Tween(ui.Tween{From: 0.35, To: 1, Duration: 420 * time.Millisecond, Ease: ui.EaseOut}, ui.AtTime(0))
		intro.backY = tl.Tween(ui.Tween{From: -10, To: 0, Duration: 280 * time.Millisecond, Ease: ui.EaseOut}, ui.AtTime(60*time.Millisecond))
		intro.backFade = tl.Tween(ui.Tween{From: 0, To: 1, Duration: 240 * time.Millisecond, Ease: ui.EaseOut}, ui.WithPrevious(0))
		intro.scrubY = tl.Tween(ui.Tween{From: 16, To: 0, Duration: 320 * time.Millisecond, Ease: ui.EaseOut}, ui.AtTime(100*time.Millisecond))
		intro.scrubFade = tl.Tween(ui.Tween{From: 0, To: 1, Duration: 280 * time.Millisecond, Ease: ui.EaseOut}, ui.WithPrevious(0))
		intro.ctrlY = tl.Tween(ui.Tween{From: 22, To: 0, Duration: 340 * time.Millisecond, Ease: ui.EaseOut}, ui.AtTime(140*time.Millisecond))
		intro.ctrlFade = tl.Tween(ui.Tween{From: 0, To: 1, Duration: 300 * time.Millisecond, Ease: ui.EaseOut}, ui.WithPrevious(0))
		intro.metaY = tl.Tween(ui.Tween{From: 16, To: 0, Duration: 320 * time.Millisecond, Ease: ui.EaseOut}, ui.AtTime(190*time.Millisecond))
		intro.metaFade = tl.Tween(ui.Tween{From: 0, To: 1, Duration: 280 * time.Millisecond, Ease: ui.EaseOut}, ui.WithPrevious(0))
	}

	root.Children(func() {
		// In-flow background ambient layers (z=0) with breathing radial light while playing
		ambient := ui.Box(c).Fill().ZIndex(0)
		if intro.ambientFade < 1 {
			ambient.Opacity(intro.ambientFade)
		}
		ambient.Children(func() {
			if art := a.episodeArtworkURL(ep); art != "" {
				ui.Box(c).Fill().Opacity(0.22).Blur(68).Children(func() {
					netimage.Image(c, art, netimage.Options{MaxSide: 400}).Fill()
				})
			}
			playing := isCurrent && snap.Playing
			reduceMotion := c.Preferences().ReduceMotion
			ui.Box(c).Absolute().Fill().Draw(func(pt *ui.Painter, r ui.Rect) {
				cx1, cy1 := float32(0.84), float32(0.18)
				cx2, cy2 := float32(0.18), float32(0.82)
				a1, a2 := float32(0.28), float32(0.14)
				if playing && !reduceMotion {
					pt.AnimationFrameRate(30)
					t := float64(pt.Now().UnixMilli()) / 1000.0
					cx1 += float32(math.Sin(t*0.42)) * 0.06
					cy1 += float32(math.Cos(t*0.33)) * 0.06
					cx2 += float32(math.Cos(t*0.37)) * 0.05
					cy2 += float32(math.Sin(t*0.29)) * 0.05
					a1 = 0.24 + 0.10*float32(0.5+0.5*math.Sin(t*0.65))
					a2 = 0.10 + 0.07*float32(0.5+0.5*math.Cos(t*0.52))
				}
				pt.FillRadialGradient(r, ui.RadialGradient{
					Center: ui.Point{X: cx1, Y: cy1},
					Radius: max(r.W, r.H) * 0.65,
					Oklab:  true,
					Stops: []ui.GradientStop{
						{Color: glowColor.Alpha(a1), At: 0},
						{Color: glowColor.Alpha(a1 * 0.35), At: 0.45},
						{Color: ui.Transparent, At: 0.90},
					},
				}, 0)
				pt.FillRadialGradient(r, ui.RadialGradient{
					Center: ui.Point{X: cx2, Y: cy2},
					Radius: max(r.W, r.H) * 0.55,
					Oklab:  true,
					Stops: []ui.GradientStop{
						{Color: glowColor.Alpha(a2), At: 0},
						{Color: ui.Transparent, At: 0.80},
					},
				}, 0)
			})
		})

		// Foreground interactive UI layer (z=1)
		ui.Box(c).Absolute().Fill().ZIndex(1).Children(func() {
			// Top-left back button
			backWrap := ui.Box(c).Absolute().Top(14).Left(18).ZIndex(2)
			if intro.backY != 0 || intro.backFade < 1 {
				backWrap.Translate(0, intro.backY).Opacity(intro.backFade)
			}
			backWrap.Children(func() {
				backBtn := ui.ButtonBase(c.Key("tr-back")).Height(30).Padding(0, 12).Gap(5).Radius(999).Center().Background(ui.RGBA(255, 255, 255, 0.14)).TextColor(ui.RGBA(255, 255, 255, 0.92)).FocusRing(false).Cursor(ui.CursorPointer).Label("返回详情").Transition(animUIHover)
				if backBtn.Hovered() {
					backBtn.Background(ui.RGBA(255, 255, 255, 0.24))
				}
				applyTapPose(backBtn, 1.04, 0)
				backBtn.Children(func() {
					ui.Icon(c, iconBack).Size(14, 14)
					ui.Text(c, "单集详情").FontSize(12.5).FontWeight(500)
				})
				if backBtn.Clicked() {
					a.setTranscriptMode(false)
				}
			})

			// Unified responsive stage: glides smoothly between centered standalone cover and split transcript view
			coverSize := float32(336)
			splitRow := ui.Row(c.Key("imm-split-" + ep.ID)).Fill().AlignItems(ui.Center)
			if showTranscript {
				coverSize = 296
				splitRow.Justify(ui.Start).Padding(32, 48, 28).Gap(54)
			} else {
				splitRow.Justify(ui.Center).Padding(42, 32, 28).Gap(0)
			}

			splitRow.Children(func() {
				a.immersivePlayerColumn(c, ep, snap, isCurrent, hasTranscript, showTranscript, loading, coverSize, intro)

				if !showTranscript {
					return
				}

				// RIGHT COLUMN: Iconic macOS Apple Music large lyric/transcript stream
				trPanel := ui.Scroll(c.Key("tr-scroll-"+ep.ID)).TrackScroll(&a.transcriptScroll).Grow(1).Shrink(1).MinWidth(0).FillHeight().Padding(40, 12, 80, 0).Transition(ui.ElementTransition{
					Animation: &animLayoutSpring,
					Enter:     &ui.Motion{X: 38, Blur: 8},
					Exit:      &ui.Motion{X: 38, Blur: 8},
				})
				trPanel.Children(func() {
					ui.Row(c).Gap(7).Margin(0, 0, 18, 10).AlignItems(ui.Center).Children(func() {
						dotAlpha := float32(0.88)
						if isCurrent && snap.Playing {
							dotAlpha = 0.55 + 0.45*c.Root().Loop("lyrics-dots", 2400*time.Millisecond, ui.Bounce(ui.EaseInOut))
						}
						ui.Box(c).Size(7, 7).Radius(999).Background(ui.RGBA(255, 255, 255, dotAlpha))
						ui.Box(c).Size(7, 7).Radius(999).Background(ui.RGBA(255, 255, 255, 0.42))
						ui.Box(c).Size(7, 7).Radius(999).Background(ui.RGBA(255, 255, 255, 0.42))
						if loading {
							ui.Row(c).Gap(6).Margin(0, 0, 0, 10).AlignItems(ui.Center).Children(func() {
								ui.Spinner(c).Size(12, 12)
								ui.Text(c, "加载文稿中...").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.65))
							})
						}
					})

					activeIdx := activeTranscriptLine(tr, snap, ep.ID)
					if activeIdx < 0 && len(tr.Lines) > 0 {
						activeIdx = 0
					}

					ui.Column(c).FillWidth().Gap(10).Children(func() {
						for idx, ln := range tr.Lines {
							active := idx == activeIdx
							past := idx < activeIdx

							fg := ui.RGBA(255, 255, 255, 0.42)
							if active {
								fg = ui.RGBA(255, 255, 255, 0.96)
							} else if past {
								fg = ui.RGBA(255, 255, 255, 0.30)
							}

							lineBtn := ui.ButtonBase(c.Key(fmt.Sprintf("trl-%s-%d", ep.ID, idx))).FillWidth().Padding(8, 12).Radius(12).Justify(ui.Start).AlignItems(ui.Start).FocusRing(false).TextColor(fg).Cursor(ui.CursorPointer).Label(ln.Text).Transition(ui.ElementTransition{
								Colors:   true,
								Duration: 220 * time.Millisecond,
							})
							if lineBtn.Hovered() {
								lineBtn.Background(ui.RGBA(255, 255, 255, 0.08)).TextColor(ui.RGBA(255, 255, 255, 0.88))
							} else if active && ln.HasTimestamp {
								lineBtn.Background(ui.RGBA(255, 255, 255, 0.06))
							}

							lineBtn.Children(func() {
								ui.Column(c).FillWidth().AlignItems(ui.Start).Gap(3).Children(func() {
									if ln.HasTimestamp {
										tag := formatSeconds(ln.StartSec)
										if ln.Speaker != "" {
											tag += " · " + ln.Speaker
										}
										ui.Text(c, tag).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(fg.Alpha(0.75))
									} else if ln.Speaker != "" {
										ui.Text(c, ln.Speaker).FontSize(12.5).FontWeight(600).TextColor(fg.Alpha(0.75))
									}
									bodyTxt := ln.Text
									if ln.HasTimestamp {
										bodyTxt = trimLeadingTimestamp(bodyTxt)
									}
									ui.Text(c, bodyTxt).FontSize(23).FontWeight(700).LineHeight(1.36).TextAlign(ui.Start)
								})
							})

							if active && isCurrent && snap.Playing && a.lastSyncedLine != activeIdx {
								a.lastSyncedLine = activeIdx
								lineBtn.ScrollIntoView(ui.ScrollIntoViewOptions{
									Block:     ui.ScrollCenter,
									Animation: &ui.Smooth,
								})
							}

							if lineBtn.Clicked() {
								queue := snap.Queue
								if len(queue) == 0 {
									queue = []apple.Episode{ep}
								}
								if ln.HasTimestamp {
									a.playEpisodeAt(ep, queue, ln.StartSec)
									a.lastSyncedLine = idx
								} else if ep.DurationMS > 0 && len(tr.Lines) > 1 {
									estSec := float64(idx) / float64(len(tr.Lines)) * float64(ep.DurationMS) / 1000.0
									a.playEpisodeAt(ep, queue, estSec)
								}
							}
						}
					})
				})
			})
		})
	})
}

func (a *App) immersivePlayerColumn(
	c *ui.Context,
	ep apple.Episode,
	snap player.Snapshot,
	isCurrent, hasTranscript, showTranscript, loading bool,
	coverSize float32,
	intro immIntroMotion,
) {
	playerCol := ui.Column(c.Key("imm-player-" + ep.ID)).Width(coverSize + 8).Shrink(0).AlignItems(ui.Center).Gap(16).Transition(ui.ElementTransition{
		Position:  true,
		Size:      true,
		Animation: &animLayoutSpring,
	})
	playerCol.Children(func() {
		ui.Box(c.Key("imm-cover-"+ep.ID)).Size(coverSize, coverSize).Radius(16).Shadow(0, 22, 52, -8, ui.RGBA(0, 0, 0, 0.58)).Clip().Transition(ui.ElementTransition{
			Animation: &animHeroFlight,
			Match:     artworkHeroMatch{ep.ID},
			Fly:       true,
			Position:  true,
			Size:      true,
		}).Children(func() {
			netimage.Image(c, a.episodeArtworkURL(ep), netimage.Options{MaxSide: 400}).Fill()
		})

		// Enlarged scrubber + elapsed/remaining timestamps
		scrubCol := ui.Column(c).Width(coverSize).Gap(5)
		if intro.scrubY != 0 || intro.scrubFade < 1 {
			scrubCol.Translate(0, intro.scrubY).Opacity(intro.scrubFade)
		}
		scrubCol.Children(func() {
			curSec, totalSec := a.episodeProgressSecs(ep, snap)
			if isCurrent && a.trScrub.active {
				curSec = a.trScrub.val
			}
			a.scrubberWithColors(
				c.Key("tr-scrub"),
				&a.trScrub,
				snap,
				isCurrent,
				curSec,
				totalSec,
				ui.RGBA(255, 255, 255, 0.22),
				ui.RGBA(255, 255, 255, 0.92),
				ui.RGBA(255, 255, 255, 0.36),
				ui.Hex("#ffffff"),
				5,
				7,
			).FillWidth().Height(18).Disabled(!isCurrent)

			remSec := max(0.0, totalSec-curSec)
			ui.Row(c).FillWidth().Children(func() {
				ui.Text(c, formatSeconds(curSec)).FontSize(12).FontFeatures("tnum").TextColor(ui.RGBA(255, 255, 255, 0.72))
				ui.Spacer(c)
				ui.Text(c, "-"+formatSeconds(remSec)).FontSize(12).FontFeatures("tnum").TextColor(ui.RGBA(255, 255, 255, 0.72))
			})
		})

		// Large macOS-style transport controls row: [1x]   [⏮  ⏸/▶  ⏭]   [💬]
		ctrlRow := ui.Row(c).Width(coverSize).Height(58).Justify(ui.SpaceBetween).AlignItems(ui.Center)
		if intro.ctrlY != 0 || intro.ctrlFade < 1 {
			ctrlRow.Translate(0, intro.ctrlY).Opacity(intro.ctrlFade)
		}
		ctrlRow.Children(func() {
			moreBtn := ui.ButtonBase(c.Key("tr-more")).Height(36).Padding(0, 12).Radius(999).Center().FocusRing(false).TextColor(ui.RGBA(255, 255, 255, 0.80)).Cursor(ui.CursorPointer).Label("切换倍速").Tooltip("切换播客倍速").Transition(animUIHover)
			if moreBtn.Hovered() {
				moreBtn.Background(ui.RGBA(255, 255, 255, 0.16)).TextColor(ui.Hex("#ffffff"))
			}
			applyTapPose(moreBtn, 1.05, 0)
			moreBtn.Children(func() {
				ui.Textf(c, "%.2gx", snap.Speed).FontSize(14).FontWeight(700).ContentTransition(ui.ContentTransition{Numeric: true})
			})
			if moreBtn.Clicked() {
				next := a.player.CycleSpeed()
				a.updateState(func() { a.state.Speed = next })
			}

			ui.Row(c).Gap(22).AlignItems(ui.Center).Children(func() {
				rewBtn := ui.ButtonBase(c.Key("tr-rew")).Size(46, 46).Radius(999).Center().FocusRing(false).TextColor(ui.RGBA(255, 255, 255, 0.92)).Cursor(ui.CursorPointer).Label("快退 15 秒").Tooltip("快退 15 秒").Transition(animUIHover)
				if rewBtn.Hovered() {
					rewBtn.Background(ui.RGBA(255, 255, 255, 0.16))
				}
				applyTapPose(rewBtn, 1.08, 0)
				rewBtn.Children(func() { ui.Icon(c, iconPrev).Size(24, 24) })
				if rewBtn.Clicked() && isCurrent {
					a.player.Skip(-15 * time.Second)
				}

				playBtn := ui.ButtonBase(c.Key("tr-play")).Size(58, 58).Radius(999).Center().FocusRing(false).Background(ui.RGBA(255, 255, 255, 0.16)).TextColor(ui.Hex("#ffffff")).Cursor(ui.CursorPointer).Label("播放/暂停").Transition(animUIHover)
				if playBtn.Hovered() {
					playBtn.Background(ui.RGBA(255, 255, 255, 0.26))
				}
				applyTapPose(playBtn, 1.07, 0)
				playBtn.Children(func() {
					if isCurrent && snap.Playing {
						ui.Icon(c, iconPause).Size(28, 28).ContentTransition(ui.ContentTransition{})
					} else {
						ui.Icon(c, iconPlay).Size(28, 28).ContentTransition(ui.ContentTransition{})
					}
				})
				if playBtn.Clicked() {
					if isCurrent {
						a.player.TogglePlay()
					} else {
						queue := snap.Queue
						if len(queue) == 0 {
							queue = []apple.Episode{ep}
						}
						a.playEpisode(ep, queue)
					}
				}

				fwdBtn := ui.ButtonBase(c.Key("tr-fwd")).Size(46, 46).Radius(999).Center().FocusRing(false).TextColor(ui.RGBA(255, 255, 255, 0.92)).Cursor(ui.CursorPointer).Label("快进 30 秒").Tooltip("快进 30 秒").Transition(animUIHover)
				if fwdBtn.Hovered() {
					fwdBtn.Background(ui.RGBA(255, 255, 255, 0.16))
				}
				applyTapPose(fwdBtn, 1.08, 0)
				fwdBtn.Children(func() { ui.Icon(c, iconNext).Size(24, 24) })
				if fwdBtn.Clicked() && isCurrent {
					a.player.Skip(30 * time.Second)
				}
			})

			quoteBtn := ui.ButtonBase(c.Key("tr-quote")).Size(40, 40).Radius(10).Center().FocusRing(false).Disabled(!hasTranscript).Label("文稿开关").Transition(animUIHover)
			switch {
			case !hasTranscript:
				quoteBtn.TextColor(ui.RGBA(255, 255, 255, 0.24)).Tooltip("该单集暂无文稿内容")
			case showTranscript:
				quoteBtn.Background(ui.RGBA(255, 255, 255, 0.26)).TextColor(ui.Hex("#ffffff")).Cursor(ui.CursorPointer).Tooltip("收起文稿")
				applyTapPose(quoteBtn, 1.06, 0)
			default:
				quoteBtn.TextColor(ui.RGBA(255, 255, 255, 0.85)).Cursor(ui.CursorPointer).Tooltip("显示文稿")
				if quoteBtn.Hovered() {
					quoteBtn.Background(ui.RGBA(255, 255, 255, 0.16))
				}
				applyTapPose(quoteBtn, 1.06, 0)
			}
			quoteBtn.Children(func() { ui.Icon(c, iconQuote).Size(20, 20) })
			if quoteBtn.Clicked() && hasTranscript {
				a.hideImmersiveText = !a.hideImmersiveText
			}
		})

		metaCol := ui.Column(c).Width(coverSize).AlignItems(ui.Center).Gap(4)
		if intro.metaY != 0 || intro.metaFade < 1 {
			metaCol.Translate(0, intro.metaY).Opacity(intro.metaFade)
		}
		metaCol.Children(func() {
			ui.Text(c, ep.Name).FontSize(17.5).FontWeight(700).TextColor(ui.RGBA(255, 255, 255, 0.94)).MaxLines(2).LineHeight(1.25).TextAlign(ui.Center)
			if sub := cmpNonEmpty(ep.ShowName, ep.ArtistName); sub != "" {
				ui.Text(c, sub).FontSize(13.5).TextColor(ui.RGBA(255, 255, 255, 0.58)).SingleLine().TextAlign(ui.Center)
			}
			if loading {
				ui.Text(c, "正在检查文稿...").FontSize(11.5).TextColor(ui.RGBA(255, 255, 255, 0.45))
			}
		})
	})
}

func (a *App) showCard(c *ui.Context, p palette, show apple.Show) {
	card := ui.ButtonBase(c).Padding(6).Radius(11).FocusRing(false).Label(show.Name).Cursor(ui.CursorPointer).Transition(animUIHover)
	if card.Hovered() {
		card.Background(p.hover)
	}
	applyTapPose(card, 1.015, -2)
	card.Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Stretch).Gap(6).Children(func() {
			ui.Box(c).FillWidth().AspectRatio(1).Radius(9).Shadow(0, 5, 14, -2, p.shade).Clip().Children(func() {
				netimage.Image(c, apple.FormatArtwork(show.ArtworkURL, 600), netimage.Options{MaxSide: 400}).Fill()
			})
			ui.Column(c).AlignItems(ui.Start).Gap(1).Children(func() {
				ui.Text(c, show.Name).FontSize(13).FontWeight(600).SingleLine().TextAlign(ui.Start)
				ui.Text(c, show.ArtistName).FontSize(11.5).TextColor(p.muted).SingleLine().TextAlign(ui.Start)
			})
		})
	})
	if card.Clicked() {
		a.openShow(show.ID)
	}
}

func (a *App) episodeRow(c *ui.Context, p palette, ep apple.Episode, queue []apple.Episode, snap player.Snapshot) {
	a.episodeRowWithKey(c, p, "row-"+ep.ID, ep, queue, snap)
}

func (a *App) episodeRowWithKey(c *ui.Context, p palette, menuKey string, ep apple.Episode, queue []apple.Episode, snap player.Snapshot) {
	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	row := ui.ButtonBase(c).Padding(9, 12).Gap(12).Radius(10).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Label(ep.Name).Cursor(ui.CursorPointer).Transition(animUIHover)
	switch {
	case isCurrent:
		row.Background(p.accent.Alpha(0.10))
	case row.Hovered():
		row.Background(p.hover)
	}

	var playClicked, showClicked, saveClicked, moreClicked bool
	row.Children(func() {
		coverBtn := ui.ButtonBase(c.Key("row-play")).Size(48, 48).Shrink(0).Radius(8).Clip().FocusRing(false).Label("播放").Cursor(ui.CursorPointer)
		coverBtn.Children(func() {
			netimage.Image(c, a.episodeArtworkURL(ep), netimage.Options{MaxSide: 400}).Fill()
			if isCurrent || coverBtn.Hovered() {
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(ui.RGBA(0, 0, 0, 0.40)).Center().TextColor(ui.Hex("#ffffff")).Children(func() {
					if isCurrent && snap.Playing {
						ui.Icon(c, iconPause).Size(17, 17)
					} else {
						ui.Icon(c, iconPlay).Size(17, 17)
					}
				})
			}
		})
		if coverBtn.Clicked() {
			playClicked = true
		}

		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(2).Children(func() {
			title := ui.Text(c, ep.Name).FontSize(13.5).FontWeight(600).SingleLine().TextAlign(ui.Start)
			if isCurrent {
				title.TextColor(p.accent)
			}

			var meta []string
			if ep.ShowName != "" {
				meta = append(meta, ep.ShowName)
			}
			if d := apple.FormatDate(ep.ReleaseDate); d != "" {
				meta = append(meta, d)
			}
			if ep.DurationMS > 0 {
				meta = append(meta, apple.FormatDurationMS(ep.DurationMS))
			}
			if ep.IsSubscriberOnly {
				meta = append(meta, "订阅专享")
			}
			progLabel, progFrac, progCompleted, hasProg := a.episodeProgressDisplay(ep, snap)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if len(meta) > 0 {
					ui.Text(c, strings.Join(meta, " · ")).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0).TextAlign(ui.Start)
				}
				if hasProg {
					a.inlineProgressBadge(c, p, progLabel, progFrac, progCompleted)
				}
			})

			if desc := compactEpisodeSummary(ep); desc != "" {
				ui.Text(c, desc).FontSize(11.5).TextColor(p.faint).SingleLine().TextAlign(ui.Start)
			}
		})

		ui.Row(c).Gap(4).Shrink(0).AlignItems(ui.Center).Children(func() {
			if ep.ShowID != "" && a.route.page != pageShow {
				if ui.Button(c.Key("show"), "").Label("查看节目").Tooltip("打开所属节目").Children(func() {
					ui.Text(c, "节目").FontSize(11.5)
				}).Clicked() {
					showClicked = true
				}
			}

			saved := a.isSaved(ep.ID)
			saveIcon := iconBookmark
			saveColor := p.muted
			if saved {
				saveIcon = iconBookmarkFill
				saveColor = p.accent
			}
			saveBtn := ui.ButtonBase(c.Key("save")).Size(30, 30).Radius(999).Center().TextColor(saveColor).FocusRing(false).Label("收藏单集").Tooltip("收藏/取消收藏")
			if saveBtn.Hovered() {
				saveBtn.Background(p.hover)
			}
			saveBtn.Children(func() { ui.Icon(c, saveIcon).Size(14, 14) })
			if saveBtn.Clicked() {
				saveClicked = true
			}

			moreClicked = a.episodeMoreMenuButton(c.Key("more"), menuKey, ep, queue, snap, episodeMoreStyle{
				size:     30,
				iconSize: 15,
				fg:       p.muted,
				hoverBg:  p.hover,
				activeBg: p.accent.Alpha(0.18),
				activeFg: p.accent,
				label:    "更多操作 " + ep.Name,
			})
		})
	})

	switch {
	case playClicked:
		if isCurrent {
			a.player.TogglePlay()
		} else {
			a.playEpisode(ep, queue)
		}
	case showClicked:
		a.openShow(ep.ShowID)
	case saveClicked:
		a.toggleSaveEpisode(ep)
	case moreClicked:
		// Handled by episodeMoreMenuButton
	case row.Clicked():
		if isCurrent {
			a.player.TogglePlay()
		} else {
			a.playEpisode(ep, queue)
		}
	}
}

type episodeMoreStyle struct {
	size     float32
	iconSize float32
	fg       ui.Color
	idleBg   ui.Color
	hoverBg  ui.Color
	activeBg ui.Color
	activeFg ui.Color
	label    string
}

func (a *App) beginEpisodeMenu() {
	a.epMenu.seenHover = false
	a.epMenu.seenOpen = false
}

func (a *App) finishEpisodeMenu() {
	m := &a.epMenu
	if !m.seenHover {
		m.hoverID = ""
		m.hoverSince = time.Time{}
		m.dismissedHoverID = ""
	}
	if m.openID != "" && !m.seenOpen {
		m.openID = ""
		m.pinned = false
		m.leaveSince = time.Time{}
	}
}

// episodeMoreMenuButton renders a "..." trigger whose floating frosted Apple-style menu:
//   - opens transiently after the cursor dwells on "..." for epMenuHoverDelay and
//     auto-dismisses once the cursor leaves both trigger and menu panel;
//   - opens and pins persistently on left-click (or upgrades a hover-preview menu to pinned),
//     staying visible until clicking an item, clicking outside, pressing Escape, or clicking "..." again.
func (a *App) episodeMoreMenuButton(
	c *ui.Context,
	menuID string,
	ep apple.Episode,
	queue []apple.Episode,
	snap player.Snapshot,
	style episodeMoreStyle,
) bool {
	now := c.Now()
	m := &a.epMenu

	btn := ui.ButtonBase(c).
		Size(style.size, style.size).
		Radius(999).
		Center().
		FocusRing(false).
		TextColor(style.fg).
		Label(style.label).
		Cursor(ui.CursorPointer).
		Transition(animUIHover)
	applyTapPose(btn, 1.06, 0)

	triggerHovered := btn.Hovered()
	clicked := btn.Clicked()

	if triggerHovered {
		if m.hoverID != menuID {
			m.hoverID = menuID
			m.hoverSince = now
		}
		m.seenHover = true
	} else {
		if m.hoverID == menuID {
			m.hoverID = ""
			m.hoverSince = time.Time{}
		}
		if m.dismissedHoverID == menuID {
			m.dismissedHoverID = ""
		}
	}

	if clicked {
		if m.openID == menuID && m.pinned {
			m.openID = ""
			m.pinned = false
			m.leaveSince = time.Time{}
			m.dismissedHoverID = menuID
		} else {
			m.openID = menuID
			m.pinned = true
			m.leaveSince = time.Time{}
			m.dismissedHoverID = ""
		}
	} else if triggerHovered && !m.pinned && m.dismissedHoverID != menuID && m.openID != menuID {
		elapsed := now.Sub(m.hoverSince)
		if elapsed >= epMenuHoverDelay {
			m.openID = menuID
			m.pinned = false
			m.leaveSince = time.Time{}
		} else {
			c.After(epMenuHoverDelay - elapsed)
		}
	}

	// Dismiss transient hover-dwell menu once the pointer has been outside trigger & panel for epMenuLeaveGrace.
	if m.openID == menuID && !m.pinned && !triggerHovered && !m.leaveSince.IsZero() && now.Sub(m.leaveSince) >= epMenuLeaveGrace {
		m.openID = ""
		m.leaveSince = time.Time{}
	}

	isOpen := m.openID == menuID
	switch {
	case isOpen:
		btn.Background(style.activeBg).TextColor(style.activeFg)
	case triggerHovered:
		btn.Background(style.hoverBg).TextColor(style.activeFg)
	case style.idleBg.A > 0:
		btn.Background(style.idleBg)
	}
	btn.Children(func() {
		ui.Icon(c, iconMore).Size(style.iconSize, style.iconSize)
	})

	if isOpen {
		m.seenOpen = true
		a.renderEpisodeMenuOverlay(c, btn, menuID, ep, queue, snap, triggerHovered)
	}

	return clicked
}

func (a *App) renderEpisodeMenuOverlay(
	c *ui.Context,
	trigger ui.Element,
	menuID string,
	ep apple.Episode,
	queue []apple.Episode,
	snap player.Snapshot,
	triggerHovered bool,
) {
	m := &a.epMenu
	now := c.Now()
	open := true
	panelHovered := false
	actionTaken := false

	dark := c.Theme().Dark
	menuBg := ui.RGBA(248, 248, 252, 0.86)
	menuBorder := ui.RGBA(0, 0, 0, 0.10)
	menuText := ui.Hex("#1d1d1f")
	menuHover := ui.RGBA(0, 0, 0, 0.07)
	menuSep := ui.RGBA(0, 0, 0, 0.09)
	menuShadow := ui.RGBA(0, 0, 0, 0.18)
	if dark {
		menuBg = ui.RGBA(34, 34, 38, 0.78)
		menuBorder = ui.RGBA(255, 255, 255, 0.13)
		menuText = ui.RGBA(255, 255, 255, 0.94)
		menuHover = ui.RGBA(255, 255, 255, 0.11)
		menuSep = ui.RGBA(255, 255, 255, 0.10)
		menuShadow = ui.RGBA(0, 0, 0, 0.52)
	}

	isCurrent := snap.Episode != nil && snap.Episode.ID == ep.ID
	prog, hasProg := a.episodeProgress(ep.ID)
	isCompleted := hasProg && prog.Completed
	playLabel := "播放单集"
	switch {
	case isCurrent && snap.Playing:
		playLabel = "暂停播放"
	case isCurrent && snap.Position > 5*time.Second:
		playLabel = "继续播放 " + formatSeconds(snap.Position.Seconds())
	case hasProg && !prog.Completed && prog.PositionSec > 5:
		playLabel = "继续播放 " + formatSeconds(prog.PositionSec)
	case isCompleted:
		playLabel = "重新播放"
	}

	saved := a.isSaved(ep.ID)
	saveLabel := "收藏单集"
	if saved {
		saveLabel = "取消收藏"
	}
	playedLabel := "标记为已播完"
	if isCompleted {
		playedLabel = "标记为未播"
	}

	ui.PopoverBase(c.Key("ep-menu-"+menuID), trigger, &open, func(panel ui.Element) {
		panel.Role(ui.RoleMenu).
			AttachTo(trigger, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(6, 0, 0, 0).
			Width(220).
			Radius(12).
			Shadow(0, 14, 36, -4, menuShadow).
			Transition(ui.ElementTransition{
				Animation: &animMenuPop,
				Enter:     &ui.Motion{Scale: 0.94, Y: -6, Blur: 4},
				Exit:      &ui.Motion{Scale: 0.96, Y: -4},
			})

		panelHovered = panel.Hovered()

		// Frosted backdrop blur layer (z=0)
		ui.Box(c).Absolute().Fill().Radius(12).Material(glass.Blur{Radius: 24}).ZIndex(0)

		// In-flow translucent Apple Music-style menu list (z=1)
		ui.Column(c).FillWidth().ZIndex(1).Radius(12).Background(menuBg).Border(1, menuBorder).Padding(5, 5).Gap(1).Children(func() {
			if a.episodeMenuItem(c.Key("mi-play"), playLabel, menuText, menuHover) {
				actionTaken = true
				open = false
				if isCurrent {
					a.player.TogglePlay()
				} else {
					a.playEpisode(ep, queue)
				}
			}
			if a.episodeMenuItem(c.Key("mi-next"), "插播", menuText, menuHover) {
				actionTaken = true
				open = false
				a.enqueueEpisodeNext(ep)
			}
			if a.episodeMenuItem(c.Key("mi-queue"), "添加到队列", menuText, menuHover) {
				actionTaken = true
				open = false
				a.enqueueEpisodeLast(ep)
			}

			episodeMenuSep(c, menuSep)

			if a.episodeMenuItem(c.Key("mi-save"), saveLabel, menuText, menuHover) {
				actionTaken = true
				open = false
				a.toggleSaveEpisode(ep)
			}
			if a.episodeMenuItem(c.Key("mi-played"), playedLabel, menuText, menuHover) {
				actionTaken = true
				open = false
				a.setEpisodePlayed(ep, !isCompleted)
			}

			episodeMenuSep(c, menuSep)

			if a.episodeMenuItem(c.Key("mi-detail"), "单集详情", menuText, menuHover) {
				actionTaken = true
				open = false
				a.openEpisode(ep, false)
			}
			if a.episodeMenuItem(c.Key("mi-imm"), "沉浸播放视图", menuText, menuHover) {
				actionTaken = true
				open = false
				a.openEpisode(ep, true)
			}
			if ep.ShowID != "" && !(a.route.page == pageShow && a.route.showID == ep.ShowID) {
				if a.episodeMenuItem(c.Key("mi-show"), "前往节目", menuText, menuHover) {
					actionTaken = true
					open = false
					a.openShow(ep.ShowID)
				}
			}

			episodeMenuSep(c, menuSep)

			if a.episodeMenuItem(c.Key("mi-copy"), "复制单集链接", menuText, menuHover) {
				actionTaken = true
				open = false
				a.copyEpisodeLink(ep)
			}
		})
	})

	if !open {
		m.openID = ""
		m.pinned = false
		m.leaveSince = time.Time{}
		if actionTaken || triggerHovered {
			m.dismissedHoverID = menuID
		}
		return
	}

	if !m.pinned {
		if triggerHovered || panelHovered {
			m.leaveSince = time.Time{}
		} else if m.leaveSince.IsZero() {
			m.leaveSince = now
			c.After(epMenuLeaveGrace)
		} else if rem := epMenuLeaveGrace - now.Sub(m.leaveSince); rem > 0 {
			c.After(rem)
		}
	}
}

func (a *App) episodeMenuItem(c *ui.Context, label string, textCol, hoverBg ui.Color) bool {
	item := ui.ButtonBase(c).
		Role(ui.RoleMenuItem).
		FillWidth().
		Height(32).
		Padding(0, 11).
		Radius(7).
		Justify(ui.Start).
		AlignItems(ui.Center).
		FocusRing(false).
		TextColor(textCol).
		Label(label).
		Cursor(ui.CursorPointer).
		Transition(animUIHover)
	if item.Hovered() {
		item.Background(hoverBg)
	}
	item.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).SingleLine().TextAlign(ui.Start)
	})
	return item.Clicked()
}

func episodeMenuSep(c *ui.Context, sepCol ui.Color) {
	ui.Box(c).FillWidth().Height(1).Margin(4, 9).Background(sepCol)
}

func (a *App) playerBarView(c *ui.Context, p palette, snap player.Snapshot) {
	bar := ui.Row(c.Key("player-bar")).Height(playerBarH).Shrink(0).PaddingX(16).Gap(16).AlignItems(ui.Center).Transition(ui.ElementTransition{
		Animation: &animLayoutSpring,
		Enter:     &ui.Motion{YPercent: 100},
		Exit:      &ui.Motion{YPercent: 100},
	})
	if !c.Vibrancy() {
		bar.Background(p.sidebar)
	}
	bar.Children(func() {
		// Left: clickable episode artwork + strictly left-aligned episode/show titles
		ui.Row(c).Grow(1).Basis(0).MinWidth(180).Gap(10).Justify(ui.Start).AlignItems(ui.Center).Children(func() {
			if snap.Episode == nil {
				ui.Box(c).Size(40, 40).Radius(7).Background(p.hover).Center().TextColor(p.faint).Children(func() {
					ui.Icon(c, iconPodcast).Size(18, 18)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(1).Children(func() {
					ui.Text(c, "未在播放").FontSize(12.5).FontWeight(600).TextColor(p.muted).SingleLine().TextAlign(ui.Start)
					ui.Text(c, "选择任意播客单集开始收听").FontSize(11).TextColor(p.faint).SingleLine().TextAlign(ui.Start)
				})
				return
			}

			ep := snap.Episode
			showingSameDetail := a.route.page == pageEpisode && a.route.epID == ep.ID && !a.route.transcript
			coverBtn := ui.ButtonBase(c.Key("bar-cover-"+ep.ID)).Size(40, 40).Shrink(0).Radius(7).Clip().FocusRing(false).Tooltip("查看单集详情与文稿").Label(ep.Name).Cursor(ui.CursorPointer)
			if !showingSameDetail {
				coverBtn.Transition(ui.ElementTransition{
					Animation: &animHeroFlight,
					Match:     artworkHeroMatch{ep.ID},
					Fly:       true,
				})
			}
			applyTapPose(coverBtn, 1.05, 0)
			coverBtn.Children(func() {
				netimage.Image(c, a.episodeArtworkURL(*ep), netimage.Options{MaxSide: 400}).Fill()
				if coverBtn.Hovered() {
					ui.Box(c).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.32)).Center().TextColor(ui.Hex("#ffffff")).Children(func() {
						ui.Icon(c, iconInfo).Size(15, 15)
					})
				}
			})
			if coverBtn.Clicked() {
				a.openEpisode(*ep, false)
			}

			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Start).Gap(1).Children(func() {
				titleBtn := ui.ButtonBase(c.Key("bar-title")).Shrink(1).MinWidth(0).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Label(ep.Name).Cursor(ui.CursorPointer)
				titleBtn.Children(func() {
					ui.Text(c, ep.Name).FontSize(12.5).FontWeight(600).SingleLine().Shrink(1).MinWidth(0).TextAlign(ui.Start)
				})
				if titleBtn.Clicked() {
					a.openEpisode(*ep, false)
				}

				switch {
				case snap.Loading:
					ui.Text(c, "正在缓冲音频流...").FontSize(11).TextColor(p.accent).SingleLine().TextAlign(ui.Start)
				case snap.Err != "":
					ui.Text(c, snap.Err).FontSize(11).TextColor(c.Theme().Danger).SingleLine().TextAlign(ui.Start)
				default:
					sub := cmpNonEmpty(ep.ShowName, ep.ArtistName)
					if ep.ShowID != "" {
						showBtn := ui.ButtonBase(c.Key("bar-show")).Shrink(1).MinWidth(0).Justify(ui.Start).AlignItems(ui.Center).FocusRing(false).Label(sub).Cursor(ui.CursorPointer)
						showBtn.Children(func() {
							ui.Text(c, sub).FontSize(11).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0).TextAlign(ui.Start)
						})
						if showBtn.Clicked() {
							a.openShow(ep.ShowID)
						}
					} else if sub != "" {
						ui.Text(c, sub).FontSize(11).TextColor(p.muted).SingleLine().TextAlign(ui.Start)
					}
				}
			})
		})

		// Center: compact transport controls + progress scrubber
		ui.Column(c).Width(430).Shrink(1).MinWidth(260).Gap(2).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).Gap(8).Center().Children(func() {
				if a.barIconBtn(c.Key("skip-back"), p, iconSkipBack, 15, "快退 15 秒").Clicked() {
					a.player.Skip(-15 * time.Second)
				}
				if a.barIconBtn(c.Key("prev-ep"), p, iconPrev, 14, "上一集").Clicked() {
					a.player.Prev()
				}

				playBtn := ui.ButtonBase(c.Key("play-toggle")).Size(30, 30).Radius(999).Center().Background(p.accent).TextColor(p.onAccent).Label("播放/暂停").Cursor(ui.CursorPointer).Transition(animUIHover)
				applyTapPose(playBtn, 1.07, 0)
				playBtn.Children(func() {
					if snap.Playing {
						ui.Icon(c, iconPause).Size(14, 14).ContentTransition(ui.ContentTransition{})
					} else {
						ui.Icon(c, iconPlay).Size(14, 14).ContentTransition(ui.ContentTransition{})
					}
				})
				if playBtn.Clicked() {
					a.player.TogglePlay()
				}

				if a.barIconBtn(c.Key("next-ep"), p, iconNext, 14, "下一集").Clicked() {
					a.player.Next()
				}
				if a.barIconBtn(c.Key("skip-fwd"), p, iconSkipFwd, 15, "快进 30 秒").Clicked() {
					a.player.Skip(30 * time.Second)
				}
			})

			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				posSec := snap.Position.Seconds()
				if a.barScrub.active {
					posSec = a.barScrub.val
				}
				durSec := snap.Duration.Seconds()
				ui.Text(c, formatSeconds(posSec)).FontSize(10.5).FontFeatures("tnum").TextColor(p.muted).Width(42).TextAlign(ui.End)
				a.scrubber(c, p, snap).Grow(1)
				ui.Text(c, formatSeconds(durSec)).FontSize(10.5).FontFeatures("tnum").TextColor(p.muted).Width(42)
			})
		})

		// Right: speed, transcript toggle, details, volume
		ui.Row(c).Grow(1).Basis(0).MinWidth(180).Justify(ui.End).AlignItems(ui.Center).Gap(6).Children(func() {
			speedLabel := fmt.Sprintf("%.2gx", snap.Speed)
			if ui.Button(c.Key("speed-btn"), speedLabel).Height(26).Padding(0, 8).Tooltip("切换播客倍速（保持人声原调）").Clicked() {
				next := a.player.CycleSpeed()
				a.updateState(func() { a.state.Speed = next })
			}

			if snap.Episode != nil {
				inTr := a.route.page == pageEpisode && a.route.epID == snap.Episode.ID && a.route.transcript
				trBtn := ui.ButtonBase(c.Key("bar-tr")).Size(28, 28).Radius(7).Center().FocusRing(false).Tooltip("打开沉浸视图").Label("文稿").Cursor(ui.CursorPointer).Transition(animUIHover)
				applyTapPose(trBtn, 1.06, 0)
				switch {
				case inTr:
					trBtn.Background(p.accent.Alpha(0.16)).TextColor(p.accent)
				case trBtn.Hovered():
					trBtn.Background(p.hover).TextColor(p.text)
				default:
					trBtn.TextColor(p.text)
				}
				trBtn.Children(func() { ui.Icon(c, iconQuote).Size(14, 14) })
				if trBtn.Clicked() {
					if inTr {
						a.setTranscriptMode(false)
					} else {
						a.openEpisode(*snap.Episode, true)
					}
				}

				if a.barIconBtn(c.Key("bar-info"), p, iconInfo, 14, "查看单集详情").Clicked() {
					a.openEpisode(*snap.Episode, false)
				}
			}

			volIcon := iconVolume
			if snap.Volume <= 0.01 {
				volIcon = iconMute
			}
			if a.barIconBtn(c.Key("mute-btn"), p, volIcon, 15, "静音/恢复音量").Clicked() {
				if snap.Volume > 0.01 {
					a.player.SetVolume(0)
				} else {
					v := a.state.Volume
					if v <= 0.01 {
						v = 0.8
					}
					a.player.SetVolume(v)
				}
			}

			vol := snap.Volume
			if ui.Slider(c.Key("vol-slider"), &vol, 0, 1).Width(76).Label("音量").Changed() {
				a.player.SetVolume(vol)
				if vol > 0.01 {
					a.updateState(func() { a.state.Volume = vol })
				}
			}
		})
	})
}

func (a *App) scrubber(c *ui.Context, p palette, snap player.Snapshot) ui.Element {
	return a.scrubberWithColors(
		c.Key("bar-scrub"),
		&a.barScrub,
		snap,
		true,
		0,
		0,
		p.border,
		p.accent,
		p.faint.Alpha(0.35),
		p.accent,
		3.5,
		5.5,
	)
}

func (a *App) scrubberWithColors(
	c *ui.Context,
	st *scrubState,
	snap player.Snapshot,
	bindActiveTrack bool,
	overridePos, overrideDur float64,
	trackCol, fillCol, dlCol, knobCol ui.Color,
	thickIdle, thickHot float32,
) ui.Element {
	durSec := snap.Duration.Seconds()
	posSec := snap.Position.Seconds()
	if !bindActiveTrack {
		posSec = max(0, overridePos)
		durSec = max(overrideDur, 1)
	}
	if !st.active && !st.held {
		st.val = posSec
	}

	s := ui.SliderBase(c, &st.val, 0, max(durSec, 1)).Height(14).PaddingX(4).FocusRing(false).Label("播放进度").Cursor(ui.CursorPointer)
	s.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button == 0 {
				st.held = true
			}
		case ui.InputPointerUp:
			st.held = false
			if ev.Canceled {
				st.active = false
			}
		}
		return false
	})

	switch {
	case st.held:
		st.active = true
	case st.active:
		st.active = false
		if bindActiveTrack {
			a.player.Seek(time.Duration(st.val * float64(time.Second)))
		}
	case s.Changed():
		if bindActiveTrack {
			a.player.Seek(time.Duration(st.val * float64(time.Second)))
		}
	}

	hot := s.Hovered() || st.held
	s.Draw(func(pt *ui.Painter, r ui.Rect) {
		at := posSec
		if st.active {
			at = st.val
		}
		frac := float32(0)
		if durSec > 0 {
			frac = float32(min(1, max(0, at/durSec)))
		}

		thick := thickIdle
		if hot {
			thick = thickHot
		}
		y := r.Y + (r.H-thick)/2
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: thick}, trackCol, thick/2)
		if bindActiveTrack && snap.DownloadFrac > 0 && snap.DownloadFrac < 1 {
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W * float32(snap.DownloadFrac), H: thick}, dlCol, thick/2)
		}
		playedW := max(thick, r.W*frac)
		if frac <= 0 {
			playedW = 0
		}
		if playedW > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: y, W: min(r.W, playedW), H: thick}, fillCol, thick/2)
		}
		if hot {
			kx := r.X + r.W*frac
			ky := r.Y + r.H/2
			pt.Fill(ui.Rect{X: kx - 5, Y: ky - 5, W: 10, H: 10}, knobCol, 5)
		}
	})
	return s
}

func (a *App) barIconBtn(c *ui.Context, p palette, icon *ui.SVG, size float32, tooltip string) ui.Element {
	b := ui.ButtonBase(c).Size(28, 28).Radius(999).Center().TextColor(p.text).FocusRing(false).Label(tooltip).Tooltip(tooltip).Cursor(ui.CursorPointer).Transition(animUIHover)
	if b.Hovered() {
		b.Background(p.hover)
	}
	applyTapPose(b, 1.06, 0)
	b.Children(func() { ui.Icon(c, icon).Size(size, size) })
	return b
}

func (a *App) errorBanner(c *ui.Context, p palette, msg string, retry func()) {
	ui.Row(c).Padding(14).Gap(12).Radius(10).Background(c.Theme().Danger.Alpha(0.10)).Border(1, c.Theme().Danger.Alpha(0.28)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, msg).FontSize(13).Grow(1)
		if retry != nil {
			if ui.Button(c.Key("retry"), "重试").Clicked() {
				retry()
			}
		}
	})
}

func (a *App) episodeArtworkURL(ep apple.Episode) string {
	if ep.ArtworkURL != "" {
		return apple.FormatArtwork(ep.ArtworkURL, 600)
	}
	if ep.ShowArtworkURL != "" {
		return apple.FormatArtwork(ep.ShowArtworkURL, 600)
	}
	if ep.ShowID != "" {
		if st := a.shows[ep.ShowID]; st != nil && st.detail != nil && st.detail.Show.ArtworkURL != "" {
			return apple.FormatArtwork(st.detail.Show.ArtworkURL, 600)
		}
	}
	return ""
}

func activeChapterIndex(chapters []apple.TranscriptLine, snap player.Snapshot, epID string) int {
	if len(chapters) == 0 || snap.Episode == nil || snap.Episode.ID != epID {
		return -1
	}
	posSec := snap.Position.Seconds()
	best := -1
	for i, ln := range chapters {
		if posSec+0.4 >= ln.StartSec {
			best = i
		}
	}
	return best
}

func activeTranscriptLine(tr *apple.EpisodeTranscript, snap player.Snapshot, epID string) int {
	if tr == nil || len(tr.Lines) == 0 || snap.Episode == nil || snap.Episode.ID != epID {
		return -1
	}
	posSec := snap.Position.Seconds()
	if tr.HasSyncedTime {
		best := -1
		for i, ln := range tr.Lines {
			if ln.HasTimestamp && posSec+0.4 >= ln.StartSec {
				best = i
			}
		}
		return best
	}
	dur := snap.Duration.Seconds()
	if dur <= 0 || posSec <= 0 {
		return 0
	}
	idx := int(min(1, max(0, posSec/dur)) * float64(len(tr.Lines)-1))
	return idx
}

func trimLeadingTimestamp(s string) string {
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	if len(fields) > 1 && strings.Contains(fields[0], ":") && len(fields[0]) <= 10 {
		trimmed := strings.TrimSpace(strings.TrimPrefix(s, fields[0]))
		trimmed = strings.TrimLeft(trimmed, "-—–:：| ")
		if trimmed != "" {
			return trimmed
		}
	}
	return s
}

func compactEpisodeSummary(ep apple.Episode) string {
	s := strings.TrimSpace(ep.ShortDescription)
	if s == "" {
		s = strings.TrimSpace(ep.Description)
	}
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func (a *App) inlineProgressBadge(c *ui.Context, p palette, label string, frac float32, completed bool) {
	ui.Row(c).Shrink(0).Gap(5).AlignItems(ui.Center).Children(func() {
		if completed {
			ui.Icon(c, iconCheck).Size(11, 11).TextColor(p.accent)
		} else if frac > 0.02 {
			ui.Box(c).Width(28).Height(4).Radius(2).Background(p.accent.Alpha(0.22)).Clip().Children(func() {
				ui.Box(c).FillHeight().Width(28 * frac).Radius(2).Background(p.accent)
			})
		}
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.accent).SingleLine()
	})
}

func (a *App) episodeProgressDisplay(ep apple.Episode, snap player.Snapshot) (label string, frac float32, completed bool, hasProgress bool) {
	if snap.Episode != nil && snap.Episode.ID == ep.ID && snap.Duration > 0 {
		pos := snap.Position.Seconds()
		dur := snap.Duration.Seconds()
		if pos > 5 {
			frac = float32(min(1.0, max(0.0, pos/dur)))
			remMS := max(int64(0), (snap.Duration - snap.Position).Milliseconds())
			if rem := apple.FormatShortDurationMS(remMS); rem != "" && remMS > 0 {
				return "剩 " + rem, frac, false, true
			}
			return "已播 " + formatSeconds(pos), frac, false, true
		}
	}
	prog, ok := a.episodeProgress(ep.ID)
	if !ok {
		return "", 0, false, false
	}
	if prog.Completed {
		return "已播完", 1, true, true
	}
	if prog.PositionSec <= 5 {
		return "", 0, false, false
	}
	durSec := float64(ep.DurationMS) / 1000.0
	if durSec <= 0 {
		durSec = prog.DurationSec
	}
	if durSec > 0 {
		frac = float32(min(1.0, max(0.0, prog.PositionSec/durSec)))
		remMS := max(int64(0), int64((durSec-prog.PositionSec)*1000))
		if rem := apple.FormatShortDurationMS(remMS); rem != "" && remMS > 0 {
			return "剩 " + rem, frac, false, true
		}
	}
	return "已播 " + formatSeconds(prog.PositionSec), frac, false, true
}

func (a *App) episodeProgressSecs(ep apple.Episode, snap player.Snapshot) (cur, total float64) {
	if snap.Episode != nil && snap.Episode.ID == ep.ID {
		cur = snap.Position.Seconds()
		total = snap.Duration.Seconds()
	} else if prog, ok := a.episodeProgress(ep.ID); ok && !prog.Completed && prog.PositionSec > 0 {
		cur = prog.PositionSec
		total = prog.DurationSec
	}
	if total <= 0 && ep.DurationMS > 0 {
		total = float64(ep.DurationMS) / 1000.0
	}
	return cur, max(total, 1)
}

func parseHexColor(hexStr string, fallback ui.Color) ui.Color {
	h := strings.TrimPrefix(strings.TrimSpace(hexStr), "#")
	if len(h) != 6 && len(h) != 3 {
		return fallback
	}
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(h, "%02x%02x%02x", &r, &g, &b); err != nil {
		return fallback
	}
	return ui.RGB(r, g, b)
}

func colorLuminance(c ui.Color) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255.0
}

func richHeroCardBg(c ui.Color) ui.Color {
	lum := colorLuminance(c)
	if lum > 0.56 {
		return c.MixOklab(ui.Hex("#17151c"), 0.52)
	}
	if lum > 0.42 {
		return c.MixOklab(ui.Hex("#17151c"), 0.28)
	}
	return c
}

func lyricsBackdropColors(hexStr string) (topLeft, botRight, glow ui.Color) {
	base := parseHexColor(hexStr, ui.Hex("#6b1b1d"))
	lum := colorLuminance(base)
	// Ensure dark velvety richness similar to macOS Apple Music lyrics screen
	switch {
	case lum > 0.55:
		base = base.MixOklab(ui.Hex("#201218"), 0.65)
	case lum > 0.35:
		base = base.MixOklab(ui.Hex("#161015"), 0.45)
	case lum < 0.05:
		// Nearly pure black -> lift slightly into warm espresso/charcoal so gradient glow shows
		base = base.MixOklab(ui.Hex("#2f2428"), 0.45)
	}
	topLeft = base.MixOklab(ui.Hex("#120c10"), 0.25)
	botRight = base.MixOklab(ui.Hex("#0b090c"), 0.48)
	glow = base.MixOklab(ui.Hex("#ff9c78"), 0.30)
	return topLeft, botRight, glow
}

func cmpNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func formatSeconds(sec float64) string {
	if sec <= 0 {
		return "0:00"
	}
	total := int(sec + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
