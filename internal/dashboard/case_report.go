package dashboard

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	cdpPage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/gofiber/fiber/v2"
)

func caseReportHTML(p casePackage) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=utf-8><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'\"><style>body{font:14px sans-serif;color:#182230;line-height:1.6}h1{font-size:26px}dt{font-weight:bold;margin-top:12px}dd{margin:0;white-space:pre-wrap;overflow-wrap:anywhere}section{margin-top:24px}small{color:#475467}</style></head><body>")
	fmt.Fprintf(&b, "<h1>Laporan kasus #%d</h1><small>Dibuat %s · waktu UTC</small><dl>", p.Case.ID, p.GeneratedAt.Format(time.RFC3339))
	field := func(k, v string) {
		if v == "" {
			v = "Tidak tersedia"
		}
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>", html.EscapeString(k), html.EscapeString(v))
	}
	m := p.Mention
	field("Status kasus", p.Case.Status)
	field("Subjek", m.SubjectID)
	if p.Subject != nil {
		field("Nama subjek", p.Subject.FullName)
	}
	field("Platform", m.Source)
	field("Nama penulis", m.AuthorName)
	field("Username", m.AuthorHandle)
	field("URL asli", m.SourceURL)
	published := "Tidak tersedia"
	if m.PublishedAt != nil {
		published = m.PublishedAt.Format(time.RFC3339)
	}
	field("Tanggal publikasi", published)
	field("Tanggal pengumpulan", m.CollectedAt.Format(time.RFC3339))
	field("Teks asli", m.ContentText)
	field("Relevansi", m.Relevance)
	field("Sentimen", m.Analysis.Sentiment)
	field("Kategori risiko", m.Analysis.RiskCategory)
	field("Skor risiko", fmt.Sprintf("%d/100", m.Analysis.RiskScore))
	field("Ringkasan analisis", m.Analysis.Summary)
	field("Alasan analisis", strings.Join(m.Analysis.Reasons, "\n"))
	field("Screenshot dalam paket bukti", m.ScreenshotPath)
	field("Hash konten", m.ContentHash)
	for _, r := range p.Runs {
		field("Pencarian / eksekusi", r.Query+" / "+r.ExecutionID)
	}
	for _, a := range p.Audit {
		field("Riwayat", a.CreatedAt+" · "+a.Action+" · "+a.Actor)
	}
	b.WriteString("</dl><section>Analisis AI merupakan bahan tinjauan. Reviewer perlu memverifikasi konten dan alasan pelaporan sebelum mengirim laporan kepada platform. Screenshot, bila tersedia, disertakan dalam paket bukti ZIP.</section></body></html>")
	return b.String()
}

func (s *Server) caseEmail(c *fiber.Ctx) error {
	p, err := s.loadCasePackage(c)
	if err != nil {
		return err
	}
	c.Attachment(fmt.Sprintf("case-%d-email-draft.txt", p.Case.ID))
	c.Type("txt")
	return c.SendString(emailDraft(p))
}

func (s *Server) casePDF(c *fiber.Ctx) error {
	p, err := s.loadCasePackage(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 25*time.Second)
	defer cancel()
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if s.Config.Browser.ExecutablePath != "" {
		opts = append(opts, chromedp.ExecPath(s.Config.Browser.ExecutablePath))
	}
	alloc, stop := chromedp.NewExecAllocator(ctx, opts...)
	defer stop()
	browser, closeBrowser := chromedp.NewContext(alloc)
	defer closeBrowser()
	var pdf []byte
	err = chromedp.Run(browser, chromedp.Navigate("about:blank"), chromedp.ActionFunc(func(ctx context.Context) error {
		tree, e := cdpPage.GetFrameTree().Do(ctx)
		if e != nil {
			return e
		}
		if e = cdpPage.SetDocumentContent(tree.Frame.ID, caseReportHTML(p)).Do(ctx); e != nil {
			return e
		}
		pdf, _, e = cdpPage.PrintToPDF().WithPrintBackground(true).Do(ctx)
		return e
	}))
	if err != nil {
		return fiber.NewError(503, "PDF gagal dibuat. Pastikan Chrome tersedia lalu coba lagi.")
	}
	c.Attachment(fmt.Sprintf("case-%d-report.pdf", p.Case.ID))
	c.Type("pdf")
	return c.Send(pdf)
}
