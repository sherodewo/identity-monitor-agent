package browser

import (
	"context"
	"fmt"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/evidence"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/securefs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Page struct{ URL, HTML, Text, Screenshot string }
type SessionAPI interface {
	Observe() (Page, error)
	Navigate(string) error
	Scroll(int) error
	Screenshot() (string, error)
	Back() error
	Limit(context.Context) func() bool
}
type Manager struct {
	Config      config.Browser
	EvidenceDir string
	Metrics     *observability.Metrics
	mu          sync.Mutex
	locks       map[string]bool
}
type Session struct {
	ctx     context.Context
	cancel  context.CancelFunc
	manager *Manager
	policy  guardrail.Policy
	blocked atomic.Bool
}

func New(c config.Browser, dir string, m *observability.Metrics) *Manager {
	return &Manager{Config: c, EvidenceDir: dir, Metrics: m, locks: map[string]bool{}}
}
func (m *Manager) Open(ctx context.Context, source string, policy guardrail.Policy, bootstrap bool) (*Session, error) {
	if source != "generic_web_search" && source != "youtube_public_search" && source != "threads_public_search" {
		return nil, fmt.Errorf("unknown browser profile")
	}
	m.mu.Lock()
	if m.locks[source] {
		m.mu.Unlock()
		return nil, fmt.Errorf("source browser already active")
	}
	m.locks[source] = true
	m.mu.Unlock()
	release := func() { m.mu.Lock(); delete(m.locks, source); m.mu.Unlock() }
	profile, e := filepath.Abs(filepath.Join(m.Config.ProfileDirectory, source))
	if e != nil {
		release()
		return nil, e
	}
	if e = os.MkdirAll(profile, 0700); e != nil {
		release()
		return nil, e
	}
	if e = securefs.Restrict(profile, true); e != nil {
		release()
		return nil, e
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.UserDataDir(profile), chromedp.Flag("headless", m.Config.Headless && !bootstrap), chromedp.Flag("disable-sync", true), chromedp.Flag("disable-extensions", true), chromedp.WindowSize(1400, 1000))
	if m.Config.ExecutablePath != "" {
		opts = append(opts, chromedp.ExecPath(m.Config.ExecutablePath))
	}
	alloc, ac := chromedp.NewExecAllocator(ctx, opts...)
	tab, tc := chromedp.NewContext(alloc)
	var closeOnce sync.Once
	s := &Session{ctx: tab, manager: m, policy: policy, cancel: func() { closeOnce.Do(func() { tc(); ac(); release() }) }}
	if e = chromedp.Run(tab, cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDeny)); e != nil {
		s.Close()
		return nil, fmt.Errorf("start Chrome (check executable_path): %w", e)
	}
	if !bootstrap {
		chromedp.ListenTarget(tab, func(ev any) {
			if event, ok := ev.(*fetch.EventRequestPaused); ok {
				deny := event.Request.Method != "GET" && event.Request.Method != "HEAD"
				if event.ResourceType == network.ResourceTypeDocument {
					deny = deny || policy.URL(event.Request.URL) != nil
					if deny {
						s.blocked.Store(true)
					}
				}
				go func() {
					_ = chromedp.Run(tab, chromedp.ActionFunc(func(c context.Context) error {
						if deny {
							return fetch.FailRequest(event.RequestID, network.ErrorReasonBlockedByClient).Do(c)
						}
						return fetch.ContinueRequest(event.RequestID).Do(c)
					}))
				}()
			}
		})
		if e = chromedp.Run(tab, fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}})); e != nil {
			s.Close()
			return nil, e
		}
	}
	return s, nil
}
func (s *Session) Close()                                { s.cancel() }
func (s *Session) Limit(ctx context.Context) func() bool { return context.AfterFunc(ctx, s.cancel) }
func (s *Session) run(actions ...chromedp.Action) error {
	s.manager.Metrics.Inc("browser_actions_total")
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(s.manager.Config.PageTimeoutSeconds)*time.Second)
	defer cancel()
	e := chromedp.Run(ctx, actions...)
	if e != nil {
		s.manager.Metrics.Inc("browser_action_errors_total")
	}
	return e
}
func (s *Session) Navigate(url string) error {
	if e := s.policy.URL(url); e != nil {
		return e
	}
	s.blocked.Store(false)
	e := s.run(chromedp.Navigate(url))
	if s.blocked.Load() {
		return guardrail.ErrHuman
	}
	return e
}
func (s *Session) BootstrapNavigate(url string) error { return s.run(chromedp.Navigate(url)) }
func (s *Session) Observe() (Page, error) {
	var p Page
	e := s.run(chromedp.Location(&p.URL), chromedp.OuterHTML("html", &p.HTML), chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &p.Text))
	if e != nil {
		p.Screenshot, _ = s.Screenshot()
		return p, e
	}
	if s.blocked.Load() {
		return p, guardrail.ErrHuman
	}
	if e = guardrail.Detect(p.URL, p.HTML, p.Text); e != nil {
		p.Screenshot, _ = s.Screenshot()
		return p, e
	}
	if e = s.policy.URL(p.URL); e != nil {
		return p, e
	}
	return p, nil
}
func (s *Session) Screenshot() (string, error) {
	var b []byte
	if e := s.run(chromedp.FullScreenshot(&b, 80)); e != nil {
		return "", e
	}
	return evidence.Save(s.manager.EvidenceDir, b)
}
func (s *Session) Scroll(pixels int) error {
	return s.run(chromedp.Evaluate(fmt.Sprintf(`(() => {
 const candidates = [...document.querySelectorAll('main, [role="main"], div')].filter(el => {
  const style = getComputedStyle(el), rect = el.getBoundingClientRect();
  return /auto|scroll/.test(style.overflowY) && el.scrollHeight > el.clientHeight + 100 && rect.width > 300 && rect.height > 250 && rect.bottom > 0 && rect.top < innerHeight;
 });
 candidates.sort((a,b)=>b.clientHeight*b.clientWidth-a.clientHeight*a.clientWidth);
 const target = candidates[0] || document.scrollingElement;
 if (target) target.scrollBy(0, %d);
})()`, pixels), nil))
}
func (s *Session) Back() error {
	return fmt.Errorf("history navigation disabled; use an explicit allowlisted URL")
}
