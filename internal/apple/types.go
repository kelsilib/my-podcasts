package apple

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Show represents an Apple Podcasts show (podcast series).
type Show struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	ArtistName       string   `json:"artistName"`
	Description      string   `json:"description"`
	ArtworkURL       string   `json:"artworkUrl"`
	BgColor          string   `json:"bgColor,omitempty"`
	HeroArtworkURL   string   `json:"heroArtworkUrl,omitempty"`
	HeroBgColor      string   `json:"heroBgColor,omitempty"`
	Genres           []string `json:"genres,omitempty"`
	TrackCount       int      `json:"trackCount,omitempty"`
	ReleaseFrequency string   `json:"releaseFrequency,omitempty"`
	FeedURL          string   `json:"feedUrl,omitempty"`
	ContentRating    string   `json:"contentRating,omitempty"`
	URL              string   `json:"url,omitempty"`
	Subscribed       bool     `json:"subscribed,omitempty"`
}

// Episode represents a single podcast episode.
type Episode struct {
	ID               string           `json:"id"`
	ShowID           string           `json:"showId"`
	ShowName         string           `json:"showName"`
	Name             string           `json:"name"`
	ArtistName       string           `json:"artistName"`
	Description      string           `json:"description"`
	ShortDescription string           `json:"shortDescription"`
	FullDescription  string           `json:"fullDescription,omitempty"`
	ArtworkURL       string           `json:"artworkUrl"`
	BgColor          string           `json:"bgColor,omitempty"`
	ArtworkOrigin    string           `json:"artworkOrigin,omitempty"`
	ShowArtworkURL   string           `json:"showArtworkUrl,omitempty"`
	ShowBgColor      string           `json:"showBgColor,omitempty"`
	HeroArtworkURL   string           `json:"heroArtworkUrl,omitempty"`
	HeroBgColor      string           `json:"heroBgColor,omitempty"`
	FeedURL          string           `json:"feedUrl,omitempty"`
	WebsiteURL       string           `json:"websiteUrl,omitempty"`
	AssetURL         string           `json:"assetUrl"`
	HLSURL           string           `json:"hlsUrl,omitempty"`
	PlaybackPosition PlaybackPosition `json:"-"`
	DurationMS       int64            `json:"durationMs"`
	ReleaseDate      string           `json:"releaseDate"`
	EpisodeNumber    int              `json:"episodeNumber,omitempty"`
	ContentRating    string           `json:"contentRating,omitempty"`
	IsSubscriberOnly bool             `json:"isSubscriberOnly,omitempty"`
	URL              string           `json:"url,omitempty"`
}

// PlaybackPosition is the account's last recorded position for an episode.
type PlaybackPosition struct {
	Known      bool
	PositionMS float64
	Completed  bool
	RecordedAt time.Time
}

// TranscriptLine represents a single paragraph or time-synced cue of an episode transcript or show timeline.
type TranscriptLine struct {
	StartSec     float64 `json:"startSec"`
	EndSec       float64 `json:"endSec"`
	HasTimestamp bool    `json:"hasTimestamp"`
	Speaker      string  `json:"speaker,omitempty"`
	Text         string  `json:"text"`
}

// EpisodeTranscript holds structured transcript cues or parsed episode notes/chapters.
type EpisodeTranscript struct {
	Source        string           `json:"source"`
	Lines         []TranscriptLine `json:"lines"`
	HasSyncedTime bool             `json:"hasSyncedTime"`
}

// HasTranscript reports whether this contains a real verbatim transcript (not plain show notes).
func (t *EpisodeTranscript) HasTranscript() bool {
	return t != nil && t.Source == "podcast:transcript" && len(t.Lines) > 0
}

// ChartsResult holds the top shows and trending episodes for a storefront.
type ChartsResult struct {
	TopShows         []Show
	TrendingEpisodes []Episode
}

// SearchResult holds catalog search results for shows and episodes.
type SearchResult struct {
	Shows    []Show
	Episodes []Episode
}

// ShowDetailResult holds a show and a page of its episodes.
type ShowDetailResult struct {
	Show             Show
	Episodes         []Episode
	NextEpisodesPath string
}

