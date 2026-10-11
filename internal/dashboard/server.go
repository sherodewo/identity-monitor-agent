package dashboard

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"identity-monitor-agent/internal/collector"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/ollama"
	"identity-monitor-agent/internal/report"
	"identity-monitor-agent/internal/storage"
	"identity-monitor-agent/web"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	App        *fiber.App
	Config     config.Config
	Store      *storage.SQLite
	Supervisor *collector.Supervisor
	Ollama     ollama.ChatClient
	Reports    *report.Service
	Metrics    *observability.Metrics
}

func New(c config.Config, db *storage.SQLite, s *collector.Supervisor, o ollama.ChatClient, r *report.Service, m *observability.Metrics) *Server {
	v := &Server{Config: c, Store: db, Supervisor: s, Ollama: o, Reports: r, Metrics: m}
	v.App = fiber.New(fiber.Config{AppName: "identity-monitor-agent", DisableStartupMessage: true, BodyLimit: 64 * 1024, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, ErrorHandler: func(c *fiber.Ctx, e error) error {
		code := 500
		message := "internal error"
		var fe *fiber.Error
		if errors.As(e, &fe) {
			code = fe.Code
			message = fe.Message
		}
		if errors.Is(e, sql.ErrNoRows) {
			code = 404
			message = "not found"
		}
		return c.Status(code).JSON(fiber.Map{"error": fiber.Map{"code": code, "message": message}})
	}})
	v.routes()
	return v
}
func (s *Server) routes() {
	app := s.App
	app.Use(recover.New())
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("Cache-Control", "no-store")
		host := c.Hostname()
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fiber.NewError(403, "invalid Host")
		}
		if origin := c.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != string(c.Request().Host()) || u.Scheme != "http" {
				return fiber.NewError(403, "cross-origin access denied")
			}
		}
		if strings.HasPrefix(c.Path(), "/api/") || c.Path() == "/metrics" || strings.HasPrefix(c.Path(), "/evidence/") {
			if s.Config.App.APIToken != "" {
				provided := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
				if subtle.ConstantTimeCompare([]byte(provided), []byte(s.Config.App.APIToken)) != 1 {
					return fiber.NewError(401, "local API token required")
				}
			}
		}
		if c.Method() != "GET" && c.Method() != "HEAD" && c.Get("X-Local-Request") != "1" {
			return fiber.NewError(403, "X-Local-Request: 1 required")
		}
		return c.Next()
	})
	app.Use(limiter.New(limiter.Config{Max: 240, Expiration: time.Minute}))
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/readyz", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
		defer cancel()
		if e := s.Store.DB.PingContext(ctx); e != nil {
			return fiber.NewError(503, "database unavailable")
		}
		if e := s.Ollama.Health(ctx); e != nil {
			return fiber.NewError(503, "Ollama or configured model unavailable")
		}
		return c.JSON(fiber.Map{"status": "ready", "demo": s.Config.Demo})
	})
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.HandlerFor(s.Metrics.Registry, promhttp.HandlerOpts{})))
	app.Get("/", func(c *fiber.Ctx) error {
		b, e := web.Files.ReadFile("templates/social.html")
		if e != nil {
			return e
		}
		c.Type("html")
		return c.Send(b)
	})
	app.Get("/static/*", func(c *fiber.Ctx) error {
		name := "static/" + c.Params("*")
		if !fs.ValidPath(name) {
			return fiber.ErrNotFound
		}
		b, e := web.Files.ReadFile(name)
		if e != nil {
			return fiber.ErrNotFound
		}
		c.Type(filepath.Ext(name))
		return c.Send(b)
	})
	app.Get("/evidence/:name", s.evidence)
	api := app.Group("/api/v1")
	api.Get("/dashboard/summary", s.summary)
	api.Get("/subjects", s.subjects)
	api.Post("/subjects", s.createSubject)
	api.Get("/mentions", s.mentions)
	api.Get("/mentions/:id", s.mention)
	api.Post("/mentions/:id/cases", s.createCase)
	api.Get("/review-queue", func(c *fiber.Ctx) error { c.Locals("review", true); return s.mentions(c) })
	api.Get("/cases", s.cases)
	api.Get("/cases/:id", s.caseDetail)
	for path, status := range map[string]string{"request-approval": "WAITING_APPROVAL", "approve": "APPROVED", "reject": "REJECTED", "mark-submitted": "SUBMITTED_MANUALLY", "mark-removed": "REMOVED", "close": "CLOSED", "start-review": "UNDER_REVIEW", "mark-reviewing": "PLATFORM_REVIEWING", "mark-not-removed": "NOT_REMOVED"} {
		to := status
		api.Post("/cases/:id/"+path, func(c *fiber.Ctx) error { return s.transition(c, to) })
	}
	api.Get("/runs", s.runs)
	api.Post("/runs", s.startRun)
	api.Post("/runs/:id/retry", s.retryRun)
	api.Get("/reports/daily", s.daily)
	api.Get("/reports/daily.csv", s.dailyCSV)
}
func id(c *fiber.Ctx) (int64, error) {
	v, e := strconv.ParseInt(c.Params("id"), 10, 64)
	if e != nil || v < 1 {
		return 0, fiber.NewError(400, "invalid id")
	}
	return v, nil
}
func page(c *fiber.Ctx) (int, int, error) {
	limit, offset := 50, 0
	var e error
	if v := c.Query("limit"); v != "" {
		limit, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, fiber.NewError(400, "invalid limit")
		}
	}
	if v := c.Query("offset"); v != "" {
		offset, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, fiber.NewError(400, "invalid offset")
		}
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return 0, 0, fiber.NewError(400, "limit must be 1..100; offset >= 0")
	}
	return limit, offset, nil
}
func date(c *fiber.Ctx, loc *time.Location) (string, error) {
	v := c.Query("date", time.Now().In(loc).Format("2006-01-02"))
	if _, e := time.Parse("2006-01-02", v); e != nil {
		return "", fiber.NewError(400, "date must be YYYY-MM-DD")
	}
	return v, nil
}
func (s *Server) subjects(c *fiber.Ctx) error {
	v, e := s.Store.Subjects(c.UserContext())
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"data": v, "total": len(v)})
}
func (s *Server) createSubject(c *fiber.Ctx) error {
	var v model.Subject
	if e := c.BodyParser(&v); e != nil {
		return fiber.NewError(400, "invalid subject JSON")
	}
	if e := config.ValidateSubject(v); e != nil {
		return fiber.NewError(400, e.Error())
	}
	if len(v.ReferenceImages) > 0 {
		return fiber.NewError(400, "configure reference image files locally in subjects YAML")
	}
	if e := s.Store.UpsertSubject(c.UserContext(), v); e != nil {
		return e
	}
	return c.Status(201).JSON(v)
}
func (s *Server) mentions(c *fiber.Ctx) error {
	limit, offset, e := page(c)
	if e != nil {
		return e
	}
	minRisk := 0
	if c.Query("min_risk") != "" {
		minRisk, e = strconv.Atoi(c.Query("min_risk"))
		if e != nil || minRisk < 0 || minRisk > 100 {
			return fiber.NewError(400, "min_risk must be 0..100")
		}
	}
	day := c.Query("date")
	if day != "" {
		if _, e = time.Parse("2006-01-02", day); e != nil {
			return fiber.NewError(400, "invalid date")
		}
	}
	v, n, e := s.Store.Mentions(c.UserContext(), model.MentionFilter{Subject: c.Query("subject"), Source: c.Query("source"), Date: day, Sentiment: c.Query("sentiment"), Category: c.Query("risk_category"), MinRisk: minRisk, Limit: limit, Offset: offset, Review: c.Locals("review") == true})
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"data": v, "total": n, "limit": limit, "offset": offset})
}
func (s *Server) mention(c *fiber.Ctx) error {
	v, e := id(c)
	if e != nil {
		return e
	}
	m, e := s.Store.Mention(c.UserContext(), v)
	if e != nil {
		return e
	}
	media, e := s.Store.Media(c.UserContext(), v)
	if e != nil {
		return e
	}
	return c.JSON(struct {
		model.Mention
		Media []storage.Media `json:"media"`
	}{m, media})
}
func (s *Server) createCase(c *fiber.Ctx) error {
	v, e := id(c)
	if e != nil {
		return e
	}
	if _, e = s.Store.Mention(c.UserContext(), v); e != nil {
		return e
	}
	cas, e := s.Store.CreateCase(c.UserContext(), v, "local-reviewer")
	if e != nil {
		return fiber.NewError(409, "case already exists or cannot be created")
	}
	s.Metrics.Inc("cases_total")
	return c.Status(201).JSON(cas)
}
func (s *Server) cases(c *fiber.Ctx) error {
	limit, offset, e := page(c)
	if e != nil {
		return e
	}
	v, e := s.Store.Cases(c.UserContext())
	if e != nil {
		return e
	}
	total := len(v)
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	return c.JSON(fiber.Map{"data": v[offset:end], "total": total, "limit": limit, "offset": offset})
}
func (s *Server) caseDetail(c *fiber.Ctx) error {
	v, e := id(c)
	if e != nil {
		return e
	}
	cases, e := s.Store.Cases(c.UserContext())
	if e != nil {
		return e
	}
	for _, cas := range cases {
		if cas.ID == v {
			audit, e := s.Store.CaseAudit(c.UserContext(), v)
			if e != nil {
				return e
			}
			m, e := s.Store.Mention(c.UserContext(), cas.MentionID)
			if e != nil {
				return e
			}
			return c.JSON(fiber.Map{"case": cas, "evidence": m, "audit": audit, "approvals": filterApprovals(audit)})
		}
	}
	return fiber.ErrNotFound
}
func filterApprovals(a []model.Audit) []model.Audit {
	out := []model.Audit{}
	for _, v := range a {
		if v.Action == "APPROVED" || v.Action == "REJECTED" || v.Action == "WAITING_APPROVAL" {
			out = append(out, v)
		}
	}
	return out
}
func (s *Server) transition(c *fiber.Ctx, to string) error {
	v, e := id(c)
	if e != nil {
		return e
	}
	if e = s.Store.TransitionCase(c.UserContext(), v, to, "local-reviewer"); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return e
		}
		return fiber.NewError(409, e.Error())
	}
	return c.JSON(fiber.Map{"id": v, "status": to, "external_action": false})
}
func (s *Server) runs(c *fiber.Ctx) error {
	limit, offset, e := page(c)
	if e != nil {
		return e
	}
	v, e := s.Store.Runs(c.UserContext())
	if e != nil {
		return e
	}
	total := len(v)
	if offset > total {
		offset = total
	}
	return c.JSON(fiber.Map{"data": v[offset:min(offset+limit, total)], "total": total, "limit": limit, "offset": offset})
}

