package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"apple-podcasts/internal/apple"
)

// selectLibrary runs on the UI thread when the account changes. A previous
// build stored its signed-in library in the same fields as the guest library.
func (a *App) selectLibrary(token string, migrate bool) {
	a.cloudLoading = false
	a.cloudWriting = false
	a.cloudErr = ""
	a.cloudUpNext = nil
	a.cloudNewEps = nil
	a.cloudRecentPlayed = nil
	a.cloudPositions = nil
	a.chartShowLatest = nil
	a.lastCloudSync = time.Time{}
	a.lastCloudLibrarySync = time.Time{}
	a.invalidateCatalogCachesForAuth()
	a.updateFeaturedEpisodes()

	token = strings.TrimSpace(token)
	a.stateMu.Lock()

	cache := a.state.CloudLibrary
	needSave := false
	if token == "" {
		if cache != nil || a.state.AccountName != "" {
			if migrate && cache != nil {
				// Keep unverified cache untouched on startup when signed out.
			} else if cache == nil && migrate {
				// These entries belonged to a session that we can no longer verify.
				a.state.CloudLibrary = &CloudLibrary{
					AccountName: a.state.AccountName, Subscriptions: a.state.Subscriptions, SavedEpisodes: a.state.SavedEpisodes,
				}
				a.state.Subscriptions = nil
				a.state.SavedEpisodes = nil
				a.state.AccountName = ""
				needSave = true
			} else {
				a.state.CloudLibrary = nil
				a.state.AccountName = ""
				needSave = true
			}
		}
		if needSave {
			saveAppState(a.dataDir, a.state)
		}
		a.stateMu.Unlock()
		return
	}

	key := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	if cache != nil && cache.SessionKey == key {
		if cache.Progress == nil {
			cache.Progress = map[string]EpisodeProgress{}
		}
		if len(cache.RecentPlayed) > 0 {
			a.cloudRecentPlayed = slices.Clone(cache.RecentPlayed)
		}
		if len(cache.CloudPositions) > 0 {
			a.cloudPositions = make(map[string]apple.PlaybackPosition, len(cache.CloudPositions))
			for id, pos := range cache.CloudPositions {
				if pos.Known {
					a.cloudPositions[id] = pos
				}
			}
		}
		sf := cache.Storefront
		a.stateMu.Unlock()
		if sf != "" && a.client != nil {
			a.client.SetAccountStorefront(sf)
		}
		return
	}

	a.state.CloudLibrary = &CloudLibrary{
		SessionKey:     key,
		Progress:       map[string]EpisodeProgress{},
		CloudPositions: map[string]apple.PlaybackPosition{},
	}
	if migrate && cache == nil {
		a.state.CloudLibrary.AccountName = a.state.AccountName
		a.state.CloudLibrary.Subscriptions = a.state.Subscriptions
		a.state.CloudLibrary.SavedEpisodes = a.state.SavedEpisodes
		a.state.Subscriptions = nil
		a.state.SavedEpisodes = nil
	}
	a.state.AccountName = ""
	saveAppState(a.dataDir, a.state)
	a.stateMu.Unlock()
}

func (a *App) invalidateCatalogCachesForAuth() {
	a.shows = map[string]*showDetailState{}
	for _, st := range a.episodes {
		if st != nil {
			st.fetched = false
			st.loading = false
		}
	}
	a.searchRes = nil
}

func (a *App) refreshCurrentPageForAuthChange() {
	switch a.route.page {
	case pageShow:
		if a.route.showID != "" {
			a.ensureShowLoaded(a.route.showID)
		}
	case pageEpisode:
		if a.route.epID != "" {
			ep := apple.Episode{ID: a.route.epID, ShowID: a.route.showID}
			if st := a.episodes[a.route.epID]; st != nil && st.episode != nil {
				ep = *st.episode
			}
			a.ensureEpisodeLoaded(ep)
		}
	case pageSearch:
		if strings.TrimSpace(a.searchLast) != "" {
			a.runSearch(a.searchLast)
		}
	}
}

func (a *App) subscriptions() []apple.Show {
	if a.client != nil && a.client.HasAuth() && a.state.CloudLibrary != nil {
		return a.state.CloudLibrary.Subscriptions
	}
	return a.state.Subscriptions
}

func (a *App) savedEpisodes() []apple.Episode {
	if a.client != nil && a.client.HasAuth() && a.state.CloudLibrary != nil {
		return a.state.CloudLibrary.SavedEpisodes
	}
	return a.state.SavedEpisodes
}

func (a *App) recentEpisodes() []apple.Episode {
	if a.client != nil && a.client.HasAuth() {
		return a.cloudRecentPlayed
	}
	return a.state.RecentEpisodes
}

func (a *App) accountName() string {
	if a.state.CloudLibrary == nil {
		return ""
	}
	return a.state.CloudLibrary.AccountName
}

func (a *App) updateFeaturedEpisodes() {
	if a.charts != nil {
		a.featuredEps = buildFeaturedEpisodes(a.cloudNewEps, a.chartShowLatest, a.charts.TrendingEpisodes, 8)
	}
}

