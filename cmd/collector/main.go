package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"identity-monitor-agent/internal/app"
	"identity-monitor-agent/internal/browser"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/scheduler"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if e := run(); e != nil {
		slog.Error("command failed", "error", e.Error())
		os.Exit(1)
	}
}
func run() error {
	if err := config.LoadEnvFile(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	args := os.Args[1:]
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	if command == "browser" {
		if len(args) == 0 || args[0] != "bootstrap" {
			return fmt.Errorf("usage: browser bootstrap --source=<source>")
		}
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := flags.String("config", "configs/config.example.yaml", "YAML config")
	subjects := flags.String("subjects", "configs/subjects.example.yaml", "subjects YAML")
	sourceName := flags.String("source", "generic_web_search", "source adapter")
	subject := flags.String("subject", "subject-001", "subject id")
	query := flags.String("query", "", "search query (default first generated query)")
	day := flags.String("date", "", "report date YYYY-MM-DD")
	dateFrom := flags.String("date-from", "", "only store results published on/after YYYY-MM-DD")
	dateTo := flags.String("date-to", "", "only store results published on/before YYYY-MM-DD")
	demoFlag := flags.Bool("demo", false, "synthetic fixture mode; isolated data directory")
	address := flags.String("address", "", "override loopback HTTP address")
	once := flags.Bool("once", false, "seed demo and exit")
	outsideHours := flags.Bool("allow-outside-hours", false, "allow manual run-once outside the collection schedule")
	if e := flags.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return nil
		}
		return e
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	c, e := config.Load(*configPath)
	if e != nil {
		return e
	}
	c.Demo = *demoFlag || command == "demo"
	if *outsideHours && command != "run-once" {
		return fmt.Errorf("--allow-outside-hours is only supported for run-once")
	}
	c.AllowOutsideHours = *outsideHours
	if c.Demo {
		c.App.DataDirectory = filepath.Join(c.App.DataDirectory, "demo")
		c.App.EvidenceDirectory = filepath.Join(c.App.EvidenceDirectory, "demo")
		c.App.ReportDirectory = filepath.Join(c.App.ReportDirectory, "demo")
		c.Scheduler.Enabled = false
	}
	if *address != "" {
		c.App.HTTPAddress = *address
	}
	if e = c.Validate(); e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "browser" {
		for _, s := range c.Sources {
			if s.Name == *sourceName {
				manager := browser.New(c.Browser, c.App.EvidenceDirectory, observability.New())
				session, e := manager.Open(ctx, s.Name, guardrail.Policy{Domains: s.AllowedDomains}, true)
				if e != nil {
					return e
				}
				defer session.Close()
				home := "https://" + s.AllowedDomains[0]
				if e = session.BootstrapNavigate(home); e != nil {
					return e
				}
				fmt.Println("Login manually in the dedicated browser. Press Enter here when finished. No password/OTP is read by the agent.")
				done := make(chan struct{})
				go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); close(done) }()
				select {
				case <-ctx.Done():
				case <-done:
				}
				return nil
			}
		}
		return fmt.Errorf("unknown source")
	}
	if command != "serve" && command != "demo" && command != "run-once" && command != "report" && command != "migrate" {
		return fmt.Errorf("commands: serve, demo, run-once, report, migrate, browser bootstrap")
	}
	runtime, e := app.Open(ctx, c, *subjects, command != "report" && command != "migrate")
	if e != nil {
		return e
	}
	defer runtime.Close()
	switch command {
	case "migrate":
		fmt.Println("SQLite migrations applied")
		return nil
	case "report":
		if *day == "" {
			*day = time.Now().In(runtime.Store.Location).Format("2006-01-02")
		}
		r, e := runtime.Reports.Generate(ctx, *day)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	case "run-once":
		r, e := runtime.Supervisor.SubmitFiltered(ctx, *subject, *sourceName, *query, *dateFrom, *dateTo)
		if e != nil {
			return e
		}
		r, e = runtime.Supervisor.Wait(ctx, r.ID)
		if e != nil {
			return e
		}
		if e = json.NewEncoder(os.Stdout).Encode(r); e != nil {
			return e
		}
		if r.Status != "COMPLETED" {
			return fmt.Errorf("collection ended with %s", r.Status)
		}
		return nil
	case "demo":
		if e = runtime.Demo(ctx); e != nil {
			return e
		}
		if *once {
			return nil
		}
	}
	var scheduled *scheduler.Scheduler
	if c.Scheduler.Enabled {
		scheduled, e = scheduler.New(ctx, runtime.Supervisor, runtime.Reports)
		if e != nil {
			return e
		}
		scheduled.Start()
		defer scheduled.Stop()
	}
	errCh := make(chan error, 1)
	go func() { errCh <- runtime.Dashboard.App.Listen(c.App.HTTPAddress) }()
	slog.Info("dashboard listening", "address", c.App.HTTPAddress, "demo", c.Demo)
	select {
	case <-ctx.Done():
		return runtime.Dashboard.App.ShutdownWithTimeout(5 * time.Second)
	case e := <-errCh:
		return e
	}
}
