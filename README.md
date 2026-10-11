# identity-monitor-agent

Agent Go 1.25 untuk monitoring penyebutan publik, analisis risiko dengan Ollama lokal, klasifikasi sentimen IndoBERT lokal, review manusia, dan laporan harian. Dashboard memakai layout social Metronic 8.2.3 demo2 dan CSS dari template yang tersedia di workspace, di-embed ke binary; tidak memerlukan Node atau CDN.

**Tidak ada API pengiriman report/takedown atau engagement ke platform.** `APPROVED` hanya catatan persetujuan. Operator sendiri melakukan pelaporan melalui platform, lalu memilih `Mark as submitted manually`.

## Demo cepat di Windows

Dari root proyek:

```powershell
.\scripts\dev.ps1 build
.\bin\identity-monitor-agent.exe demo
```

Buka **http://localhost:8080**. Demo menggunakan fixture HTML sintetis dan server mock Ollama pada loopback melalui client HTTP yang sama dengan mode live. Menghasilkan 4 mention, 1 review berisiko, 1 case otomatis, dan run ulang dengan 3 duplikat. Mode demo ditandai jelas pada UI dan laporan. Angka ini berlaku pada database demo baru.

Data demo terisolasi di `data/demo`, `evidence/demo`, dan `reports/demo`. Scheduler live tidak aktif dalam demo. Demo ekstraksi tidak membuka situs nyata; pengujian browser fixture dan dashboard di bawah menggunakan Chrome sungguhan.

Untuk server live di Windows, gunakan script kontrol agar proses mudah ditemukan dan dihentikan:

```powershell
.\scripts\server.cmd run     # foreground; Ctrl+C untuk berhenti
.\scripts\server.cmd status
.\scripts\server.cmd stop
```

```powershell
# Hanya seed + laporan, lalu keluar
.\bin\identity-monitor-agent.exe demo --once
# Port alternatif
.\bin\identity-monitor-agent.exe demo --address=127.0.0.1:8081
```

