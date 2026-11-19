package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"apple-podcasts/internal/apple"
)

func TestAccountLibraryRefreshReplacesListsAndIsolatesSessions(t *testing.T) {
	a := newTestApp()
	a.state.Subscriptions = []apple.Show{{ID: "guest-show"}}
	a.state.SavedEpisodes = []apple.Episode{{ID: "guest-episode"}}
	a.client.SetAuth("session-a", "")
	a.selectLibrary("session-a", false)
	a.applyCloudLibrary(cloudLibraryResult{
		shows: []apple.Show{{ID: "account-a-show"}},
		saved: []apple.Episode{{ID: "account-a-episode"}},
	})
	if a.isSubscribed("guest-show") || a.isSaved("guest-episode") || !a.isSubscribed("account-a-show") || !a.isSaved("account-a-episode") {
		t.Fatal("account A view does not match the account library")
	}
	a.applyCloudLibrary(cloudLibraryResult{showsErr: errors.New("temporary error")})
	if !a.isSubscribed("account-a-show") || a.isSaved("account-a-episode") {
		t.Fatal("an empty successful list must replace its cache and a failed list must keep its cache")
	}
	a.applyCloudLibrary(cloudLibraryResult{})
	if a.isSubscribed("account-a-show") {
		t.Fatal("removed remote subscription remained after successful empty refresh")
	}
	a.applyCloudLibrary(cloudLibraryResult{shows: []apple.Show{{ID: "account-a-show"}}})
	a.client.ClearAuth()
	a.selectLibrary("", false)
	if !a.isSubscribed("guest-show") || !a.isSaved("guest-episode") || a.isSubscribed("account-a-show") {
		t.Fatal("sign-out did not restore the guest library")
	}
	a.client.SetAuth("session-b", "")
	a.selectLibrary("session-b", false)
	if len(a.subscriptions()) != 0 || len(a.savedEpisodes()) != 0 {
		t.Fatal("a new account inherited another library")
	}
}

func TestOlderAccountCacheMigratesWithoutOverwritingRoute(t *testing.T) {
	a := newTestApp()
	a.state.AccountName = "Prior account"
	a.state.Subscriptions = []apple.Show{{ID: "old"}}
	a.state.LastRoute = &SavedRoute{Page: pageSaved}
	a.client.SetAuth("prior-session", "")
	a.selectLibrary("prior-session", true)
	if a.accountName() != "Prior account" || !a.isSubscribed("old") || a.state.LastRoute.Page != pageSaved {
		t.Fatal("the existing account library or saved route was lost during migration")
	}
	a.client.ClearAuth()
	a.selectLibrary("", false)
	if a.isSubscribed("old") {
		t.Fatal("legacy account content appeared in the guest library")
	}
}

func TestPlayEpisodeUsesMostRecentPosition(t *testing.T) {
	a := newTestApp()
	a.state = defaultAppState()
	a.client.SetAuth("fictional-session", "")
	a.selectLibrary("fictional-session", false)
	recordedAt := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	episode := apple.Episode{ID: "episode", DurationMS: 360000, PlaybackPosition: apple.PlaybackPosition{
		Known: true, PositionMS: 125000, RecordedAt: recordedAt,
	}}
	queue := []apple.Episode{episode}
	a.player.Restore(episode, queue, 75*time.Second)
	a.applyCloudLibrary(cloudLibraryResult{upNext: []apple.Episode{episode}})
	if got := a.player.Snapshot().Position; got != 125*time.Second {
		t.Errorf("paused restored episode after cloud refresh = %v, want 2m5s", got)
	}
	a.playEpisode(episode, queue)
	if got := a.player.Snapshot().Position; got != 125*time.Second {
		t.Errorf("cloud resume = %v, want 2m5s", got)
	}
	// Pre-login guest progress must not override the signed-in account's cloud progress.
	a.state.Progress[episode.ID] = EpisodeProgress{PositionSec: 12, UpdatedAt: recordedAt.Add(2 * time.Minute).Unix()}
	a.playEpisode(episode, queue)
	if got := a.player.Snapshot().Position; got != 125*time.Second {
		t.Errorf("guest progress leaked into signed-in resume: got %v, want 2m5s", got)
	}
	a.state.CloudLibrary.Progress[episode.ID] = EpisodeProgress{PositionSec: 30, UpdatedAt: recordedAt.Add(time.Minute).Unix()}
	a.playEpisode(episode, queue)
	if got := a.player.Snapshot().Position; got != 30*time.Second {
		t.Errorf("newer local resume = %v, want 30s", got)
	}
	episode.PlaybackPosition = apple.PlaybackPosition{Known: true, Completed: true, RecordedAt: time.Now().Add(time.Second)}
	a.applyCloudLibrary(cloudLibraryResult{upNext: []apple.Episode{episode}})
	a.playEpisode(episode, queue)
	if got := a.player.Snapshot().Position; got != 0 {
		t.Errorf("replay of remotely completed episode = %v, want 0", got)
	}
}

