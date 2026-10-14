package model

// Relevance is decided only by a successful context stage. The zero-value
// Relevant=false on a newly scraped or failed mention must not exclude it.
func (m Mention) RelevanceState() string {
	if m.Analysis.ManualRelevance == "relevant" || m.Analysis.ManualRelevance == "excluded" {
		return m.Analysis.ManualRelevance
	}
	if m.Analysis.ContextStatus != "COMPLETED" && m.AnalysisStatus != "COMPLETED" {
		return "pending"
	}
	if m.Analysis.Relevant {
		return "relevant"
	}
	return "excluded"
}