type cloudLibraryResult struct {
	shows        []apple.Show
	saved        []apple.Episode
	upNext       []apple.Episode
	newEps       []apple.Episode
	recentPlayed []apple.Episode
	storefront   string
	name         string
	showsErr     error
	savedErr     error
	nextErr      error
	newErr       error
	recentErr    error
}

func (a *App) maybeSyncCloudLibrary() {
	if a.client == nil || !a.client.HasAuth() {
		return
	}
	if !a.lastCloudLibrarySync.IsZero() && time.Since(a.lastCloudLibrarySync) < 45*time.Second {
		return
	}
	a.syncCloudLibrary()
}

func (a *App) syncCloudLibrary() {
	session := a.client.AuthContext()
	if session == nil || a.cloudLoading || a.cloudWriting {
		return
	}
	a.cloudLoading = true
	a.cloudErr = ""

	go func() {
		ctx, cancel := context.WithTimeout(session, 25*time.Second)
		defer cancel()

		var result cloudLibraryResult
		var fetch sync.WaitGroup
		fetch.Go(func() { result.shows, result.showsErr = a.client.LibraryShows(ctx) })
		fetch.Go(func() { result.saved, result.savedErr = a.client.SavedEpisodes(ctx) })
		fetch.Go(func() { result.upNext, result.nextErr = a.client.ContinuePlaying(ctx) })
		fetch.Go(func() { result.newEps, result.newErr = a.client.NewEpisodes(ctx) })
		fetch.Go(func() { result.recentPlayed, result.recentErr = a.client.RecentPlayed(ctx) })
		fetch.Go(func() { result.storefront, _ = a.client.UserStorefront(ctx) })
		fetch.Go(func() { result.name, _ = a.client.AccountInfo(ctx) })
		fetch.Wait()

		a.update(func() {
			if session.Err() != nil {
				return
			}
			a.cloudLoading = false
			a.applyCloudLibrary(result)
			a.refreshActivePagePositions()
		})
	}()
}

func (a *App) applyCloudLibrary(result cloudLibraryResult) {
	a.lastCloudLibrarySync = time.Now()
	if result.storefront != "" && a.client != nil {
		a.client.SetAccountStorefront(result.storefront)
	}
	if cache := a.state.CloudLibrary; cache != nil {
		a.updateState(func() {
			if result.name != "" {
				cache.AccountName = result.name
			}
			if result.storefront != "" {
				cache.Storefront = result.storefront
			}
			if result.showsErr == nil {
				cache.Subscriptions = result.shows
			}
			if result.savedErr == nil {
				cache.SavedEpisodes = result.saved
			}
			if result.recentErr == nil {
				cache.RecentPlayed = result.recentPlayed
			}
		})
	}

	if result.nextErr == nil {
		a.cloudUpNext = result.upNext
		a.mergeCloudPositions(result.upNext)
	}
	if result.newErr == nil {
		a.cloudNewEps = result.newEps
		a.mergeCloudPositions(result.newEps)
		a.updateFeaturedEpisodes()
	}
	if result.savedErr == nil {
		a.mergeCloudPositions(result.saved)
	}
	if result.recentErr == nil {
		a.cloudRecentPlayed = result.recentPlayed
		a.mergeCloudPositions(result.recentPlayed)
	}

	if err := errors.Join(result.showsErr, result.savedErr, result.nextErr, result.newErr); err != nil {
		if errors.Is(err, apple.ErrUnauthorized) {
			a.cloudErr = "部分云端资料无法读取，请检查 Apple ID 登录状态"
		} else {
			a.cloudErr = err.Error()
		}
	}
}

func (a *App) mergeCloudPositions(episodes []apple.Episode) {
	if a.client == nil || !a.client.HasAuth() || len(episodes) == 0 {
		return
	}
	changed := false
	for _, ep := range episodes {
		if ep.ID == "" || !ep.PlaybackPosition.Known {
			continue
		}
		if a.cloudPositions == nil {
			a.cloudPositions = map[string]apple.PlaybackPosition{}
		}
		incoming := ep.PlaybackPosition
		cur, ok := a.cloudPositions[ep.ID]
		if !ok || cur.RecordedAt.IsZero() || !incoming.RecordedAt.Before(cur.RecordedAt) {
			if !ok || cur != incoming {
				a.cloudPositions[ep.ID] = incoming
				changed = true
			}
		}
	}
	if !changed {
		return
	}
	if cache := a.state.CloudLibrary; cache != nil {
		a.stateMu.Lock()
		if cache.CloudPositions == nil {
			cache.CloudPositions = map[string]apple.PlaybackPosition{}
		}
		for id, pos := range a.cloudPositions {
			cache.CloudPositions[id] = pos
		}
		saveAppState(a.dataDir, a.state)
		a.stateMu.Unlock()
	}
	a.updatePausedPosition()
}

