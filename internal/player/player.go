package player

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"

	"apple-podcasts/internal/apple"
)

// SpeedOptions lists the supported podcast playback rates.
var SpeedOptions = []float64{1.0, 1.25, 1.5, 1.75, 2.0, 0.75}

// Snapshot is an immutable view of current playback state for UI rendering.
type Snapshot struct {
	Episode      *apple.Episode
	Queue        []apple.Episode
	QueueIndex   int
	Playing      bool
	Loading      bool
	Err          string
	Position     time.Duration
	Duration     time.Duration
	Speed        float64
	Volume       float64
	DownloadFrac float64
}

// Player manages progressive episode streaming, WSOLA speed control, and
// Windows System Media Transport Controls (NowPlaying).
type Player struct {
	mu         sync.Mutex
	onChange   func()
	onProgress func(ep apple.Episode, posSec float64, completed bool)

	queue      []apple.Episode
	queueIndex int
	current    *apple.Episode

	out        *audio.Player
	dl         *download
	loadCancel context.CancelFunc
	gen        uint64

	playing     bool
	loading     bool
	completed   bool
	err         string
	speed       float64
	volume      float64
	restoredPos time.Duration
	artData     []byte
}

// New constructs a Player and wires Windows media keys via mygo.NowPlaying.
func New(onChange func(), onProgress func(ep apple.Episode, posSec float64, completed bool)) *Player {
	audio.SetLatency(audio.LatencyPlayback)
	p := &Player{
		onChange:   onChange,
		onProgress: onProgress,
		queueIndex: -1,
		speed:      1.0,
		volume:     0.8,
	}

	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) {
		switch cmd {
		case mygo.MediaPlay:
			p.Play()
		case mygo.MediaPause:
			p.Pause()
		case mygo.MediaStop:
			p.Pause()
		case mygo.MediaNext:
			p.Next()
		case mygo.MediaPrevious:
			p.Prev()
		case mygo.MediaSeek:
			p.Seek(pos)
		}
	})

	return p
}

// Snapshot returns a consistent copy of the current player state.
func (p *Player) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	var epCopy *apple.Episode
	if p.current != nil {
		cp := *p.current
		epCopy = &cp
	}

	var pos, dur time.Duration
	if p.out != nil {
		pos = p.out.Position()
		dur = p.out.Duration()
	} else if p.current != nil {
		pos = p.restoredPos
	}
	if dur <= 0 && p.current != nil && p.current.DurationMS > 0 {
		dur = time.Duration(p.current.DurationMS) * time.Millisecond
	}

	dlFrac := 0.0
	if p.dl != nil {
		dlFrac = p.dl.Progress()
	}

	return Snapshot{
		Episode:      epCopy,
		Queue:        slices.Clone(p.queue),
		QueueIndex:   p.queueIndex,
		Playing:      p.playing,
		Loading:      p.loading,
		Err:          p.err,
		Position:     pos,
		Duration:     dur,
		Speed:        p.speed,
		Volume:       p.volume,
		DownloadFrac: dlFrac,
	}
}

// SetInitialPrefs configures saved volume and playback speed before playback starts.
func (p *Player) SetInitialPrefs(volume, speed float64) {
	p.mu.Lock()
	if volume >= 0 && volume <= 1 {
		p.volume = volume
	}
	if speed >= 0.5 && speed <= 4.0 {
		p.speed = speed
	}
	out := p.out
	volume, speed = p.volume, p.speed
	p.mu.Unlock()

	if out != nil {
		out.SetVolume(volume).SetPreservesPitch(true).SetSpeed(speed)
	}
}

// PlayEpisode starts streaming ep (and sets queue as the active playback queue).
func (p *Player) PlayEpisode(ep apple.Episode, queue []apple.Episode, startPos time.Duration) {
	p.saveCurrentProgress(false)
	p.startEpisode(ep, queue, startPos, nil)
}

