package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"apple-podcasts/internal/apple"
)

const (
	secretMUTKey    = "media-user-token"
	secretCookieKey = "cookie-header"
)

// EpisodeProgress tracks local playback position and completion state.
type EpisodeProgress struct {
	PositionSec float64 `json:"positionSec"`
	DurationSec float64 `json:"durationSec"`
	Completed   bool    `json:"completed"`
	UpdatedAt   int64   `json:"updatedAt"`
}

// SavedPlayback remembers the active episode, playback queue, timestamp, and playback state across restarts.
type SavedPlayback struct {
	Episode     apple.Episode   `json:"episode"`
	Queue       []apple.Episode `json:"queue,omitempty"`
	QueueIndex  int             `json:"queueIndex"`
	PositionSec float64         `json:"positionSec"`
	DurationSec float64         `json:"durationSec"`
	WasPlaying  bool            `json:"wasPlaying"`
	UpdatedAt   int64           `json:"updatedAt"`
}

// SavedRoute remembers the page / immersive state shown when closing the client.
type SavedRoute struct {
	Page       pageKind `json:"page"`
	ShowID     string   `json:"showId,omitempty"`
	EpID       string   `json:"epId,omitempty"`
	Transcript bool     `json:"transcript,omitempty"`
}

// AppState stores user preferences, subscriptions, saved episodes, playback session, and history.
type AppState struct {
	Storefront     string                     `json:"storefront"`
	Volume         float64                    `json:"volume"`
	Speed          float64                    `json:"speed"`
	Subscriptions  []apple.Show               `json:"subscriptions"`
	SavedEpisodes  []apple.Episode            `json:"savedEpisodes"`
	RecentEpisodes []apple.Episode            `json:"recentEpisodes"`
	Progress       map[string]EpisodeProgress `json:"progress"`
	Playback       *SavedPlayback             `json:"playback,omitempty"`
	LastRoute      *SavedRoute                `json:"lastRoute,omitempty"`
	CloudLibrary   *CloudLibrary              `json:"cloudLibrary,omitempty"`
	AccountName    string                     `json:"accountName,omitempty"` // Migrated from older builds.
}

type CloudLibrary struct {
	SessionKey     string                            `json:"sessionKey"`
	AccountName    string                            `json:"accountName,omitempty"`
	Storefront     string                            `json:"storefront,omitempty"`
	Subscriptions  []apple.Show                      `json:"subscriptions"`
	SavedEpisodes  []apple.Episode                   `json:"savedEpisodes"`
	RecentPlayed   []apple.Episode                   `json:"recentPlayed,omitempty"`
	Progress       map[string]EpisodeProgress        `json:"progress,omitempty"`
	CloudPositions map[string]apple.PlaybackPosition `json:"cloudPositions,omitempty"`
}

// AuthCredentials holds user tokens obtained from WebView2 sign-in.
type AuthCredentials struct {
	MediaUserToken string `json:"mediaUserToken"`
	CookieHeader   string `json:"cookieHeader"`
}

func defaultAppState() AppState {
	return AppState{
		Storefront: "us",
		Volume:     0.8,
		Speed:      1.0,
		Progress:   map[string]EpisodeProgress{},
	}
}

func resolveDataDir(defaultDir string) string {
	exePath, err := os.Executable()
	if err != nil {
		return defaultDir
	}
	return resolveDataDirForExe(filepath.Dir(exePath), defaultDir)
}

func resolveDataDirForExe(exeDir, defaultDir string) string {
	for _, marker := range []string{"portable.txt", "portable", ".portable"} {
		if _, err := os.Stat(filepath.Join(exeDir, marker)); err == nil {
			dir := filepath.Join(exeDir, "data")
			if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
				return dir
			}
		}
	}
	return defaultDir
}

func loadAppState(dir string) AppState {
	st := defaultAppState()
	if dir == "" {
		return st
	}
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return st
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return defaultAppState()
	}
	if st.Storefront == "" {
		st.Storefront = "us"
	}
	if st.Volume <= 0 || st.Volume > 1 {
		st.Volume = 0.8
	}
	if st.Speed < 0.5 || st.Speed > 4.0 {
		st.Speed = 1.0
	}
	if st.Progress == nil {
		st.Progress = map[string]EpisodeProgress{}
	}
	return st
}

func saveAppState(dir string, st AppState) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	atomicWriteFile(filepath.Join(dir, "state.json"), raw, 0o600)
}

func loadCredentials(dir string) AuthCredentials {
	raw, err := readStoredCredentials(dir)
	if err == nil {
		var creds AuthCredentials
		if err := json.Unmarshal(raw, &creds); err == nil && creds.MediaUserToken != "" {
			if err := clearLegacyCredentials(dir); err != nil {
				log.Printf("apple-podcasts: remove legacy credentials: %v", err)
			}
			return creds
		}
		log.Print("apple-podcasts: stored credentials are invalid")
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("apple-podcasts: read stored credentials: %v", err)
	}

	creds := loadLegacyCredentials(dir)
	if creds.MediaUserToken != "" {
		if err := saveCredentials(dir, creds); err != nil {
			log.Printf("apple-podcasts: migrate credentials: %v", err)
		}
	}
	return creds
}

func loadLegacyCredentials(dir string) AuthCredentials {
	if dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, "auth.json")); err == nil {
			var creds AuthCredentials
			if json.Unmarshal(raw, &creds) == nil && creds.MediaUserToken != "" {
				return creds
			}
		}
	}
	mut, _ := mygo.Secrets.Get(secretMUTKey)
	cookie, _ := mygo.Secrets.Get(secretCookieKey)
	return AuthCredentials{MediaUserToken: string(mut), CookieHeader: string(cookie)}
}

func saveCredentials(dir string, creds AuthCredentials) error {
	raw, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	if err := writeStoredCredentials(dir, raw); err != nil {
		return err
	}
	if err := clearLegacyCredentials(dir); err != nil {
		return fmt.Errorf("remove legacy credentials: %w", err)
	}
	return nil
}

func clearCredentials(dir string) error {
	return errors.Join(removeStoredCredentials(dir), clearLegacyCredentials(dir))
}

func clearLegacyCredentials(dir string) error {
	err := errors.Join(mygo.Secrets.Delete(secretMUTKey), mygo.Secrets.Delete(secretCookieKey))
	if dir != "" {
		err = errors.Join(err, removeIfPresent(filepath.Join(dir, "auth.json")))
	}
	return err
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