// refreshActivePagePositions pulls fresh personalized playback positions for whatever
// episodes are currently visible on Show, Episode, Search, or Discover pages.
func (a *App) refreshActivePagePositions() {
	if a.client == nil {
		return
	}
	session := a.client.AuthContext()
	if session == nil {
		return
	}
	var ids []string
	switch a.route.page {
	case pageShow:
		if st := a.shows[a.route.showID]; st != nil && st.detail != nil {
			for _, ep := range st.detail.Episodes {
				ids = append(ids, ep.ID)
			}
		}
	case pageEpisode:
		if a.route.epID != "" {
			ids = append(ids, a.route.epID)
		}
	case pageSearch:
		if a.searchRes != nil {
			for _, ep := range a.searchRes.Episodes {
				ids = append(ids, ep.ID)
			}
		}
	case pageDiscover:
		for _, ep := range a.featuredEps {
			ids = append(ids, ep.ID)
		}
		if a.charts != nil {
			for _, ep := range a.charts.TrendingEpisodes {
				ids = append(ids, ep.ID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(session, 15*time.Second)
		defer cancel()
		posMap, err := a.client.PlaybackPositions(ctx, ids)
		if err != nil || len(posMap) == 0 {
			return
		}
		eps := make([]apple.Episode, 0, len(posMap))
		for id, pos := range posMap {
			eps = append(eps, apple.Episode{ID: id, PlaybackPosition: pos})
		}
		a.update(func() {
			if session.Err() != nil {
				return
			}
			a.mergeCloudPositions(eps)
		})
	}()
}

func (a *App) newerCloudPosition(id string, local EpisodeProgress, hasLocal bool) (apple.PlaybackPosition, bool) {
	cloud, ok := a.cloudPositions[id]
	return cloud, ok && (!hasLocal || cloud.RecordedAt.Unix() > local.UpdatedAt)
}

func (a *App) updatePausedPosition() {
	if a.player == nil {
		return
	}
	snap := a.player.Snapshot()
	if snap.Episode == nil || snap.Playing || snap.Loading {
		return
	}
	local, ok := a.localEpisodeProgress(snap.Episode.ID)
	cloud, newer := a.newerCloudPosition(snap.Episode.ID, local, ok)
	if newer {
		var pos time.Duration
		if !cloud.Completed {
			pos = time.Duration(max(0, cloud.PositionMS) * float64(time.Millisecond))
		}
		a.player.ApplyResumePosition(snap.Episode.ID, pos)
	}
}

func (a *App) isSubscribed(showID string) bool {
	return slices.ContainsFunc(a.subscriptions(), func(s apple.Show) bool { return s.ID == showID })
}

func (a *App) toggleSubscribe(show apple.Show) {
	follow := !a.isSubscribed(show.ID)
	toast := "已取消订阅「" + show.Name + "」"
	if follow {
		toast = "已订阅「" + show.Name + "」"
	}
	a.changeLibrary(
		func(ctx context.Context) error { return a.client.SubscribeShow(ctx, show.ID, follow) },
		func(shows *[]apple.Show, _ *[]apple.Episode) {
			show.Subscribed = follow
			*shows = toggleLibraryItem(*shows, show, follow, func(s apple.Show) bool { return s.ID == show.ID })
		},
		toast, "云端订阅同步失败",
	)
}

func (a *App) isSaved(episodeID string) bool {
	return slices.ContainsFunc(a.savedEpisodes(), func(ep apple.Episode) bool { return ep.ID == episodeID })
}

func (a *App) toggleSaveEpisode(episode apple.Episode) {
	save := !a.isSaved(episode.ID)
	toast := "已移出收藏"
	if save {
		toast = "已收藏单集"
	}
	a.changeLibrary(
		func(ctx context.Context) error { return a.client.SaveEpisode(ctx, episode.ID, save) },
		func(_ *[]apple.Show, episodes *[]apple.Episode) {
			*episodes = toggleLibraryItem(*episodes, episode, save, func(ep apple.Episode) bool { return ep.ID == episode.ID })
		},
		toast, "云端单集同步失败",
	)
}

func toggleLibraryItem[T any](items []T, item T, add bool, matches func(T) bool) []T {
	items = slices.DeleteFunc(slices.Clone(items), matches)
	if add {
		items = append([]T{item}, items...)
	}
	return items
}

func (a *App) changeLibrary(request func(context.Context) error, change func(*[]apple.Show, *[]apple.Episode), success, failure string) {
	session := a.client.AuthContext()
	if session == nil {
		a.updateState(func() { change(&a.state.Subscriptions, &a.state.SavedEpisodes) })
		a.pendingToast = success
		return
	}
	if a.cloudLoading || a.cloudWriting {
		a.pendingToast = "云端资料正在同步，请稍后重试"
		return
	}
	a.cloudWriting = true
	go func() {
		ctx, cancel := context.WithTimeout(session, 15*time.Second)
		defer cancel()
		err := request(ctx)
		a.update(func() {
			if session.Err() != nil {
				return
			}
			a.cloudWriting = false
			if err != nil {
				a.pendingToast = failure + "：" + err.Error()
				return
			}
			if cache := a.state.CloudLibrary; cache != nil {
				a.updateState(func() { change(&cache.Subscriptions, &cache.SavedEpisodes) })
			}
			a.pendingToast = success
		})
	}()
}
