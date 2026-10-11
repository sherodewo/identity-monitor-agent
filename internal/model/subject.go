package model

type Subject struct {
	ID               string   `json:"id" yaml:"id"`
	FullName         string   `json:"full_name" yaml:"full_name"`
	PopularName      string   `json:"popular_name" yaml:"popular_name"`
	Position         string   `json:"position" yaml:"position"`
	Institution      string   `json:"institution" yaml:"institution"`
	Aliases          []string `json:"aliases" yaml:"aliases"`
	SpellingVariants []string `json:"spelling_variants" yaml:"spelling_variants"`
	OfficialHandles  []string `json:"official_handles" yaml:"official_handles"`
	RiskKeywords     []string `json:"risk_keywords" yaml:"risk_keywords"`
	ReferenceImages  []string `json:"reference_images" yaml:"reference_images"`
}