func TestCrossPageCloudPositionsPersistAndPreservePausedTimestamps(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp()
	a.dataDir = dir
	a.state = defaultAppState()
	a.client.SetAuth("session-a", "")
	a.selectLibrary("session-a", false)

	t1 := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	t2 := time.Now().Add(-2 * time.Minute).Truncate(time.Second)

	// Merge a position fetched from Show Detail first.
	a.mergeCloudPositions([]apple.Episode{{
		ID: "show-ep",
		PlaybackPosition: apple.PlaybackPosition{
			Known: true, PositionMS: 900000, RecordedAt: t1,
		},
	}})

	// Cloud library refresh brings positions from Saved, NewEpisodes, and RecentPlayed without wiping Show Detail positions.
	a.applyCloudLibrary(cloudLibraryResult{
		storefront: "cn",
		saved: []apple.Episode{{
			ID: "saved-ep",
			PlaybackPosition: apple.PlaybackPosition{
				Known: true, PositionMS: 300000, RecordedAt: t1,
			},
		}},
		newEps: []apple.Episode{{
			ID: "new-ep",
			PlaybackPosition: apple.PlaybackPosition{
				Known: true, PositionMS: 120000, RecordedAt: t1,
			},
		}},
		recentPlayed: []apple.Episode{{
			ID: "recent-ep",
			PlaybackPosition: apple.PlaybackPosition{
				Known: true, Completed: true, RecordedAt: t2,
			},
		}},
	})

	for _, check := range []struct {
		id        string
		wantSec   float64
		completed bool
	}{
		{"show-ep", 900, false},
		{"saved-ep", 300, false},
		{"new-ep", 120, false},
		{"recent-ep", 0, true},
	} {
		prog, ok := a.episodeProgress(check.id)
		if !ok || prog.PositionSec != check.wantSec || prog.Completed != check.completed {
			t.Errorf("episodeProgress(%q) = (%+v, %v), want pos=%v completed=%v", check.id, prog, ok, check.wantSec, check.completed)
		}
	}

	// Simulate an older paused episode persisted across close/reopen, then verify newer iCloud position still wins.
	pausedEp := apple.Episode{ID: "paused-ep", DurationMS: 3600000}
	a.state.CloudLibrary.Progress[pausedEp.ID] = EpisodeProgress{PositionSec: 60, UpdatedAt: t1.Unix()}
	a.player.Restore(pausedEp, []apple.Episode{pausedEp}, 60*time.Second)
	a.persistSessionNow()
	if got := a.state.CloudLibrary.Progress[pausedEp.ID].UpdatedAt; got != t1.Unix() {
		t.Fatalf("persistSessionNow stamped unchanged paused episode UpdatedAt=%d, want %d", got, t1.Unix())
	}
	a.mergeCloudPositions([]apple.Episode{{
		ID: pausedEp.ID,
		PlaybackPosition: apple.PlaybackPosition{
			Known: true, PositionMS: 1500000, RecordedAt: t2,
		},
	}})
	if got := a.player.Snapshot().Position; got != 1500*time.Second {
		t.Fatalf("newer iCloud position failed to update paused player: got %v, want 25m0s", got)
	}

	// Cold restart under same session restores cached cloud positions, storefront, and recent played.
	a2 := newTestApp()
	a2.dataDir = dir
	a2.state = loadAppState(dir)
	a2.client.SetAuth("session-a", "")
	a2.selectLibrary("session-a", true)
	if a2.client.AccountStorefront() != "cn" {
		t.Errorf("restored AccountStorefront = %q, want cn", a2.client.AccountStorefront())
	}
	if len(a2.recentEpisodes()) != 1 || a2.recentEpisodes()[0].ID != "recent-ep" {
		t.Errorf("restored recentEpisodes = %+v", a2.recentEpisodes())
	}
	if prog, ok := a2.episodeProgress("show-ep"); !ok || prog.PositionSec != 900 {
		t.Errorf("restored cloud show-ep progress = (%+v, %v), want 900s", prog, ok)
	}
	if prog, ok := a2.episodeProgress("saved-ep"); !ok || prog.PositionSec != 300 {
		t.Errorf("restored cloud saved-ep progress = (%+v, %v), want 300s", prog, ok)
	}

	// Sign-out clears account progress & recent episodes from guest view.
	a2.client.ClearAuth()
	a2.selectLibrary("", false)
	if _, ok := a2.episodeProgress("show-ep"); ok {
		t.Error("signed-out guest view still saw account cloud progress")
	}
	if len(a2.recentEpisodes()) != 0 {
		t.Errorf("signed-out guest view inherited account recent episodes: %+v", a2.recentEpisodes())
	}
}

func TestResolveDataDirPortableMode(t *testing.T) {
	exeDir := t.TempDir()
	defaultDir := t.TempDir()

	if got := resolveDataDirForExe(exeDir, defaultDir); got != defaultDir {
		t.Fatalf("without marker resolveDataDirForExe = %q, want %q", got, defaultDir)
	}
	if err := os.WriteFile(filepath.Join(exeDir, "portable.txt"), []byte("portable"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantPortable := filepath.Join(exeDir, "data")
	if got := resolveDataDirForExe(exeDir, defaultDir); got != wantPortable {
		t.Fatalf("with portable.txt resolveDataDirForExe = %q, want %q", got, wantPortable)
	}
	if st, err := os.Stat(wantPortable); err != nil || !st.IsDir() {
		t.Fatalf("portable data dir not created: err=%v", err)
	}
}