// FormatArtwork resolves an Apple artwork template URL to a concrete size.
func FormatArtwork(tpl string, size int) string {
	if tpl == "" {
		return ""
	}
	if size <= 0 {
		size = 300
	}
	fmtExt := "jpg"
	// Apple PodcastProtectedASource CDN presets accept 500x500bb.webp while rejecting 300/600 jpg.
	if strings.Contains(tpl, "PodcastProtectedASource") && size >= 300 {
		size = 500
		fmtExt = "webp"
	}
	s := strconv.Itoa(size)
	r := strings.NewReplacer(
		"{w}", s,
		"{h}", s,
		"{c}", "bb",
		"{f}", fmtExt,
	)
	return r.Replace(tpl)
}

// FormatHeroArtwork resolves Apple's showPageTall editorial artwork template
// to a 3:4 portrait WebP image supported by mzstatic.com CDN.
func FormatHeroArtwork(tpl string) string {
	if tpl == "" {
		return ""
	}
	if strings.Contains(tpl, "{c}") {
		r := strings.NewReplacer(
			"{w}", "450",
			"{h}", "600",
			"{c}", "bb",
			"{f}", "webp",
		)
		return r.Replace(tpl)
	}
	return FormatArtwork(tpl, 450)
}

// FormatDurationMS formats a millisecond duration as "mm:ss" or "h:mm:ss".
func FormatDurationMS(ms int64) string {
	if ms <= 0 {
		return "0:00"
	}
	totalSec := ms / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	sec := totalSec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// FormatShortDurationMS formats a millisecond duration compactly (e.g. "1h 46m", "27m").
func FormatShortDurationMS(ms int64) string {
	if ms <= 0 {
		return ""
	}
	mins := (ms + 30000) / 60000
	if mins < 1 {
		mins = 1
	}
	if mins >= 60 {
		h := mins / 60
		m := mins % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", mins)
}

// FormatDate trims an ISO-8601 timestamp to YYYY-MM-DD for display.
func FormatDate(iso string) string {
	if len(iso) >= 10 {
		return iso[:10]
	}
	return iso
}

// FormatRelativeDate formats an ISO-8601 date similarly to Apple Podcasts web ("23h ago", "6d ago", "Apr 15", "06/25/2025").
func FormatRelativeDate(iso string) string {
	if iso == "" {
		return ""
	}
	var t time.Time
	var err error
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		t, err = time.Parse(layout, iso)
		if err == nil {
			break
		}
	}
	if err != nil {
		return FormatDate(iso)
	}
	d := time.Since(t)
	if d >= 0 && d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", max(1, int(d.Hours())))
	}
	if d >= 0 && d < 7*24*time.Hour {
		return fmt.Sprintf("%dd ago", max(1, int(d.Hours()/24)))
	}
	if t.Year() == time.Now().Year() {
		return t.Format("Jan 2")
	}
	return t.Format("01/02/2006")
}

var (
	htmlBlockCloseRe = regexp.MustCompile(`(?i)</(?:p|div|li|h[1-6]|blockquote|tr)>|<br\s*/?>`)
	htmlTagRe        = regexp.MustCompile(`<[^>]+>`)
	dividerOnlyRe    = regexp.MustCompile(`^[-—–_=*·•\s\x{FFFD}]+$`)
	timestampLineRe  = regexp.MustCompile(`^\s*[\[\(【]?((?:\d{1,2}:)?\d{1,2}:\d{2})[\]\)】]?(?:\s*[-–—:：|]?\s*(.*))?$`)
)

