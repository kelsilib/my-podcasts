package apple

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"
)

const (
	baseAPIURL = "https://amp-api.podcasts.apple.com"
	webOrigin  = "https://podcasts.apple.com"
	webReferer = "https://podcasts.apple.com/"
	userAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	accountURL = "https://amp-account.podcasts.apple.com/account/v2/web/info"
	defaultJWT = "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiIsImtpZCI6IkM0SjdHQlA3NEgifQ.eyJpc3MiOiJVTTdOOVJUVDdHIiwiaWF0IjoxNzk0MjU5NjcyLCJleHAiOjE4MDAzMDc2NzIsInJvb3RfaHR0cHNfb3JpZ2luIjpbImFwcGxlLmNvbSJdfQ.GFqTiqeJ4_eWP4owlTynDbzQWfo1YqQ8nDVQj2B1HuGuEAEA1DItfF863khxD7kkTOJe-gCxub_j_OCNnXToSQ"
)

var (
	// ErrUnauthorized indicates missing or expired user authentication.
	ErrUnauthorized = errors.New("apple podcasts: unauthorized")
	// ErrNotFound indicates the requested resource or empty user library returned HTTP 404.
	ErrNotFound = errors.New("apple podcasts: not found")

	scriptSrcRe = regexp.MustCompile(`src="(/assets/index~[^"]+\.js)"`)
	jwtRe       = regexp.MustCompile(`eyJ[a-zA-Z0-9_\-]+\.eyJ[a-zA-Z0-9_\-]+\.[a-zA-Z0-9_\-]+`)
)

// Client calls Apple Podcasts Web API endpoints on amp-api.podcasts.apple.com.
type Client struct {
	mu                sync.RWMutex
	refreshMu         sync.Mutex
	bearerToken       string
	mediaUserToken    string
	cookieHeader      string
	authContext       context.Context
	cancelAuth        context.CancelFunc
	storefront        string
	accountStorefront string
	httpClient        *http.Client
}

// NewClient constructs a Client initialized with the embedded Apple Podcasts web JWT.
func NewClient() *Client {
	return &Client{
		bearerToken: defaultJWT,
		storefront:  "us",
		httpClient: &http.Client{
			Transport: netproxy.Transport(),
			Timeout:   25 * time.Second,
		},
	}
}

// SetStorefront updates the storefront code (e.g. "us", "cn", "jp", "gb").
func (c *Client) SetStorefront(sf string) {
	sf = strings.ToLower(strings.TrimSpace(sf))
	if sf == "" {
		sf = "us"
	}
	c.mu.Lock()
	c.storefront = sf
	c.mu.Unlock()
}

// Storefront returns the current storefront code.
func (c *Client) Storefront() string {
	c.mu.RLock()
	sf := c.storefront
	c.mu.RUnlock()
	if sf == "" {
		return "us"
	}
	return sf
}

// SetAccountStorefront stores the signed-in Apple ID's home storefront code.
func (c *Client) SetAccountStorefront(sf string) {
	sf = strings.ToLower(strings.TrimSpace(sf))
	c.mu.Lock()
	c.accountStorefront = sf
	c.mu.Unlock()
}

// AccountStorefront returns the signed-in Apple ID's home storefront code if known.
func (c *Client) AccountStorefront() string {
	c.mu.RLock()
	sf := c.accountStorefront
	c.mu.RUnlock()
	return sf
}

// SetAuth sets the signed-in user's media-user-token and web session cookies.
func (c *Client) SetAuth(mediaUserToken, cookieHeader string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelAuth != nil {
		c.cancelAuth()
	}
	c.mediaUserToken = strings.TrimSpace(mediaUserToken)
	c.cookieHeader = strings.TrimSpace(cookieHeader)
	c.accountStorefront = ""
	c.authContext, c.cancelAuth = nil, nil
	if c.mediaUserToken != "" {
		c.authContext, c.cancelAuth = context.WithCancel(context.Background())
	}
}

// ClearAuth removes user credentials from the client.
func (c *Client) ClearAuth() {
	c.SetAuth("", "")
}

// HasAuth reports whether a media-user-token is configured.
func (c *Client) HasAuth() bool {
	c.mu.RLock()
	ok := c.mediaUserToken != ""
	c.mu.RUnlock()
	return ok
}

