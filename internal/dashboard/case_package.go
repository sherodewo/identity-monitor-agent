package dashboard

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type casePackage struct {
	Case        model.Case      `json:"case"`
	Mention     model.Mention   `json:"mention"`
	Subject     *model.Subject  `json:"subject,omitempty"`
	Media       []storage.Media `json:"media,omitempty"`
	Runs        []model.Run     `json:"collection_runs,omitempty"`
	Audit       []model.Audit   `json:"audit"`
	GeneratedAt time.Time       `json:"generated_at"`
}

func (s *Server) loadCasePackage(c *fiber.Ctx) (casePackage, error) {
	caseID, err := id(c)
	if err != nil {
		return casePackage{}, err
	}
	cases, err := s.Store.Cases(c.UserContext())
	if err != nil {
		return casePackage{}, err
	}
	var cas model.Case
	for _, v := range cases {
		if v.ID == caseID {
			cas = v
			break
		}
	}
	if cas.ID == 0 {
		return casePackage{}, fiber.ErrNotFound
	}
	m, err := s.Store.Mention(c.UserContext(), cas.MentionID)
	if err != nil {
		return casePackage{}, err
	}
	media, err := s.Store.Media(c.UserContext(), cas.MentionID)
	if err != nil {
		return casePackage{}, err
	}
	runs, err := s.Store.MentionRuns(c.UserContext(), cas.MentionID)
	if err != nil {
		return casePackage{}, err
	}
	audit, err := s.Store.CaseAudit(c.UserContext(), cas.ID)
	if err != nil {
		return casePackage{}, err
	}
	p := casePackage{Case: cas, Mention: m, Media: media, Runs: runs, Audit: audit, GeneratedAt: time.Now().UTC()}
	subjects, err := s.Store.Subjects(c.UserContext())
	if err != nil {
		return casePackage{}, err
	}
	for _, sub := range subjects {
		if sub.ID == m.SubjectID {
			v := sub
			p.Subject = &v
			break
		}
	}
	return p, nil
}

func (s *Server) casePackage(c *fiber.Ctx) error {
	p, err := s.loadCasePackage(c)
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	write := func(name string, b []byte) error {
		w, e := zw.Create(name)
		if e == nil {
			_, e = w.Write(b)
		}
		return e
	}
	if err := write("case.json", data); err != nil {
		return err
	}
	if err := write("email-draft.txt", []byte(emailDraft(p))); err != nil {
		return err
	}
	if err := write("report.html", []byte(htmlReport(p))); err != nil {
		return err
	}
	if path := p.Mention.ScreenshotPath; path != "" {
		full := filepath.Join(s.Config.App.EvidenceDirectory, path)
		if filepath.Base(path) == path && !strings.ContainsAny(path, "/\\") {
			if b, e := os.ReadFile(full); e == nil {
				if err = write("evidence/"+filepath.Base(full), b); err != nil {
					return err
				}
			}
		}
	}
	if err = zw.Close(); err != nil {
		return err
	}
	c.Type("zip")
	c.Attachment(fmt.Sprintf("case-%d-evidence.zip", p.Case.ID))
	return c.Send(out.Bytes())
}

func emailDraft(p casePackage) string {
	return fmt.Sprintf("To: [platform abuse/takedown email]\nSubject: Takedown request — case #%d — %s\n\nHello,\n\nPlease review the attached evidence for case #%d.\n\nSource: %s\nUsername: %s\nPublished: %v\nCollected: %s\n\nContent:\n%s\n\nRequested action: please review this content under your platform policy and take appropriate action.\n\nThis draft was generated locally and must be reviewed before sending.\n", p.Case.ID, p.Mention.Source, p.Case.ID, p.Mention.SourceURL, p.Mention.AuthorHandle, p.Mention.PublishedAt, p.Mention.CollectedAt.Format(time.RFC3339), p.Mention.ContentText)
}

func htmlReport(p casePackage) string {
	return caseReportHTML(p)
}