// ParseHTMLParagraphs converts an HTML description into clean readable text paragraphs.
func ParseHTMLParagraphs(fullHTML, plainDesc string) []string {
	raw := strings.TrimSpace(fullHTML)
	if raw == "" {
		raw = strings.TrimSpace(plainDesc)
	}
	if raw == "" {
		return nil
	}
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = htmlBlockCloseRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	rawLines := strings.Split(s, "\n")
	out := make([]string, 0, len(rawLines))
	for _, ln := range rawLines {
		ln = strings.TrimSpace(ln)
		if ln == "" || dividerOnlyRe.MatchString(ln) {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func parseTimestampSec(tok string) (float64, bool) {
	parts := strings.Split(tok, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	vals := make([]int, len(parts))
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, false
		}
		vals[i] = v
	}
	if len(vals) == 2 {
		if vals[1] >= 60 {
			return 0, false
		}
		return float64(vals[0]*60 + vals[1]), true
	}
	if vals[1] >= 60 || vals[2] >= 60 {
		return 0, false
	}
	return float64(vals[0]*3600 + vals[1]*60 + vals[2]), true
}

// ParseTranscriptFromText extracts time-synced lines (chapters/cues) and full paragraphs
// from an episode's rich HTML or plain description.
func ParseTranscriptFromText(fullHTML, plainDesc string, durationMS int64) *EpisodeTranscript {
	paras := ParseHTMLParagraphs(fullHTML, plainDesc)
	if len(paras) == 0 {
		return &EpisodeTranscript{Source: "notes"}
	}

	maxSec := float64(durationMS) / 1000.0
	lines := make([]TranscriptLine, 0, len(paras))
	hasSynced := false
	for _, p := range paras {
		line := TranscriptLine{Text: p}
		if m := timestampLineRe.FindStringSubmatch(p); len(m) >= 2 {
			if sec, ok := parseTimestampSec(m[1]); ok && (maxSec <= 0 || sec <= maxSec+60) {
				line.HasTimestamp = true
				line.StartSec = sec
				hasSynced = true
			}
		}
		lines = append(lines, line)
	}

	// Fill EndSec for synced lines so active range lookup is exact.
	for i := range lines {
		if !lines[i].HasTimestamp {
			continue
		}
		end := maxSec
		for j := i + 1; j < len(lines); j++ {
			if lines[j].HasTimestamp && lines[j].StartSec > lines[i].StartSec {
				end = lines[j].StartSec
				break
			}
		}
		lines[i].EndSec = end
	}

	return &EpisodeTranscript{
		Source:        "notes",
		Lines:         lines,
		HasSyncedTime: hasSynced,
	}
}

// --- Raw Apple JSON:API wire types ---

type flexDescription struct {
	Standard string `json:"standard"`
	Short    string `json:"short"`
}

func (d *flexDescription) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		d.Standard = s
		return nil
	}
	type alias flexDescription
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*d = flexDescription(a)
	return nil
}

