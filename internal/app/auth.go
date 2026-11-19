package app

import (
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

const (
	legacyLoginSessionName = "apple-podcasts-login"
	loginURL               = "https://podcasts.apple.com"
	cookiePollEvery        = 500 * time.Millisecond
	loginTimeout           = 5 * time.Minute
)

// startWebSignIn opens a temporary WebView2 window for Apple ID login,
// extracts media-user-token and session cookies once authenticated, then closes
// the window so WebView2 processes exit.
func (a *App) startWebSignIn() {
	if a.signingIn && a.signInWin != nil {
		a.signInWin.Focus()
		return
	}
	if !mygo.WebViewAvailable() {
		a.pendingToast = "当前系统未检测到 WebView2 Runtime，无法打开 Apple ID 登录窗口"
		return
	}

	session := mygo.NewEphemeralSession()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "登录 Apple Podcasts",
		URL:       loginURL,
		Width:     520,
		Height:    700,
		MinWidth:  400,
		MinHeight: 520,
		Parent:    a.win,
		Page:      mygo.PageOptions{Session: session},
	})
	_ = win.SetIcon(appIconPNG)

	a.signingIn = true
	a.signInWin = win

	closed := make(chan struct{})
	win.OnClosed(func() {
		select {
		case <-closed:
		default:
			close(closed)
		}
		a.update(func() {
			if a.signInWin == win {
				a.signingIn = false
				a.signInWin = nil
			}
		})
	})

	go func() {
		tick := time.NewTicker(cookiePollEvery)
		defer tick.Stop()
		timer := time.NewTimer(loginTimeout)
		defer timer.Stop()

		for {
			select {
			case <-closed:
				return
			case <-timer.C:
				a.update(func() {
					if a.signInWin == win {
						a.pendingToast = "登录超时，请重试"
						win.Close()
					}
				})
				return
			case <-tick.C:
				cookies, err := session.Cookies.List(mygo.CookieFilter{URL: loginURL})
				if err != nil {
					log.Printf("apple-podcasts auth poll: %v", err)
					continue
				}
				mut, header := extractAppleCookies(cookies)
				if mut == "" {
					continue
				}

				a.update(func() {
					if a.signInWin != win {
						return
					}
					creds := AuthCredentials{MediaUserToken: mut, CookieHeader: header}
					a.client.SetAuth(mut, header)
					a.selectLibrary(mut, false)
					a.signingIn = false
					a.signInWin = nil
					a.pendingToast = "Apple ID 登录成功，正在同步云端资料..."
					if err := saveCredentials(a.dataDir, creds); err != nil {
						log.Printf("apple-podcasts: save credentials: %v", err)
						a.pendingToast = "Apple ID 已登录，登录凭据保存失败"
					}
					win.Close()
					a.loadCharts()
					a.refreshCurrentPageForAuthChange()
					a.syncCloudLibrary()
				})
				return
			}
		}
	}()
}

func extractAppleCookies(cookies []mygo.Cookie) (mut, header string) {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Value == "" {
			continue
		}
		if c.Name == "media-user-token" {
			mut = c.Value
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return mut, strings.Join(parts, "; ")
}

func (a *App) signOut() {
	a.client.ClearAuth()
	a.selectLibrary("", false)
	a.pendingToast = "已退出 Apple ID"
	if err := clearCredentials(a.dataDir); err != nil {
		log.Printf("apple-podcasts: clear credentials: %v", err)
		a.pendingToast = "已退出 Apple ID，清除本机登录凭据失败"
	}
	a.loadCharts()
	a.refreshCurrentPageForAuthChange()

	go func() {
		if err := mygo.NewSession(legacyLoginSessionName).ClearData(mygo.SiteDataAll); err != nil {
			log.Printf("apple-podcasts: clear legacy sign-in WebView: %v", err)
		}
	}()
}
