package player

import (
	"testing"
	"time"

	"apple-podcasts/internal/apple"
)

func TestTrackEndPreservesCompletion(t *testing.T) {
	first := apple.Episode{ID: "first", DurationMS: 12000}
	next := apple.Episode{ID: "next"}
	for _, tc := range []struct {
		name        string
		queue       []apple.Episode
		wantEpisode string
	}{
		{"autoplay", []apple.Episode{first, next}, next.ID},
		{"last episode", []apple.Episode{first}, first.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var updates []bool
			p := &Player{
				current: &first, queue: tc.queue, restoredPos: 12 * time.Second,
				onProgress: func(ep apple.Episode, _ float64, completed bool) {
					if ep.ID == first.ID {
						updates = append(updates, completed)
					}
				},
			}
			p.handleTrackEnd(p.gen)
			if got := p.Snapshot().Episode.ID; got != tc.wantEpisode {
				t.Errorf("current episode = %q, want %q", got, tc.wantEpisode)
			}
			p.Stop()
			if len(updates) != 1 || !updates[0] {
				t.Errorf("completed episode progress updates = %v, want [true]", updates)
			}
		})
	}
}

func TestSnapshotQueueDoesNotChangeOnMetadataUpdate(t *testing.T) {
	episode := apple.Episode{ID: "first", Name: "before"}
	p := &Player{current: &episode, queue: []apple.Episode{episode}}
	snapshot := p.Snapshot()
	p.UpdateCurrentEpisode(apple.Episode{ID: episode.ID, Name: "after"})
	if snapshot.Queue[0].Name != "before" {
		t.Errorf("snapshot queue name = %q, want original name", snapshot.Queue[0].Name)
	}
}

func TestEnqueueNextAndLast(t *testing.T) {
	ep1 := apple.Episode{ID: "ep-1"}
	ep2 := apple.Episode{ID: "ep-2"}
	ep3 := apple.Episode{ID: "ep-3"}
	ep4 := apple.Episode{ID: "ep-4"}

	p := &Player{current: &ep1, queue: []apple.Episode{ep1, ep2}, queueIndex: 0}
	p.EnqueueNext(ep3)
	snap := p.Snapshot()
	if len(snap.Queue) != 3 || snap.Queue[0].ID != "ep-1" || snap.Queue[1].ID != "ep-3" || snap.Queue[2].ID != "ep-2" {
		t.Fatalf("after EnqueueNext(ep3) got %+v", snap.Queue)
	}

	p.EnqueueLast(ep4)
	p.EnqueueLast(ep3) // moves existing upcoming ep3 to end without duplicate
	snap = p.Snapshot()
	if len(snap.Queue) != 4 || snap.Queue[1].ID != "ep-2" || snap.Queue[2].ID != "ep-4" || snap.Queue[3].ID != "ep-3" {
		t.Fatalf("after EnqueueLast got %+v", snap.Queue)
	}
}