type apiArtwork struct {
	URL     string `json:"url"`
	BgColor string `json:"bgColor"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

type apiOffer struct {
	Kind       string `json:"kind"`
	Type       string `json:"type"`
	HLSURL     string `json:"hlsUrl"`
	DurationMS int64  `json:"durationInMilliseconds"`
}

type apiAttributes struct {
	Name             string                 `json:"name"`
	ArtistName       string                 `json:"artistName"`
	Description      flexDescription        `json:"description"`
	FullDescription  string                 `json:"fullDescription"`
	Artwork          apiArtwork             `json:"artwork"`
	ArtworkOrigin    string                 `json:"artworkOrigin"`
	EditorialArtwork map[string]*apiArtwork `json:"editorialArtwork"`
	GenreNames       []string               `json:"genreNames"`
	TrackCount       int                    `json:"trackCount"`
	ReleaseFrequency string                 `json:"releaseFrequency"`
	FeedURL          string                 `json:"feedUrl"`
	ContentRating    string                 `json:"contentRating"`
	URL              string                 `json:"url"`
	WebsiteURL       string                 `json:"websiteUrl"`
	AssetURL         string                 `json:"assetUrl"`
	DurationMS       int64                  `json:"durationInMilliseconds"`
	ReleaseDateTime  string                 `json:"releaseDateTime"`
	EpisodeNumber    int                    `json:"episodeNumber"`
	Offers           []apiOffer             `json:"offers"`
	PositionMS       *float64               `json:"positionInMilliseconds"`
	Completed        *bool                  `json:"completed"`
	RecordedAt       string                 `json:"recordedAtTimestamp"`
}

type apiRelationshipList struct {
	Data []apiResource `json:"data"`
	Next string        `json:"next"`
}

type apiRelationships struct {
	Episodes         *apiRelationshipList `json:"episodes"`
	Podcast          *apiRelationshipList `json:"podcast"`
	PlaybackPosition *apiRelationshipList `json:"playback-position"`
}

type apiResource struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Href          string            `json:"href"`
	Attributes    apiAttributes     `json:"attributes"`
	Relationships *apiRelationships `json:"relationships"`
}

type apiChartGroup struct {
	DisplayName string        `json:"displayName"`
	Chart       string        `json:"chart"`
	Data        []apiResource `json:"data"`
	Next        string        `json:"next"`
}

type apiSearchGroup struct {
	Data []apiResource `json:"data"`
	Next string        `json:"next"`
}

type chartsResponse struct {
	Results struct {
		Podcasts        []apiChartGroup `json:"podcasts"`
		PodcastEpisodes []apiChartGroup `json:"podcast-episodes"`
	} `json:"results"`
	Included []apiResource `json:"included"`
}

type searchResponse struct {
	Results struct {
		Podcasts        apiSearchGroup `json:"podcasts"`
		PodcastEpisodes apiSearchGroup `json:"podcast-episodes"`
	} `json:"results"`
	Included []apiResource `json:"included"`
}

type dataResponse struct {
	Data     []apiResource `json:"data"`
	Included []apiResource `json:"included"`
	Next     string        `json:"next"`
}

func parseRecordedAt(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999-0700",
		"2006-01-02T15:04:05-0700",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func extractPlaybackPosition(r apiResource, included map[string]apiResource) PlaybackPosition {
	rels := r.Relationships
	if (rels == nil || rels.PlaybackPosition == nil || len(rels.PlaybackPosition.Data) == 0) && r.ID != "" && included != nil {
		if full, ok := included["podcast-episodes:"+r.ID]; ok && full.Relationships != nil {
			rels = full.Relationships
		}
	}
	if rels == nil || rels.PlaybackPosition == nil || len(rels.PlaybackPosition.Data) == 0 {
		return PlaybackPosition{}
	}
	ref := rels.PlaybackPosition.Data[0]
	attr := ref.Attributes
	if included != nil {
		key := ref.Type + ":" + ref.ID
		if ref.Type == "" {
			key = "playback-positions:" + ref.ID
		}
		if resource, ok := included[key]; ok {
			if resource.Attributes.PositionMS != nil || resource.Attributes.Completed != nil {
				attr = resource.Attributes
			}
		}
	}
	if attr.PositionMS == nil && attr.Completed == nil {
		return PlaybackPosition{}
	}
	playback := PlaybackPosition{
		Known:      true,
		RecordedAt: parseRecordedAt(attr.RecordedAt),
	}
	if attr.PositionMS != nil {
		playback.PositionMS = *attr.PositionMS
	}
	if attr.Completed != nil {
		playback.Completed = *attr.Completed
	}
	return playback
}

func heroArtFromEditorial(ed map[string]*apiArtwork) (url, bg string) {
	if ed == nil {
		return "", ""
	}
	if tall := ed["showPageTall"]; tall != nil && tall.URL != "" {
		return tall.URL, tall.BgColor
	}
	return "", ""
}

func toShow(r apiResource) Show {
	heroArt, heroBg := heroArtFromEditorial(r.Attributes.EditorialArtwork)
	return Show{
		ID:               r.ID,
		Name:             r.Attributes.Name,
		ArtistName:       r.Attributes.ArtistName,
		Description:      r.Attributes.Description.Standard,
		ArtworkURL:       r.Attributes.Artwork.URL,
		BgColor:          r.Attributes.Artwork.BgColor,
		HeroArtworkURL:   heroArt,
		HeroBgColor:      heroBg,
		Genres:           r.Attributes.GenreNames,
		TrackCount:       r.Attributes.TrackCount,
		ReleaseFrequency: r.Attributes.ReleaseFrequency,
		FeedURL:          r.Attributes.FeedURL,
		ContentRating:    r.Attributes.ContentRating,
		URL:              r.Attributes.URL,
	}
}

func toEpisode(r apiResource, fallbackShow *Show, included map[string]apiResource) Episode {
	var showID, showName, showArtwork, showBg, heroArt, heroBg, feedURL string
	if fallbackShow != nil {
		showID = fallbackShow.ID
		showName = fallbackShow.Name
		showArtwork = fallbackShow.ArtworkURL
		showBg = fallbackShow.BgColor
		heroArt = fallbackShow.HeroArtworkURL
		heroBg = fallbackShow.HeroBgColor
		feedURL = fallbackShow.FeedURL
	}
	applyPodcastAttr := func(attr apiAttributes) {
		if showName == "" && attr.Name != "" {
			showName = attr.Name
		}
		if showArtwork == "" && attr.Artwork.URL != "" {
			showArtwork = attr.Artwork.URL
		}
		if showBg == "" && attr.Artwork.BgColor != "" {
			showBg = attr.Artwork.BgColor
		}
		if heroArt == "" {
			hArt, hBg := heroArtFromEditorial(attr.EditorialArtwork)
			if hArt != "" {
				heroArt = hArt
				heroBg = hBg
			}
		}
		if feedURL == "" && attr.FeedURL != "" {
			feedURL = attr.FeedURL
		}
	}
	if r.Relationships != nil && r.Relationships.Podcast != nil && len(r.Relationships.Podcast.Data) > 0 {
		rel := r.Relationships.Podcast.Data[0]
		if showID == "" {
			showID = rel.ID
		}
		applyPodcastAttr(rel.Attributes)
		if inc, ok := included[rel.Type+":"+rel.ID]; ok {
			applyPodcastAttr(inc.Attributes)
		}
	}
	if showName == "" {
		showName = r.Attributes.ArtistName
	}

	playback := extractPlaybackPosition(r, included)

	artwork := r.Attributes.Artwork.URL
	if artwork == "" {
		artwork = showArtwork
	}
	bgColor := r.Attributes.Artwork.BgColor
	if bgColor == "" {
		bgColor = showBg
	}

	var hlsURL string
	hasSTDQ := r.Attributes.AssetURL != ""
	hasPaid := false
	dur := r.Attributes.DurationMS
	for _, off := range r.Attributes.Offers {
		if off.Type == "STDQ" {
			hasSTDQ = true
		}
		if off.Type == "PSUB" || off.Type == "PLUS" {
			hasPaid = true
			if off.HLSURL != "" && hlsURL == "" {
				hlsURL = off.HLSURL
			}
			if dur == 0 && off.DurationMS > 0 {
				dur = off.DurationMS
			}
		}
	}

	desc := r.Attributes.Description.Standard
	if desc == "" {
		desc = r.Attributes.Description.Short
	}

	return Episode{
		ID:               r.ID,
		ShowID:           showID,
		ShowName:         showName,
		Name:             r.Attributes.Name,
		ArtistName:       r.Attributes.ArtistName,
		Description:      desc,
		ShortDescription: r.Attributes.Description.Short,
		FullDescription:  r.Attributes.FullDescription,
		ArtworkURL:       artwork,
		BgColor:          bgColor,
		ArtworkOrigin:    r.Attributes.ArtworkOrigin,
		ShowArtworkURL:   showArtwork,
		ShowBgColor:      showBg,
		HeroArtworkURL:   heroArt,
		HeroBgColor:      heroBg,
		FeedURL:          feedURL,
		WebsiteURL:       r.Attributes.WebsiteURL,
		AssetURL:         r.Attributes.AssetURL,
		PlaybackPosition: playback,
		HLSURL:           hlsURL,
		DurationMS:       dur,
		ReleaseDate:      r.Attributes.ReleaseDateTime,
		EpisodeNumber:    r.Attributes.EpisodeNumber,
		ContentRating:    r.Attributes.ContentRating,
		IsSubscriberOnly: !hasSTDQ && hasPaid,
		URL:              r.Attributes.URL,
	}
}