// AuthContext is cancelled when the active account session changes.
func (c *Client) AuthContext() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authContext
}

// MediaUserToken returns the active media-user-token if signed in.
func (c *Client) MediaUserToken() string {
	c.mu.RLock()
	mut := c.mediaUserToken
	c.mu.RUnlock()
	return mut
}

// RefreshBearerToken scrapes podcasts.apple.com for the latest public developer JWT.
func (c *Client) RefreshBearerToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webOrigin+"/us/browse", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	res.Body.Close()
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch browse page: %s", res.Status)
	}
	m := scriptSrcRe.FindSubmatch(body)
	if len(m) < 2 {
		return "", errors.New("apple podcasts bundle script not found")
	}
	jsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, webOrigin+string(m[1]), nil)
	if err != nil {
		return "", err
	}
	jsReq.Header.Set("User-Agent", userAgent)
	jsRes, err := c.httpClient.Do(jsReq)
	if err != nil {
		return "", err
	}
	jsBody, err := io.ReadAll(io.LimitReader(jsRes.Body, 16<<20))
	jsRes.Body.Close()
	if err != nil {
		return "", err
	}
	if jsRes.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch bundle js: %s", jsRes.Status)
	}
	tok := jwtRe.FindString(string(jsBody))
	if tok == "" {
		return "", errors.New("bearer JWT not found in bundle")
	}
	c.mu.Lock()
	c.bearerToken = tok
	c.mu.Unlock()
	return tok, nil
}

func (c *Client) doJSON(ctx context.Context, method, pathOrURL string, bodyObj any, personalized bool, out any) error {
	return c.doJSONAttempt(ctx, method, pathOrURL, bodyObj, personalized, out, true)
}

