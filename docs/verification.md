# Verifikasi lokal — 6 Oktober 2026

Environment: Windows amd64, Go 1.25.14 di `.tools/go`, Chrome lokal, golangci-lint 2.5.0, GCC untuk race detector. Go sistem 1.20.9 tidak digunakan untuk build proyek.

| Pemeriksaan | Hasil |
|---|---|
| `go build ./...` | Lulus |
| `CGO_ENABLED=0 go build ./...` | Lulus; SQLite pure Go |
| `go test ./...` | Lulus |
| `CGO_ENABLED=1 go test -race ./...` | Lulus |
| `golangci-lint run` | Lulus, 0 issues |
| Chrome membaca fixture lokal + screenshot | Lulus (`BROWSER_TEST=1`) |
| Chrome membuka dashboard dan seluruh tab | Lulus (`DASHBOARD_TEST_URL`) |
| GET `/readyz` pada demo port 8080 | `ready` |
| Fixture extraction → mock Ollama HTTP → SQLite → API | 4 mention unik |
| Pencarian fixture web diulang | 3 duplikat, 0 mention baru |
| Review queue | 1 kandidat berisiko |
| Case approval state machine | Transisi sah lulus, approval langsung dari OPEN ditolak |
| CAPTCHA/verification | Run `HUMAN_ACTION_REQUIRED`, tanpa mention tersimpan |
| Fallback prompt injection | Navigasi ke domain asing ditolak dan diaudit |
| Laporan JSON/CSV | Dibuat di `reports/demo/2026-10-06` |
| Proteksi API | Host/Origin asing, header mutasi hilang, token salah, pagination invalid ditolak |

Screenshot dashboard: `evidence/demo/dashboard.png` (artefak runtime, diabaikan Git). Pengujian integration menggunakan database temp sendiri dan tidak mengubah persetujuan case pada dashboard demo.

Tidak dilakukan login akun, scraping situs produksi, instalasi/download model Ollama besar, atau inferensi model nyata. Demo memakai mock lokal berlabel `fixture-mock`; kelayakan selector live dan akurasi model perlu divalidasi oleh operator pada subjek dan akun monitoring yang sah.

Workspace bukan repository Git; tidak ada commit. `.gitignore` melindungi database, evidence, report, browser profile, `.env`, konfigurasi lokal, binary, dan toolchain. Template Metronic asli tetap tersedia.