Go sistem workspace awal adalah 1.20.9. Toolchain Go 1.25.14 yang diunduh dari go.dev dan diverifikasi SHA-256 tersedia lokal di `.tools/go/bin`; skrip Windows otomatis memakainya tanpa mengubah Go sistem. `.tools` tidak masuk Git. Pada komputer lain instal Go 1.25 dari [distribusi resmi Go](https://go.dev/dl/), lalu gunakan `go` atau Makefile.

## Prasyarat dan mode live

1. Go 1.25, Chrome/Chromium, dan Ollama lokal. Chrome dapat dideteksi otomatis; isi `browser.executable_path` jika diperlukan. Headful adalah default.
2. Instal Ollama dari [situs resmi](https://ollama.com/download). Pilih model instruksi yang cocok dengan RAM/VRAM dan mendukung structured output dari [library model](https://ollama.com/library). Tidak ada nama model live yang di-hardcode.
3. Unduh model pilihan menggunakan `ollama pull <nama-model>`, lalu jalankan `ollama serve` jika layanan belum aktif. `ollama list` menunjukkan model yang tersedia.
4. Salin konfigurasi dan isi subjek yang memiliki dasar monitoring yang sah:

```powershell
Copy-Item configs/config.example.yaml configs/config.yaml
Copy-Item configs/subjects.example.yaml configs/subjects.yaml
$env:OLLAMA_TEXT_MODEL='<nama-model-yang-sudah-diunduh>'
$env:OLLAMA_BASE_URL='http://127.0.0.1:11434'
$env:OLLAMA_TIMEOUT='120s'
# Opsional; isi hanya jika model vision memang tersedia
$env:OLLAMA_VISION_MODEL=''
# Opsional, direkomendasikan pada laptop yang digunakan beberapa aplikasi
$env:LOCAL_API_TOKEN='<token-lokal-pilihan-operator>'
```

Environment menimpa YAML. Nilai `${VAR}` dalam YAML diekspansi dan aplikasi memuat `.env` dari direktori kerja bila tersedia. `OLLAMA_TIMEOUT` memakai format durasi Go, misalnya `120s`. Endpoint Ollama hanya boleh HTTP loopback; redirect HTTP ditolak. Startup memeriksa nama model melalui `/api/tags`, termasuk model vision jika dikonfigurasi, dan gagal dengan pesan `ollama pull ...` jika model belum ada. Mengganti model cukup mengubah environment/YAML dan restart.

Untuk mengaktifkan IndoBERT sekali saja di Windows:

```powershell
.\scripts\setup-indobert.cmd
```

Script ini mengunduh model INT8 `mdhugol/indonesia-bert-sentiment-classification` yang sudah dikonversi ke ONNX serta ONNX Runtime 1.29.0. Inferensi kemudian berjalan di proses Go yang sama tanpa Python dan tanpa API cloud. Label model adalah `positive`, `neutral`, dan `negative`; confidence di bawah ambang konfigurasi menjadi `unknown`. Model dan runtime disimpan lokal pada direktori yang diabaikan Git.

```powershell
# Browser terpisah per source; login dilakukan sepenuhnya oleh operator.
.\bin\identity-monitor-agent.exe browser bootstrap --source=youtube_public_search --config=configs/config.yaml
# Tekan Enter pada terminal setelah selesai login.

# Satu query default; hanya dalam jam kerja yang dikonfigurasi.
.\bin\identity-monitor-agent.exe run-once --subject=subject-001 --source=generic_web_search --config=configs/config.yaml --subjects=configs/subjects.yaml

# Scheduler + API + dashboard
.\bin\identity-monitor-agent.exe serve --config=configs/config.yaml --subjects=configs/subjects.yaml

# Migrasi otomatis juga dijalankan pada startup; command eksplisit:
.\bin\identity-monitor-agent.exe migrate --config=configs/config.yaml

# Laporan tanpa kebutuhan Ollama aktif
.\bin\identity-monitor-agent.exe report --date=2026-10-06 --config=configs/config.yaml
```

`run-once --query='"Nama Lengkap" penipuan'` menjalankan query spesifik. Scheduler menghasilkan seluruh kombinasi nama, alias, variasi ejaan yang dikonfigurasi, jabatan, instansi, username, dan kata risiko. Tidak ada variasi ejaan buatan AI.

Browser memakai `data/browser-profiles/<source>`; tidak memakai profile browser pribadi. Downloads dinonaktifkan, tidak membaca password manager/OTP. Bootstrap manual memang mengizinkan login interaktif; job otomatis menolak form submission/non-GET dan berhenti saat login, CAPTCHA, MFA, atau verifikasi terdeteksi. Tidak ada fingerprint spoofing, proxy rotation, atau bypass.

Threads tersedia sebagai `threads_public_search`. Login dilakukan satu kali secara manual dengan `browser bootstrap --source=threads_public_search`; collection berikutnya membuka URL pencarian Threads langsung, memakai sesi profile tersebut, membaca post publik, dan scroll sesuai batas konfigurasi. Agent tidak membuat gerakan mouse acak atau menyamarkan fingerprint.

## Komponen

| Direktori | Tanggung jawab |
|---|---|
| `cmd/collector`, `internal/app` | CLI, dependency wiring, startup, shutdown, instance lock |
| `internal/config`, `internal/model` | YAML/env validation, domain model dan transisi status |
| `internal/collector`, `internal/scheduler` | Query generator, supervisor, checkpoint, jadwal dan heartbeat |
| `internal/browser`, `internal/source`, `internal/extractor` | chromedp, profile, selector per adapter, ekstraksi/normalisasi |
| `internal/ollama`, `internal/analyzer` | HTTP lokal, model health, JSON schema, satu repair, risk policy |
| `internal/agent`, `internal/guardrail` | Fallback terbatas, validasi tools/domain, audit tiap langkah |
| `internal/storage`, `migrations` | Repository interface, SQLite pure Go, transaksi, migrations versioned |
| `internal/imagehash`, `internal/evidence`, `internal/securefs` | SHA-256/dHash, evidence, permission/ACL |
| `internal/dashboard`, `web` | Fiber API, Metronic embedded, CSP, token dan CSRF protection |
| `internal/report`, `internal/observability` | JSON/CSV, Prometheus, structured logs |
| `internal/demo` | Fixture HTML dan mock Ollama terisolasi |

Tidak ada paket uploader karena MVP tidak mengunggah file atau mengirim laporan.

## Perilaku operasional

- Default jadwal **08:00–17:00 Asia/Jakarta**, trigger pada jam mulai dan interval 60 menit. Deadline job dipotong pada 17:00. Default 1 job; konfigurasi maksimal 4, tetap hanya 1 browser per source. Query diproses berurutan dengan delay minimum 500 ms; default 2 detik, maksimum 10 hasil dan 3 scroll.
- File lock mencegah dua collector memakai data directory yang sama. SQLite unique constraint juga mencegah job identik aktif bersamaan. Run tersimpan sebelum eksekusi, dengan checkpoint jumlah yang sudah diproses. Run terputus menjadi `PAUSED`; retry melakukan pencarian ulang dengan deduplikasi, bukan memulihkan DOM browser lama. Scheduler mengulang query pada siklus berikutnya; retry manual tersedia untuk `FAILED`, `PAUSED`, dan `HUMAN_ACTION_REQUIRED`.
- Source yang run terakhirnya `HUMAN_ACTION_REQUIRED` dilewati scheduler sampai operator bootstrap dan retry manual berhasil. Tidak ada percobaan menyelesaikan challenge oleh AI.
- Deduplikasi per subjek/source memakai canonical URL atau hash teks ternormalisasi, diperiksa **sebelum** inferensi dan dilindungi unique constraints ketika commit. Mention + analysis + case otomatis + bukti + audit disimpan dalam satu transaksi.
- Analisis JSON divalidasi field, enum, tipe dan rentang. Maksimum satu repair JSON, retry transport maksimal satu. Kegagalan disimpan `ANALYSIS_FAILED` dan masuk review, bukan hasil sentimen palsu. Kritik/mention normal tidak menjadi violation hanya karena negatif. Case otomatis untuk skor melewati threshold atau kandidat impersonation/scam/doxxing/threat yang relevan.
- Fallback hanya setelah layout deterministik gagal. Maksimum 20 langkah, timeout 120 detik, domain dan navigasi dibatasi. `type_text`, `click_element`, dan `go_back` sengaja tidak dieksekusi dalam MVP. Tools tersebut memiliki schema, tetapi policy/runtime menolaknya. Fallback dapat membaca, scroll, screenshot, navigasi URL yang diizinkan, atau meminta bantuan; ekstraksi alternatif selalu ditandai low confidence untuk reviewer. Semua instruksi dari halaman adalah data tidak tepercaya.
- Screenshot merupakan halaman hasil pencarian saat koleksi, bukan bukti bahwa halaman tujuan sudah diverifikasi. Semua hasil live dalam satu query mendapat screenshot konteks; error browser mencoba mengambil diagnostic screenshot. Fixture demo tidak memalsukan screenshot evidence.
- Tanggal publikasi/engagement hanya diisi jika tersedia secara terstruktur; label relatif yang ambigu tidak ditebak. YouTube views yang tidak tersedia sebagai angka terstruktur tetap kosong. Layout sumber nyata dapat berubah dan memerlukan pembaruan selector.

## Foto dan evidence

Isi `reference_images` pada YAML dengan JPEG/PNG lokal yang memang boleh dipakai. Startup memvalidasi file dan menyimpan SHA-256 serta dHash 64-bit ke `subject_images`. Candidate media pada domain HTTPS allowlist dapat diambil dengan batas 10 MB/25 megapiksel, lalu dibandingkan dengan reference. Tambahkan hostname CDN yang memang diperlukan secara eksplisit. File non-gambar tidak dijalankan; data kandidat diolah di memori.

Hash/distance disimpan di `mention_media`; media yang sama/mirip diberi `duplicate_of`, tanpa menghapus mention berbeda yang kebetulan memakai foto sama. Tidak semua situs mengekspos thumbnail yang dapat dibaca; tidak ada jaminan menemukan seluruh penggunaan foto di internet. **dHash adalah kemiripan gambar, bukan face recognition**, dan lemah untuk crop, komposisi ulang, atau foto wajah berbeda. Interface `ImageMatcher` disediakan untuk pengembangan yang disetujui pada fase berikutnya. Model vision dapat dikonfigurasi/di-health-check tetapi belum digunakan untuk keputusan identitas.

## Dashboard, API, dan approval

Overview, Mentions (filter subject/source/date/sentiment/category/minimum risk), Review queue, Cases, Runs, serta download laporan tersedia di localhost:8080. Tombol `Set local API token` menyimpan token hanya pada memori tab. Refresh penuh halaman memerlukan pengisian ulang token. API token tidak dimasukkan ke URL/localStorage.

Semua endpoint di prompt tersedia. GET list mendukung `limit=1..100` dan `offset`, kecuali daftar subjects kecil. Contoh:

```powershell
Invoke-RestMethod 'http://127.0.0.1:8080/api/v1/mentions?min_risk=70&limit=20' -Headers @{Authorization="Bearer $env:LOCAL_API_TOKEN"}
Invoke-RestMethod 'http://127.0.0.1:8080/api/v1/cases/1/request-approval' -Method Post -ContentType 'application/json' -Body '{}' -Headers @{Authorization="Bearer $env:LOCAL_API_TOKEN"; 'X-Local-Request'='1'}
```

Mutasi memerlukan `X-Local-Request: 1`; Host loopback dan Origin yang sama divalidasi. Body maksimum 64 KB, rate limit 240 request/menit/IP. `/metrics` dan screenshot mengikuti proteksi token; `/healthz` hanya liveness dan `/readyz` memeriksa DB + model Ollama. Error memakai `{"error":{"code":400,"message":"..."}}`.

Transisi utama: `OPEN → WAITING_APPROVAL → APPROVED → SUBMITTED_MANUALLY → REMOVED/NOT_REMOVED → CLOSED`. Tersedia juga `UNDER_REVIEW`, `REJECTED`, `PLATFORM_REVIEWING`. Approve langsung dari OPEN ditolak. Riwayat persetujuan dan audit ditambahkan, tidak dapat diedit melalui API. Reviewer lokal ditandai `local-reviewer`; MVP ini belum sistem multi-user/non-repudiation.

## Laporan dan retention

Pukul 17:00, laporan ditulis ke `reports/YYYY-MM-DD/report.json` dan `mentions.csv`; demo memakai `reports/demo/...`. Tanggal mengikuti WIB, bukan tanggal UTC. Download dashboard memakai generator yang sama. Laporan mencakup total/per-source/sentiment/risk, kegagalan, human action, top topics berbasis frekuensi kata, dan ringkasan status case saat laporan dibuat. CSV menetralkan formula injection.

Mention default 90 hari dan screenshot 30 hari; evidence terkait case dipertahankan. Audit default 365 hari adalah ambang pembuatan arsip JSONL lokal di `data/audit-archives`; jurnal sumber tetap append-only dan tidak dipangkas otomatis. Kebijakan ini mempertahankan histori approval, dengan konsekuensi pertumbuhan database. Laptop yang tidur/mati tepat pukul 17:00 tidak menjalankan cron; gunakan command `report --date=...` untuk backfill.

## Pengujian dan developer commands

```powershell
.\scripts\dev.ps1 test
.\scripts\dev.ps1 test-race
.\scripts\dev.ps1 lint
.\scripts\dev.ps1 build

# Smoke test Chrome + fixture lokal, tidak perlu internet
$env:BROWSER_TEST='1'
.\.tools\go\bin\go.exe test ./internal/browser -v

# Dengan demo server aktif
$env:DASHBOARD_TEST_URL='http://127.0.0.1:8080'
$env:DASHBOARD_SCREENSHOT='evidence/demo/dashboard.png'
.\.tools\go\bin\go.exe test ./internal/browser -v
```

Makefile: `make setup`, `run`, `demo`, `test`, `test-race`, `lint`, `build`, `migrate`, `browser-bootstrap SOURCE=youtube_public_search`, `run-once SUBJECT=subject-001`, `report DATE=YYYY-MM-DD`. `GO` dapat dioverride. Race test membutuhkan C compiler; build produksi SQLite tetap pure Go dan dapat `CGO_ENABLED=0`. Lint memakai golangci-lint v2, konfigurasi `.golangci.yml`; versi lokal pengujian 2.5.0.

Unit/integration test memakai testify, SQLite temp, fixture HTML, dan mock HTTP loopback; tidak menghubungi situs nyata. Jalur integration: fixture → extraction → normalization → real Ollama HTTP client dengan mock → analysis policy → DB → dashboard API → case workflow → report. Tes browser opsional membuktikan Chrome dapat membuka fixture dan dashboard.

Docker Compose hanya menyediakan Ollama, bind loopback dan volume lokal. Aplikasi utama sebaiknya dijalankan native agar profile/headful Chrome bekerja. Dockerfile adalah binary/demo container; tidak membundel Chrome. Pada container demo gunakan host networking Linux atau jalankan native; app sengaja tidak bind ke 0.0.0.0. Model tidak otomatis diunduh oleh Compose.

Menambah adapter: implementasikan `SourceAdapter`, simpan selector/rule dalam adapter, tambahkan nama tervalidasi pada config/browser manager, register di `internal/app`, sediakan fixture dan test session/challenge/empty/layout; tetap gunakan policy domain dan browser manager yang sama. Tambahkan migrasi SQL bernomor baru, jangan mengedit migrasi yang sudah dipakai.

## Keamanan, ancaman, dan batasan

| Ancaman | Mitigasi / batas MVP |
|---|---|
| Agent salah klik / report abuse | Klik/form/upload/engagement tidak tersedia; tidak ada kode pengiriman laporan |
| Sesi akun dicuri | Profile khusus, tanpa baca credential; permission direktori 0700/file 0600, Windows ACL user saat ini; OS/user yang sudah terkompromi tetap berisiko |
| Screenshot sensitif | Evidence lokal terproteksi token/ACL, retention; reviewer harus memeriksa sebelum membagikan secara manual |
| Prompt injection | Halaman diperlakukan untrusted; schema tool ketat, allowlist domain, batas langkah, policy di luar LLM |
| False identity/sentiment positive | Confidence rendah/failure masuk review; kritik bukan otomatis pelanggaran; hash bukan identitas wajah |
| Web lokal diserang situs lain | Bind loopback, validasi Host/Origin/header, CSP, token opsional, parameter SQL, output HTML di-escape |
| File/CSV berbahaya | Browser download deny, gambar dibatasi/didekode, formula CSV dinetralkan |

Monitoring hanya untuk halaman publik atau akses akun monitoring yang sah; patuhi ketentuan situs dan aturan yang berlaku. Jangan menambahkan mekanisme bypass CAPTCHA/MFA/rate limit. AI tidak memutuskan apakah konten melanggar hukum atau wajib dihapus.

SQLite/profile/evidence/report/config lokal/.env diabaikan Git. Workspace awal bukan repository Git, sehingga tidak ada commit yang dibuat. File Metronic asli tetap dipertahankan; lisensi template pengguna tetap berlaku.

Troubleshooting:

- **Go version error:** gunakan `.tools/go/bin/go.exe` atau instal Go 1.25, bukan Go sistem 1.20.
- **Ollama/model unavailable:** jalankan Ollama, `ollama list`, `ollama pull <model>`, cocokkan nama environment/YAML. Tidak ada fallback cloud.
- **Chrome tidak ditemukan:** set `browser.executable_path` ke executable Chrome/Chromium. Jangan menunjuk profile browser pribadi.
- **Session expired/CAPTCHA/MFA:** run menjadi `HUMAN_ACTION_REQUIRED`; lakukan bootstrap manual, kemudian Retry. Challenge tidak dibypass.
- **Profile in use:** tutup sesi bootstrap/collector sebelumnya. Jangan menjalankan dua browser dengan profile yang sama.
- **Port 8080 terpakai:** hentikan proses aplikasi sendiri atau gunakan `--address=127.0.0.1:8081`.
- **Di luar jam kerja:** tunggu 08:00 WIB atau sesuaikan jam pada konfigurasi; mode demo terisolasi dapat dijalankan kapan pun.
- **401/403:** masukkan token lokal dan header mutasi; akses melalui localhost/127.0.0.1, bukan hostname eksternal.
- **Layout changed:** perbarui selector dengan fixture baru; fallback hanya read-only, hasil rendah confidence harus direview.

Fase berikutnya: uji adapter pada akun monitoring sah, kalibrasi analisis memakai dataset berlabel, perbaiki provenance gambar dan ekstraksi metadata relatif, identitas reviewer multi-user, backfill scheduler saat laptop bangun, dan strategi arsip terverifikasi. Tidak menambahkan pengiriman takedown otomatis.

Referensi implementasi: [chromedp](https://pkg.go.dev/github.com/chromedp/chromedp), [Ollama Chat API](https://docs.ollama.com/api/chat).