func (p *Player) startEpisode(ep apple.Episode, queue []apple.Episode, startPos time.Duration, previousGen *uint64) {
	p.mu.Lock()
	if previousGen != nil && p.gen != *previousGen {
		p.mu.Unlock()
		return
	}
	p.gen++
	gen := p.gen
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	oldOut := p.out
	p.out = nil
	p.dl = nil

	if len(queue) == 0 {
		queue = []apple.Episode{ep}
	}
	p.queue = append([]apple.Episode(nil), queue...)
	p.queueIndex = 0
	for i, q := range p.queue {
		if q.ID == ep.ID {
			p.queueIndex = i
			break
		}
	}

	epCopy := ep
	p.current = &epCopy
	p.restoredPos = max(0, startPos)
	p.completed = false
	p.artData = nil
	p.err = ""

	if ep.AssetURL == "" {
		p.loading = false
		p.playing = false
		if ep.IsSubscriberOnly {
			p.err = "该单集为 Apple Podcasts 订阅专享加密内容，请使用官方网页版播放"
		} else {
			p.err = "该单集暂无可用的音频流地址"
		}
		p.mu.Unlock()
		if oldOut != nil {
			oldOut.Close()
		}
		p.announceNowPlaying()
		p.notifyChanged()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.loadCancel = cancel
	p.loading = true
	p.playing = true
	p.mu.Unlock()

	if oldOut != nil {
		oldOut.Close()
	}

	p.announceNowPlaying()
	p.notifyChanged()

	go p.fetchArtwork(gen, apple.FormatArtwork(ep.ArtworkURL, 600))
	go p.openAndPlay(ctx, gen, ep)
}

// Restore loads a saved episode, queue, and paused position without starting streaming
// until the user explicitly presses Play or jumps to playback.
func (p *Player) Restore(ep apple.Episode, queue []apple.Episode, pos time.Duration) {
	if ep.ID == "" {
		return
	}
	if pos < 0 {
		pos = 0
	}
	dur := time.Duration(ep.DurationMS) * time.Millisecond
	if dur > 0 && pos >= dur {
		pos = max(0, dur-2*time.Second)
	}

	p.mu.Lock()
	p.gen++
	gen := p.gen
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	oldOut := p.out
	p.out = nil
	p.dl = nil

	if len(queue) == 0 {
		queue = []apple.Episode{ep}
	}
	p.queue = append([]apple.Episode(nil), queue...)
	p.queueIndex = 0
	for i, q := range p.queue {
		if q.ID == ep.ID {
			p.queueIndex = i
			break
		}
	}

	epCopy := ep
	p.current = &epCopy
	p.playing = false
	p.loading = false
	p.completed = false
	p.err = ""
	p.restoredPos = pos
	p.artData = nil
	p.mu.Unlock()

	if oldOut != nil {
		oldOut.Close()
	}

	p.announceNowPlaying()
	p.notifyChanged()
	go p.fetchArtwork(gen, apple.FormatArtwork(ep.ArtworkURL, 600))
}

func (p *Player) openAndPlay(ctx context.Context, gen uint64, ep apple.Episode) {
	dec, dl, err := openHTTPStream(ctx, ep.AssetURL, ep.DurationMS)

	p.mu.Lock()
	if p.gen != gen {
		p.mu.Unlock()
		if dec != nil {
			if c, ok := dec.(io.Closer); ok {
				c.Close()
			}
		} else if dl != nil {
			dl.Close()
		}
		return
	}

	if err != nil {
		p.loading = false
		p.playing = false
		p.err = "播放失败: " + err.Error()
		p.mu.Unlock()
		p.announceNowPlaying()
		p.notifyChanged()
		return
	}

	out := audio.NewPlayer(dec)
	out.SetVolume(p.volume)
	out.SetPreservesPitch(true)
	out.SetSpeed(p.speed)
	seekPos := p.restoredPos
	if seekPos > 0 {
		if dur := out.Duration(); dur > 0 && seekPos >= dur {
			seekPos = max(0, dur-500*time.Millisecond)
		}
		out.Seek(seekPos)
	}

	out.OnEnd(func() {
		p.handleTrackEnd(gen)
	})

	p.out = out
	p.dl = dl
	p.loading = false
	shouldPlay := p.playing
	p.mu.Unlock()

	if shouldPlay {
		out.Play()
	}
	p.announceNowPlaying()
	p.notifyChanged()
}

func (p *Player) handleTrackEnd(gen uint64) {
	p.mu.Lock()
	if p.gen != gen || p.current == nil {
		p.mu.Unlock()
		return
	}
	ep := *p.current
	dur := p.currentDurationLocked()
	p.completed = true
	p.playing = false
	nextIdx := p.queueIndex + 1
	hasNext := nextIdx >= 0 && nextIdx < len(p.queue)
	var nextEp apple.Episode
	var queueCopy []apple.Episode
	if hasNext {
		nextEp = p.queue[nextIdx]
		queueCopy = slices.Clone(p.queue)
	}
	p.mu.Unlock()

	if p.onProgress != nil {
		p.onProgress(ep, dur.Seconds(), true)
	}

	if hasNext {
		p.startEpisode(nextEp, queueCopy, 0, &gen)
		return
	}
	p.syncNowPlayingPlayback()
	p.notifyChanged()
}

// TogglePlay toggles between playing and paused.
func (p *Player) TogglePlay() {
	p.mu.Lock()
	if p.current == nil {
		p.mu.Unlock()
		return
	}
	if p.playing {
		p.mu.Unlock()
		p.Pause()
	} else {
		p.mu.Unlock()
		p.Play()
	}
}

// Play starts or resumes playback.
func (p *Player) Play() {
	p.mu.Lock()
	if p.current == nil {
		p.mu.Unlock()
		return
	}
	if p.out == nil && !p.loading && p.current.AssetURL != "" {
		ep := *p.current
		queue := slices.Clone(p.queue)
		startPos := p.restoredPos
		p.mu.Unlock()
		p.PlayEpisode(ep, queue, startPos)
		return
	}
	p.playing = true
	p.completed = false
	out := p.out
	p.mu.Unlock()

	if out != nil {
		out.Play()
	}
	p.syncNowPlayingPlayback()
	p.notifyChanged()
}

// Pause pauses playback and persists progress.
func (p *Player) Pause() {
	p.mu.Lock()
	p.playing = false
	out := p.out
	p.mu.Unlock()

	if out != nil {
		out.Pause()
	}
	p.saveCurrentProgress(false)
	p.syncNowPlayingPlayback()
	p.notifyChanged()
}

// Stop stops playback and clears the active audio player.
func (p *Player) Stop() {
	p.saveCurrentProgress(false)
	p.mu.Lock()
	p.gen++
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	out := p.out
	if out != nil {
		p.restoredPos = out.Position()
	}
	p.out = nil
	p.dl = nil
	p.playing = false
	p.loading = false
	p.mu.Unlock()

	if out != nil {
		out.Close()
	}
	mygo.NowPlaying.Clear()
	p.notifyChanged()
}

// Seek jumps to pos in the current episode.
func (p *Player) Seek(pos time.Duration) {
	p.mu.Lock()
	if p.current == nil {
		p.mu.Unlock()
		return
	}
	out := p.out
	dur := p.currentDurationLocked()
	if pos < 0 {
		pos = 0
	}
	if dur > 0 && pos > dur {
		pos = max(0, dur-500*time.Millisecond)
	}
	if out == nil {
		p.restoredPos = pos
	}
	p.completed = false
	p.mu.Unlock()

	if out != nil {
		out.Seek(pos)
	}
	p.saveCurrentProgress(true)
	p.syncNowPlayingPlayback()
	p.notifyChanged()
}

// ApplyResumePosition applies an account's resume point to a paused episode.
// Unlike a user seek, this does not emit a new progress event.
func (p *Player) ApplyResumePosition(id string, pos time.Duration) {
	p.mu.Lock()
	if p.current == nil || p.current.ID != id || p.playing || p.loading {
		p.mu.Unlock()
		return
	}
	pos = max(0, pos)
	if dur := p.currentDurationLocked(); dur > 0 && pos >= dur {
		pos = max(0, dur-500*time.Millisecond)
	}
	p.restoredPos = pos
	p.completed = false
	if p.out != nil {
		p.out.Seek(pos)
	}
	p.mu.Unlock()
	p.syncNowPlayingPlayback()
	p.notifyChanged()
}

// Skip seeks relative to the current position (e.g., -15s or +30s).
func (p *Player) Skip(delta time.Duration) {
	p.mu.Lock()
	if p.current == nil {
		p.mu.Unlock()
		return
	}
	out := p.out
	curPos := p.restoredPos
	if out != nil {
		curPos = out.Position()
	}
	p.mu.Unlock()
	p.Seek(curPos + delta)
}

// SetSpeed updates playback speed while preserving vocal pitch via WSOLA.
func (p *Player) SetSpeed(speed float64) {
	if speed <= 0 {
		return
	}
	p.mu.Lock()
	p.speed = speed
	out := p.out
	p.mu.Unlock()

	if out != nil {
		out.SetPreservesPitch(true).SetSpeed(speed)
	}
	p.announceNowPlaying()
	p.notifyChanged()
}

// CycleSpeed switches to the next preset podcast speed.
func (p *Player) CycleSpeed() float64 {
	p.mu.Lock()
	cur := p.speed
	p.mu.Unlock()

	next := SpeedOptions[0]
	for i, s := range SpeedOptions {
		if cur >= s-0.01 && cur <= s+0.01 {
			next = SpeedOptions[(i+1)%len(SpeedOptions)]
			break
		}
	}
	p.SetSpeed(next)
	return next
}

// SetVolume updates playback volume in [0, 1].
func (p *Player) SetVolume(v float64) {
	v = min(1, max(0, v))
	p.mu.Lock()
	p.volume = v
	out := p.out
	p.mu.Unlock()

	if out != nil {
		out.SetVolume(v)
	}
}

// Next skips to the next episode in the queue, if available.
func (p *Player) Next() {
	p.mu.Lock()
	nextIdx := p.queueIndex + 1
	if nextIdx < 0 || nextIdx >= len(p.queue) {
		p.mu.Unlock()
		return
	}
	nextEp := p.queue[nextIdx]
	queue := slices.Clone(p.queue)
	p.mu.Unlock()
	p.PlayEpisode(nextEp, queue, 0)
}

// Prev skips to the previous episode in the queue, or restarts the current episode.
func (p *Player) Prev() {
	p.mu.Lock()
	out := p.out
	curPos := p.restoredPos
	if out != nil {
		curPos = out.Position()
	}
	prevIdx := p.queueIndex - 1
	var prevEp apple.Episode
	var queue []apple.Episode
	hasPrev := prevIdx >= 0 && prevIdx < len(p.queue)
	if hasPrev {
		prevEp = p.queue[prevIdx]
		queue = slices.Clone(p.queue)
	}
	p.mu.Unlock()

	if curPos > 5*time.Second {
		p.Seek(0)
		return
	}
	if hasPrev {
		p.PlayEpisode(prevEp, queue, 0)
	}
}

// EnqueueNext inserts ep immediately after the current episode in the playback queue.
func (p *Player) EnqueueNext(ep apple.Episode) {
	if ep.ID == "" {
		return
	}
	p.mu.Lock()
	q := slices.Clone(p.queue)
	if p.current == nil || len(q) == 0 {
		p.queue = []apple.Episode{ep}
		p.queueIndex = 0
		p.mu.Unlock()
		p.notifyChanged()
		return
	}
	curIdx := min(max(p.queueIndex, 0), len(q)-1)
	head := slices.Clone(q[:curIdx+1])
	var tail []apple.Episode
	for _, item := range q[curIdx+1:] {
		if item.ID != ep.ID {
			tail = append(tail, item)
		}
	}
	nextQueue := make([]apple.Episode, 0, len(head)+1+len(tail))
	nextQueue = append(nextQueue, head...)
	nextQueue = append(nextQueue, ep)
	nextQueue = append(nextQueue, tail...)
	p.queue = nextQueue
	p.mu.Unlock()
	p.notifyChanged()
}

// EnqueueLast appends ep to the end of the playback queue.
func (p *Player) EnqueueLast(ep apple.Episode) {
	if ep.ID == "" {
		return
	}
	p.mu.Lock()
	q := slices.Clone(p.queue)
	if p.current == nil || len(q) == 0 {
		p.queue = []apple.Episode{ep}
		p.queueIndex = 0
		p.mu.Unlock()
		p.notifyChanged()
		return
	}
	curIdx := min(max(p.queueIndex, 0), len(q)-1)
	nextQueue := make([]apple.Episode, 0, len(q)+1)
	nextQueue = append(nextQueue, q[:curIdx+1]...)
	for _, item := range q[curIdx+1:] {
		if item.ID != ep.ID {
			nextQueue = append(nextQueue, item)
		}
	}
	nextQueue = append(nextQueue, ep)
	p.queue = nextQueue
	p.mu.Unlock()
	p.notifyChanged()
}

// SaveProgress triggers the onProgress callback with the current episode position.
func (p *Player) SaveProgress() {
	p.saveCurrentProgress(true)
}

func (p *Player) saveCurrentProgress(allowZero bool) {
	p.mu.Lock()
	if p.completed {
		p.mu.Unlock()
		return
	}
	ep := p.current
	out := p.out
	pos := p.restoredPos.Seconds()
	if out != nil {
		pos = out.Position().Seconds()
	}
	p.mu.Unlock()

	if ep == nil || p.onProgress == nil {
		return
	}
	if !allowZero && pos < 1 {
		return
	}
	p.onProgress(*ep, pos, false)
}

// UpdateCurrentEpisode refreshes metadata (such as feedUrl or assetUrl) on the matching loaded episode.
func (p *Player) UpdateCurrentEpisode(ep apple.Episode) {
	if ep.ID == "" {
		return
	}
	p.mu.Lock()
	if p.current != nil && p.current.ID == ep.ID {
		cp := ep
		p.current = &cp
	}
	for i := range p.queue {
		if p.queue[i].ID == ep.ID {
			p.queue[i] = ep
		}
	}
	p.mu.Unlock()
}

func (p *Player) currentDurationLocked() time.Duration {
	if p.out != nil {
		if d := p.out.Duration(); d > 0 {
			return d
		}
	}
	if p.current != nil && p.current.DurationMS > 0 {
		return time.Duration(p.current.DurationMS) * time.Millisecond
	}
	return 0
}

func (p *Player) fetchArtwork(gen uint64, artURL string) {
	if artURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artURL, nil)
	if err != nil {
		return
	}
	res, err := streamHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil || len(data) == 0 {
		return
	}

	p.mu.Lock()
	if p.gen != gen {
		p.mu.Unlock()
		return
	}
	p.artData = data
	p.mu.Unlock()

	p.announceNowPlaying()
}

