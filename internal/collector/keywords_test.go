package collector

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/model"
	"testing"
	"time"
)

func TestKeywords(t *testing.T) {
	s := model.Subject{FullName: "Nama Lengkap", PopularName: "Nama", Aliases: []string{"Nama"}, Position: "Direktur", Institution: "Instansi", OfficialHandles: []string{"@resmi"}, RiskKeywords: []string{"penipuan"}}
	q := Keywords(s)
	require.Equal(t, []string{`"Nama Lengkap"`, `"Nama"`, `"Nama Lengkap" "Direktur"`, `"Nama Lengkap" "Instansi"`, `"Nama Lengkap" penipuan`, `"@resmi"`}, q)
}
func TestWorkWindow(t *testing.T) {
	c := config.Defaults()
	s := New(context.Background(), c, nil, nil, nil, nil)
	defer s.Stop()
	loc, e := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, e)
	for _, hour := range []int{0, 7, 17, 23} {
		_, e = s.window(time.Date(2026, 10, 6, hour, 0, 0, 0, loc))
		require.Error(t, e)
	}
	for _, hour := range []int{8, 12, 16} {
		_, e = s.window(time.Date(2026, 10, 6, hour, 0, 0, 0, loc))
		require.NoError(t, e)
	}
}

func TestMentionDateFilter(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	before := time.Date(2026, 10, 4, 12, 0, 0, 0, loc)
	inside := time.Date(2026, 10, 5, 12, 0, 0, 0, loc)
	after := time.Date(2026, 10, 7, 12, 0, 0, 0, loc)
	mentions := []model.RawMention{{ContentText: "before", PublishedAt: &before}, {ContentText: "inside", PublishedAt: &inside}, {ContentText: "after", PublishedAt: &after}, {ContentText: "unknown"}}

	filtered := filterMentionDates(mentions, "2026-10-05", "2026-10-06", loc)
	require.Equal(t, []string{"inside", "unknown"}, []string{filtered[0].ContentText, filtered[1].ContentText})
	require.NoError(t, validateDateRange("2026-10-05", "2026-10-06"))
	require.Error(t, validateDateRange("2026-10-07", "2026-10-06"))
	require.Error(t, validateDateRange("05-10-2026", ""))
}
