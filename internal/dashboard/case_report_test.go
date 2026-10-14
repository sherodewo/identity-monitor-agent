package dashboard

import (
	"identity-monitor-agent/internal/model"
	"strings"
	"testing"
)

func TestCaseReportEscapesEvidenceAndHandlesMissingDate(t *testing.T) {
	p := casePackage{Case: model.Case{ID: 42}, Mention: model.Mention{RawMention: model.RawMention{ContentText: "<script>alert(1)</script>\nBaris kedua", AuthorHandle: "@contoh"}, Analysis: model.AnalysisResult{Summary: "Ringkasan", Reasons: []string{"Alasan satu"}}}}
	report := caseReportHTML(p)
	for _, want := range []string{"kasus #42", "&lt;script&gt;", "Baris kedua", "@contoh", "Tidak tersedia", "Ringkasan", "Alasan satu"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(report, "<script>") {
		t.Fatal("unescaped evidence in report")
	}
}