func (p *Player) announceNowPlaying() {
	p.mu.Lock()
	ep := p.current
	playing := p.playing
	speed := p.speed
	art := p.artData
	var pos time.Duration
	if p.out != nil {
		pos = p.out.Position()
	} else {
		pos = p.restoredPos
	}
	dur := p.currentDurationLocked()
	p.mu.Unlock()

	if ep == nil {
		mygo.NowPlaying.Clear()
		return
	}

	state := mygo.PlaybackPaused
	if playing {
		state = mygo.PlaybackPlaying
	}
	artist := ep.ShowName
	if artist == "" {
		artist = ep.ArtistName
	}

	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
		Title:    ep.Name,
		Artist:   artist,
		Album:    ep.ShowName,
		Artwork:  art,
		Duration: dur,
		Position: pos,
		Rate:     speed,
		State:    state,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext | mygo.MediaPrevious | mygo.MediaSeek,
	})
}

func (p *Player) syncNowPlayingPlayback() {
	p.mu.Lock()
	ep := p.current
	playing := p.playing
	var pos time.Duration
	if p.out != nil {
		pos = p.out.Position()
	} else {
		pos = p.restoredPos
	}
	p.mu.Unlock()

	if ep == nil {
		mygo.NowPlaying.Clear()
		return
	}
	state := mygo.PlaybackPaused
	if playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, pos)
}

func (p *Player) notifyChanged() {
	if p.onChange != nil {
		p.onChange()
	}
}