type RunRequest struct {
	Subject  string `json:"subject_id"`
	Source   string `json:"source"`
	Query    string `json:"query"`
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}

func (s *Server) startRun(c *fiber.Ctx) error {
	var request RunRequest
	if e := c.BodyParser(&request); e != nil {
		return fiber.NewError(400, "invalid run JSON")
	}
	r, e := s.Supervisor.SubmitFiltered(c.UserContext(), request.Subject, request.Source, request.Query, request.DateFrom, request.DateTo)
	if e != nil {
		return fiber.NewError(409, e.Error())
	}
	return c.Status(202).JSON(r)
}
func (s *Server) retryRun(c *fiber.Ctx) error {
	r, e := s.Supervisor.Retry(c.UserContext(), c.Params("id"))
	if e != nil {
		return fiber.NewError(409, e.Error())
	}
	return c.Status(202).JSON(r)
}
func (s *Server) summary(c *fiber.Ctx) error {
	day := time.Now().In(s.Store.Location).Format("2006-01-02")
	mentions, e := s.Store.AllMentions(c.UserContext(), day)
	if e != nil {
		return e
	}
	sentiment := map[string]int{"positive": 0, "neutral": 0, "negative": 0, "unknown": 0}
	high := 0
	for _, v := range mentions {
		sentiment[v.Analysis.Sentiment]++
		if v.Analysis.RiskScore >= s.Config.RiskThreshold {
			high++
		}
	}
	_, review, e := s.Store.Mentions(c.UserContext(), model.MentionFilter{Review: true, Limit: 1})
	if e != nil {
		return e
	}
	s.Metrics.Review.Set(float64(review))
	health := "ready"
	ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
	defer cancel()
	if s.Ollama.Health(ctx) != nil {
		health = "unavailable"
	}
	sources := map[string]model.SessionStatus{}
	for name, adapter := range s.Supervisor.Sources {
		sources[name] = adapter.CheckSession(ctx)
	}
	return c.JSON(fiber.Map{"date": day, "demo": s.Config.Demo, "collector_status": s.Supervisor.Active(), "current_run": s.Supervisor.Active(), "mentions_today": len(mentions), "sentiment": sentiment, "high_risk": high, "review_queue": review, "source_health": sources, "browser_sessions": sources, "ollama_health": health, "image_matching": "Image similarity only; not face recognition", "schedule": s.Config.Scheduler.StartTime + "–" + s.Config.Scheduler.StopTime + " " + s.Config.App.Timezone})
}
func (s *Server) daily(c *fiber.Ctx) error {
	day, e := date(c, s.Store.Location)
	if e != nil {
		return e
	}
	d, e := s.Reports.Generate(c.UserContext(), day)
	if e != nil {
		return e
	}
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=report-%s.json", day))
	return c.JSON(d)
}
func (s *Server) dailyCSV(c *fiber.Ctx) error {
	day, e := date(c, s.Store.Location)
	if e != nil {
		return e
	}
	if _, e = s.Reports.Generate(c.UserContext(), day); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(s.Config.App.ReportDirectory, day, "mentions.csv"))
	if e != nil {
		return e
	}
	c.Set("Content-Disposition", "attachment; filename=mentions-"+day+".csv")
	c.Type("csv")
	return c.Send(b)
}
func (s *Server) evidence(c *fiber.Ctx) error {
	name := c.Params("name")
	if len(name) != 36 || !strings.HasSuffix(name, ".png") || strings.ContainsAny(name, "/\\") {
		return fiber.ErrNotFound
	}
	if _, e := os.Stat(filepath.Join(s.Config.App.EvidenceDirectory, name)); e != nil {
		return fiber.ErrNotFound
	}
	c.Type("png")
	b, e := os.ReadFile(filepath.Join(s.Config.App.EvidenceDirectory, name))
	if e != nil {
		return e
	}
	return c.Send(b)
}