func (c *Client) doJSONAttempt(ctx context.Context, method, pathOrURL string, bodyObj any, personalized bool, out any, allowRefresh bool) error {
	fullURL := pathOrURL
	if strings.HasPrefix(fullURL, "/") {
		fullURL = baseAPIURL + fullURL
	}

	var payload []byte
	if bodyObj != nil {
		var err error
		payload, err = json.Marshal(bodyObj)
		if err != nil {
			return err
		}
	}

	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return err
	}

	c.mu.RLock()
	bearer := c.bearerToken
	mut := c.mediaUserToken
	cookie := c.cookieHeader
	c.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if personalized && mut == "" {
		return ErrUnauthorized
	}

	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Origin", webOrigin)
	req.Header.Set("Referer", webReferer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if personalized {
		req.Header.Set("media-user-token", mut)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	res.Body.Close()
	if readErr != nil {
		return readErr
	}

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		shouldRefresh := allowRefresh && (res.StatusCode == http.StatusUnauthorized || !personalized)
		if shouldRefresh {
			c.refreshMu.Lock()
			c.mu.RLock()
			curBearer := c.bearerToken
			c.mu.RUnlock()
			var rerr error
			if curBearer == bearer {
				_, rerr = c.RefreshBearerToken(ctx)
			}
			c.refreshMu.Unlock()
			if rerr == nil {
				return c.doJSONAttempt(ctx, method, pathOrURL, bodyObj, personalized, out, false)
			}
		}
		if personalized {
			return ErrUnauthorized
		}
		return fmt.Errorf("apple api %s: %s", res.Status, strings.TrimSpace(string(raw)))
	}

	if res.StatusCode == http.StatusNoContent {
		return nil
	}
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(raw)))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("apple api %s: %s", res.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) doCatalogJSON(ctx context.Context, authPath, anonPath string, out any) (usedAuth bool, err error) {
	if c.HasAuth() && authPath != "" {
		if err := c.doJSON(ctx, http.MethodGet, authPath, nil, true, out); err == nil {
			return true, nil
		} else if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return false, c.doJSON(ctx, http.MethodGet, anonPath, nil, false, out)
}

func hasAnyKnownPosition(eps []Episode) bool {
	for _, ep := range eps {
		if ep.PlaybackPosition.Known {
			return true
		}
	}
	return false
}

func (c *Client) shouldHydratePositions(usedAuth bool, reqStorefront string, eps []Episode, forceWhenEmpty bool) bool {
	if !c.HasAuth() || len(eps) == 0 || hasAnyKnownPosition(eps) {
		return false
	}
	if !usedAuth || forceWhenEmpty {
		return true
	}
	acctSF := c.AccountStorefront()
	return acctSF != "" && acctSF != reqStorefront
}

func (c *Client) hydrateEpisodePositions(ctx context.Context, eps []Episode) {
	if !c.HasAuth() || len(eps) == 0 {
		return
	}
	ids := make([]string, 0, len(eps))
	for _, ep := range eps {
		if ep.ID != "" && !ep.PlaybackPosition.Known {
			ids = append(ids, ep.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	positions, err := c.PlaybackPositions(ctx, ids)
	if err != nil || len(positions) == 0 {
		return
	}
	for i := range eps {
		if !eps[i].PlaybackPosition.Known {
			if pos, ok := positions[eps[i].ID]; ok && pos.Known {
				eps[i].PlaybackPosition = pos
			}
		}
	}
}

// PlaybackPositions fetches personalized cloud playback positions for up to N episode IDs
// in batches of 50 using the signed-in account's storefront (falling back to browse storefront).
func (c *Client) PlaybackPositions(ctx context.Context, episodeIDs []string) (map[string]PlaybackPosition, error) {
	if !c.HasAuth() {
		return nil, ErrUnauthorized
	}
	seen := make(map[string]bool, len(episodeIDs))
	unique := make([]string, 0, len(episodeIDs))
	for _, id := range episodeIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil, nil
	}

	sf := c.AccountStorefront()
	browseSF := c.Storefront()
	if sf == "" {
		sf = browseSF
	}

	out := make(map[string]PlaybackPosition, len(unique))
	const batchSize = 50
	fetchBatch := func(storefront string, batch []string) (dataResponse, error) {
		path := fmt.Sprintf("/v1/catalog/%s/podcast-episodes?ids=%s&include=playback-position&fields%%5Bpodcast-episodes%%5D=name", url.PathEscape(storefront), url.QueryEscape(strings.Join(batch, ",")))
		var resp dataResponse
		err := c.doJSON(ctx, http.MethodGet, path, nil, true, &resp)
		return resp, err
	}
	for start := 0; start < len(unique); start += batchSize {
		end := min(len(unique), start+batchSize)
		batch := unique[start:end]
		resp, err := fetchBatch(sf, batch)
		if (err != nil || len(resp.Data) == 0) && browseSF != "" && browseSF != sf && ctx.Err() == nil {
			if altResp, altErr := fetchBatch(browseSF, batch); altErr == nil && len(altResp.Data) > 0 {
				resp, err = altResp, nil
			}
		}
		if err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, err
		}
		inc := indexIncluded(resp.Included)
		for _, r := range resp.Data {
			if pos := extractPlaybackPosition(r, inc); pos.Known {
				out[r.ID] = pos
			}
		}
	}
	return out, nil
}

// UserStorefront retrieves the signed-in Apple ID's home storefront code (e.g. "us", "cn").
func (c *Client) UserStorefront(ctx context.Context) (string, error) {
	var resp dataResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/me/storefront", nil, true, &resp); err != nil {
		return "", err
	}
	if len(resp.Data) == 0 {
		return "", nil
	}
	sf := strings.ToLower(strings.TrimSpace(resp.Data[0].ID))
	if sf != "" {
		c.SetAccountStorefront(sf)
	}
	return sf, nil
}

func appendQueryParam(rawPath, key, val string) string {
	u, err := url.Parse(rawPath)
	if err != nil {
		return rawPath
	}
	q := u.Query()
	existing := q.Get(key)
	if existing == "" {
		q.Set(key, val)
	} else if !strings.Contains(existing, val) {
		q.Set(key, existing+","+val)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Charts fetches the top shows and trending episodes for the active storefront.
func (c *Client) Charts(ctx context.Context, limit int) (*ChartsResult, error) {
	if limit <= 0 {
		limit = 20
	}
	sfRaw := c.Storefront()
	sf := url.PathEscape(sfRaw)
	anonPath := fmt.Sprintf("/v1/catalog/%s/charts?types=podcasts,podcast-episodes&limit=%d&include%%5Bpodcast-episodes%%5D=podcast&extend%%5Bpodcasts%%5D=editorialArtwork,feedUrl", sf, limit)
	authPath := fmt.Sprintf("/v1/catalog/%s/charts?types=podcasts,podcast-episodes&limit=%d&include%%5Bpodcast-episodes%%5D=podcast,playback-position&extend%%5Bpodcasts%%5D=editorialArtwork,feedUrl", sf, limit)
	var resp chartsResponse
	usedAuth, err := c.doCatalogJSON(ctx, authPath, anonPath, &resp)
	if err != nil {
		return nil, err
	}

	inc := indexIncluded(resp.Included)
	out := &ChartsResult{}
	showMap := make(map[string]Show)
	if len(resp.Results.Podcasts) > 0 {
		for _, r := range resp.Results.Podcasts[0].Data {
			s := toShow(r)
			out.TopShows = append(out.TopShows, s)
			showMap[s.ID] = s
		}
	}
	if len(resp.Results.PodcastEpisodes) > 0 {
		for _, r := range resp.Results.PodcastEpisodes[0].Data {
			var fallback *Show
			if r.Relationships != nil && r.Relationships.Podcast != nil && len(r.Relationships.Podcast.Data) > 0 {
				if s, ok := showMap[r.Relationships.Podcast.Data[0].ID]; ok {
					sc := s
					fallback = &sc
				}
			}
			out.TrendingEpisodes = append(out.TrendingEpisodes, toEpisode(r, fallback, inc))
		}
	}
	if c.shouldHydratePositions(usedAuth, sfRaw, out.TrendingEpisodes, true) {
		c.hydrateEpisodePositions(ctx, out.TrendingEpisodes)
	}
	return out, nil
}

// Search queries the Apple Podcasts catalog for shows and episodes.
func (c *Client) Search(ctx context.Context, term string, limit int) (*SearchResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return &SearchResult{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	sfRaw := c.Storefront()
	sf := url.PathEscape(sfRaw)
	anonPath := fmt.Sprintf("/v1/catalog/%s/search?term=%s&types=podcasts,podcast-episodes&limit=%d&include%%5Bpodcast-episodes%%5D=podcast&extend%%5Bpodcasts%%5D=editorialArtwork,feedUrl", sf, url.QueryEscape(term), limit)
	authPath := fmt.Sprintf("/v1/catalog/%s/search?term=%s&types=podcasts,podcast-episodes&limit=%d&include%%5Bpodcast-episodes%%5D=podcast,playback-position&extend%%5Bpodcasts%%5D=editorialArtwork,feedUrl", sf, url.QueryEscape(term), limit)
	var resp searchResponse
	usedAuth, err := c.doCatalogJSON(ctx, authPath, anonPath, &resp)
	if err != nil {
		return nil, err
	}

	inc := indexIncluded(resp.Included)
	out := &SearchResult{}
	for _, r := range resp.Results.Podcasts.Data {
		out.Shows = append(out.Shows, toShow(r))
	}
	for _, r := range resp.Results.PodcastEpisodes.Data {
		out.Episodes = append(out.Episodes, toEpisode(r, nil, inc))
	}
	if c.shouldHydratePositions(usedAuth, sfRaw, out.Episodes, true) {
		c.hydrateEpisodePositions(ctx, out.Episodes)
	}
	return out, nil
}

// GetShow fetches full metadata and the first page of episodes for showID.
func (c *Client) GetShow(ctx context.Context, showID string) (*ShowDetailResult, error) {
	sfRaw := c.Storefront()
	sf := url.PathEscape(sfRaw)
	anonPath := fmt.Sprintf("/v1/catalog/%s/podcasts/%s?include=episodes&limit%%5Bepisodes%%5D=50&extend=editorialArtwork,feedUrl", sf, url.PathEscape(showID))
	authPath := fmt.Sprintf("/v1/catalog/%s/podcasts/%s?include=episodes&limit%%5Bepisodes%%5D=50&include%%5Bpodcast-episodes%%5D=playback-position&extend=editorialArtwork,feedUrl", sf, url.PathEscape(showID))
	var resp dataResponse
	usedAuth, err := c.doCatalogJSON(ctx, authPath, anonPath, &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("show %s not found", showID)
	}

	inc := indexIncluded(resp.Included)
	showRes := resp.Data[0]
	show := toShow(showRes)
	out := &ShowDetailResult{Show: show}
	if showRes.Relationships != nil && showRes.Relationships.Episodes != nil {
		out.NextEpisodesPath = showRes.Relationships.Episodes.Next
		for _, epRes := range showRes.Relationships.Episodes.Data {
			out.Episodes = append(out.Episodes, toEpisode(epRes, &show, inc))
		}
	}
	if c.shouldHydratePositions(usedAuth, sfRaw, out.Episodes, true) {
		c.hydrateEpisodePositions(ctx, out.Episodes)
	}
	return out, nil
}

// GetEpisodeDetail fetches rich episode metadata (fullDescription, show editorialArtwork, feedUrl).
func (c *Client) GetEpisodeDetail(ctx context.Context, episodeID string) (*Episode, error) {
	sfRaw := c.Storefront()
	sf := url.PathEscape(sfRaw)
	anonPath := fmt.Sprintf("/v1/catalog/%s/podcast-episodes/%s?include=podcast&extend=fullDescription&extend%%5Bpodcasts%%5D=feedUrl,editorialArtwork", sf, url.PathEscape(episodeID))
	authPath := fmt.Sprintf("/v1/catalog/%s/podcast-episodes/%s?include=podcast,playback-position&extend=fullDescription&extend%%5Bpodcasts%%5D=feedUrl,editorialArtwork", sf, url.PathEscape(episodeID))
	var resp dataResponse
	usedAuth, err := c.doCatalogJSON(ctx, authPath, anonPath, &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("episode %s not found", episodeID)
	}
	inc := indexIncluded(resp.Included)
	ep := toEpisode(resp.Data[0], nil, inc)
	if c.HasAuth() && !ep.PlaybackPosition.Known && (!usedAuth || (c.AccountStorefront() != "" && c.AccountStorefront() != sfRaw)) {
		if posMap, perr := c.PlaybackPositions(ctx, []string{ep.ID}); perr == nil {
			if pos, ok := posMap[ep.ID]; ok && pos.Known {
				ep.PlaybackPosition = pos
			}
		}
	}
	return &ep, nil
}

// GetEpisodesPage fetches a subsequent episodes page from a next URL returned by GetShow.
func (c *Client) GetEpisodesPage(ctx context.Context, nextPath string, show *Show) ([]Episode, string, error) {
	if strings.TrimSpace(nextPath) == "" {
		return nil, "", nil
	}
	if !strings.Contains(nextPath, "limit=") {
		sep := "?"
		if strings.Contains(nextPath, "?") {
			sep = "&"
		}
		nextPath += sep + "limit=50"
	}
	authPath := appendQueryParam(nextPath, "include[podcast-episodes]", "playback-position")
	var resp dataResponse
	usedAuth, err := c.doCatalogJSON(ctx, authPath, nextPath, &resp)
	if err != nil {
		return nil, "", err
	}
	inc := indexIncluded(resp.Included)
	eps := make([]Episode, 0, len(resp.Data))
	for _, r := range resp.Data {
		eps = append(eps, toEpisode(r, show, inc))
	}
	if c.shouldHydratePositions(usedAuth, c.Storefront(), eps, true) {
		c.hydrateEpisodePositions(ctx, eps)
	}
	return eps, resp.Next, nil
}

// LibraryShows fetches the authenticated user's subscribed podcasts.
func (c *Client) LibraryShows(ctx context.Context) ([]Show, error) {
	var shows []Show
	err := c.readLibraryPages(ctx, "/v1/me/library/podcasts?limit=30", 15, func(resp dataResponse) {
		for _, r := range resp.Data {
			s := toShow(r)
			s.Subscribed = true
			shows = append(shows, s)
		}
	})
	return shows, err
}

// ContinuePlaying fetches the authenticated user's Up Next / Continue Playing episodes.
func (c *Client) ContinuePlaying(ctx context.Context) ([]Episode, error) {
	return c.libraryEpisodes(ctx, "/v1/me/continue-playing/podcast-episodes?include%5Bpodcast-episodes%5D=podcast,playback-position&fields%5Bpodcasts%5D=artwork,name", 10)
}

// NewEpisodes fetches recent episodes from the user's subscribed shows with synced playback positions.
func (c *Client) NewEpisodes(ctx context.Context) ([]Episode, error) {
	return c.libraryEpisodes(ctx, "/v1/me/library/new-episodes?limit=30&include%5Bpodcast-episodes%5D=podcast,playback-position&fields%5Bpodcasts%5D=artwork,name,editorialArtwork", 2)
}

// SavedEpisodes fetches episodes saved to the user's library with synced playback positions.
func (c *Client) SavedEpisodes(ctx context.Context) ([]Episode, error) {
	return c.libraryEpisodes(ctx, "/v1/me/library/podcast-episodes?limit=30&include%5Bpodcast-episodes%5D=podcast,playback-position&fields%5Bpodcasts%5D=artwork,name", 15)
}

// RecentPlayed fetches the authenticated user's recently played / completed episodes with synced playback positions.
func (c *Client) RecentPlayed(ctx context.Context) ([]Episode, error) {
	return c.libraryEpisodes(ctx, "/v1/me/recent/played/podcast-episodes?include%5Bpodcast-episodes%5D=podcast,playback-position&fields%5Bpodcasts%5D=artwork,name", 3)
}

func (c *Client) libraryEpisodes(ctx context.Context, path string, maxPages int) ([]Episode, error) {
	var episodes []Episode
	err := c.readLibraryPages(ctx, path, maxPages, func(resp dataResponse) {
		included := indexIncluded(resp.Included)
		for _, r := range resp.Data {
			episodes = append(episodes, toEpisode(r, nil, included))
		}
	})
	return episodes, err
}

func (c *Client) readLibraryPages(ctx context.Context, path string, maxPages int, visit func(dataResponse)) error {
	if maxPages <= 0 {
		maxPages = 10
	}
	seen := map[string]bool{}
	origin, _ := url.Parse(baseAPIURL)
	previous := origin

	var baseQuery url.Values
	if firstURL, err := origin.Parse(path); err == nil {
		baseQuery = firstURL.Query()
	}

	for page := 0; path != "" && page < maxPages; page++ {
		next, err := previous.Parse(path)
		if err != nil || next.Scheme != origin.Scheme || next.Host != origin.Host ||
			next.User != nil || !strings.HasPrefix(next.Path, "/v1/me/") {
			return errors.New("apple podcasts: invalid library page URL")
		}
		if page > 0 && len(baseQuery) > 0 {
			q := next.Query()
			changed := false
			for k, vals := range baseQuery {
				if k == "offset" || k == "cursor" || k == "page" {
					continue
				}
				if !q.Has(k) && len(vals) > 0 {
					q[k] = vals
					changed = true
				}
			}
			if changed {
				next.RawQuery = q.Encode()
			}
		}
		previous = next
		path = next.RequestURI()
		if seen[path] {
			return errors.New("apple podcasts: repeated library page")
		}
		seen[path] = true
		var resp dataResponse
		if err := c.doJSON(ctx, http.MethodGet, path, nil, true, &resp); err != nil {
			if page == 0 && errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}
		visit(resp)
		path = resp.Next
	}
	return nil
}

// SubscribeShow subscribes to or unsubscribes from a show in the user's cloud library.
func (c *Client) SubscribeShow(ctx context.Context, showID string, subscribe bool) error {
	method := http.MethodDelete
	if subscribe {
		method = http.MethodPost
	}
	path := fmt.Sprintf("/v1/me/library/podcasts/%s", url.PathEscape(showID))
	return c.doJSON(ctx, method, path, nil, true, nil)
}

// SaveEpisode saves or removes an episode from the user's cloud library.
func (c *Client) SaveEpisode(ctx context.Context, episodeID string, save bool) error {
	method := http.MethodDelete
	if save {
		method = http.MethodPost
	}
	path := fmt.Sprintf("/v1/me/library/podcast-episodes/%s", url.PathEscape(episodeID))
	return c.doJSON(ctx, method, path, nil, true, nil)
}

// UpdatePlaybackPosition syncs the episode playback position to iCloud.
func (c *Client) UpdatePlaybackPosition(ctx context.Context, episodeID string, posSec float64, completed bool, recordedAt time.Time) error {
	body := map[string]any{
		"type": "playback-positions",
		"attributes": map[string]any{
			"positionInMilliseconds": math.Round(max(0, posSec) * 1000),
			"recordedAtTimestamp":    recordedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
			"completed":              completed,
		},
	}
	path := fmt.Sprintf("/v1/me/playback/positions/podcast-episodes/%s", url.PathEscape(episodeID))
	return c.doJSON(ctx, http.MethodPut, path, body, true, nil)
}

// AccountInfo retrieves the signed-in user's full display name from amp-account.
func (c *Client) AccountInfo(ctx context.Context) (string, error) {
	var resp struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	}
	if err := c.doJSON(ctx, http.MethodGet, accountURL, nil, true, &resp); err != nil {
		return "", err
	}
	name := strings.TrimSpace(resp.FirstName + " " + resp.LastName)
	return name, nil
}

func indexIncluded(list []apiResource) map[string]apiResource {
	if len(list) == 0 {
		return nil
	}
	m := make(map[string]apiResource, len(list))
	for _, r := range list {
		m[r.Type+":"+r.ID] = r
	}
	return m
}

type rssTranscriptTag struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string             `xml:"title"`
	GUID        string             `xml:"guid"`
	Transcripts []rssTranscriptTag `xml:"transcript"`
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

// FetchEpisodeTranscript loads a Podcasting 2.0 <podcast:transcript> from ep.FeedURL
// when available. It returns nil when the episode has no actual transcript (without
// fabricating one from episode description / show notes).
func (c *Client) FetchEpisodeTranscript(ctx context.Context, ep Episode) (*EpisodeTranscript, error) {
	if ep.FeedURL != "" {
		if tr := c.fetchRSSPodcastTranscript(ctx, ep); tr != nil && len(tr.Lines) > 0 {
			return tr, nil
		}
	}
	return nil, nil
}

func pickBestTranscriptTag(tags []rssTranscriptTag, baseFeedURL string) (resolvedURL, mimeType string) {
	scoreTag := func(t rssTranscriptTag) int {
		lower := strings.ToLower(t.Type + " " + t.URL)
		switch {
		case strings.Contains(lower, "json"):
			return 40
		case strings.Contains(lower, "vtt"):
			return 30
		case strings.Contains(lower, "srt") || strings.Contains(lower, "subrip"):
			return 20
		case strings.Contains(lower, "plain"):
			return 10
		case strings.Contains(lower, "html"):
			return 2
		default:
			return 5
		}
	}
	var best rssTranscriptTag
	bestScore := -1
	for _, tag := range tags {
		if strings.TrimSpace(tag.URL) == "" {
			continue
		}
		if sc := scoreTag(tag); sc > bestScore {
			bestScore = sc
			best = tag
		}
	}
	if best.URL == "" {
		return "", ""
	}
	rawURL := strings.TrimSpace(best.URL)
	if baseURL, err := url.Parse(baseFeedURL); err == nil {
		if refURL, rerr := url.Parse(rawURL); rerr == nil {
			rawURL = baseURL.ResolveReference(refURL).String()
		}
	}
	return rawURL, best.Type
}

func (c *Client) fetchRSSPodcastTranscript(ctx context.Context, ep Episode) *EpisodeTranscript {
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fctx, http.MethodGet, ep.FeedURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK {
		return nil
	}
	// Quick bailout if feed contains no transcript tags at all.
	if !bytes.Contains(raw, []byte("transcript")) {
		return nil
	}

	var feed rssFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil
	}

	var matchedURL, matchedType string
	epNameLower := strings.ToLower(strings.TrimSpace(ep.Name))
	for _, item := range feed.Channel.Items {
		if len(item.Transcripts) == 0 {
			continue
		}
		itemTitle := strings.TrimSpace(item.Title)
		itemLower := strings.ToLower(itemTitle)
		titleMatches := strings.EqualFold(itemTitle, ep.Name) ||
			(epNameLower != "" && itemLower != "" &&
				(strings.Contains(itemLower, epNameLower) || (len(itemLower) >= 8 && strings.Contains(epNameLower, itemLower))))
		if titleMatches {
			matchedURL, matchedType = pickBestTranscriptTag(item.Transcripts, ep.FeedURL)
			break
		}
	}
	if matchedURL == "" {
		return nil
	}

	treq, err := http.NewRequestWithContext(fctx, http.MethodGet, matchedURL, nil)
	if err != nil {
		return nil
	}
	treq.Header.Set("User-Agent", userAgent)
	tres, err := c.httpClient.Do(treq)
	if err != nil {
		return nil
	}
	tbody, err := io.ReadAll(io.LimitReader(tres.Body, 4<<20))
	tres.Body.Close()
	if err != nil || tres.StatusCode != http.StatusOK {
		return nil
	}

	return parseTranscriptAsset(string(tbody), matchedType, matchedURL)
}

var cueTimeRe = regexp.MustCompile(`(\d{1,2}:)?\d{2}:\d{2}[.,]\d{3}\s*-->\s*(\d{1,2}:)?\d{2}:\d{2}[.,]\d{3}`)

func parseCueTimestamp(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	dot := strings.IndexByte(s, '.')
	ms := 0.0
	if dot >= 0 {
		if v, err := strconv.ParseFloat(s[dot:], 64); err == nil {
			ms = v
		}
		s = s[:dot]
	}
	sec, ok := parseTimestampSec(s)
	if !ok {
		return 0, false
	}
	return sec + ms, true
}

func parseTranscriptAsset(body, contentType, rawURL string) *EpisodeTranscript {
	lower := strings.ToLower(contentType + " " + rawURL)
	if strings.Contains(lower, "json") || strings.HasPrefix(strings.TrimSpace(body), "{") {
		var payload struct {
			Segments []struct {
				StartTime float64 `json:"startTime"`
				EndTime   float64 `json:"endTime"`
				Speaker   string  `json:"speaker"`
				Body      string  `json:"body"`
			} `json:"segments"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err == nil && len(payload.Segments) > 0 {
			lines := make([]TranscriptLine, 0, len(payload.Segments))
			for _, seg := range payload.Segments {
				txt := strings.TrimSpace(seg.Body)
				if txt == "" {
					continue
				}
				lines = append(lines, TranscriptLine{
					StartSec:     seg.StartTime,
					EndSec:       seg.EndTime,
					HasTimestamp: true,
					Speaker:      strings.TrimSpace(seg.Speaker),
					Text:         txt,
				})
			}
			return &EpisodeTranscript{Source: "podcast:transcript", Lines: lines, HasSyncedTime: true}
		}
	}

	if strings.Contains(lower, "vtt") || strings.Contains(lower, "srt") || cueTimeRe.MatchString(body) {
		rawLines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
		var cues []TranscriptLine
		for i := 0; i < len(rawLines); i++ {
			ln := strings.TrimSpace(rawLines[i])
			if !cueTimeRe.MatchString(ln) {
				continue
			}
			parts := strings.Split(ln, "-->")
			if len(parts) < 2 {
				continue
			}
			startSec, ok1 := parseCueTimestamp(parts[0])
			endFields := strings.Fields(strings.TrimSpace(parts[1]))
			var endSec float64
			if len(endFields) > 0 {
				endSec, _ = parseCueTimestamp(endFields[0])
			}
			var buf []string
			for i+1 < len(rawLines) {
				next := strings.TrimSpace(rawLines[i+1])
				if next == "" {
					break
				}
				i++
				buf = append(buf, next)
			}
			txt := html.UnescapeString(htmlTagRe.ReplaceAllString(strings.Join(buf, " "), ""))
			txt = strings.TrimSpace(txt)
			if txt != "" && ok1 {
				cues = append(cues, TranscriptLine{
					StartSec:     startSec,
					EndSec:       endSec,
					HasTimestamp: true,
					Text:         txt,
				})
			}
		}
		if len(cues) > 0 {
			return &EpisodeTranscript{Source: "podcast:transcript", Lines: cues, HasSyncedTime: true}
		}
	}

	tr := ParseTranscriptFromText("", body, 0)
	tr.Source = "podcast:transcript"
	return tr
}
