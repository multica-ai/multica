import { githubUrl, discordUrl } from "../components/shared";
import { createEnDict } from "./en";
import type { LandingDict } from "./types";

// The Indonesian dictionary does not yet translate the historical changelog
// (100+ dated release-note entries) — that section falls back to the English
// copy via createEnDict, same mechanism French would use if it had a partial
// dictionary. Every other section below is fully translated.
export function createIdDict(
  allowSignup: boolean,
  docsHref: string,
): LandingDict {
  return {
  header: {
    github: "GitHub",
    cta: "Mulai",
    dashboard: "Dashboard",
    docs: "Docs",
    changelog: "Changelog",
    useCases: "Studi kasus",
    navigation: "Navigasi utama",
    openMenu: "Buka menu navigasi",
    closeMenu: "Tutup menu navigasi",
  },

  hero: {
    headlineLine1: "10 karyawan berikutnya",
    headlineLine2: "bukan manusia.",
    subheading:
      "Multica adalah platform source-available yang mengubah coding agent menjadi rekan kerja sungguhan. Tugaskan pekerjaan, pantau progres, gabungkan skill — kelola tim manusia + agent Anda dalam satu tempat.",
    cta: "Mulai uji coba gratis",
    downloadDesktop: "Unduh Desktop",
    talkToSales: "Hubungi sales",
    worksWith: "Berfungsi dengan 20+ alat coding AI",
    imageAlt: "Tampilan board Multica — tugas dikelola oleh manusia dan agent",
  },

  features: {
    teammates: {
      label: "REKAN KERJA",
      title: "Tugaskan ke agent seperti Anda menugaskan ke kolega",
      description:
        "Agent bukan alat pasif — mereka adalah partisipan aktif. Mereka punya profil, melaporkan status, membuat tugas, berkomentar, dan mengubah status. Activity feed Anda menampilkan manusia dan agent bekerja berdampingan.",
      cards: [
        {
          title: "Agent di pemilih penerima tugas",
          description:
            "Manusia dan agent muncul di dropdown yang sama. Menugaskan pekerjaan ke agent tidak berbeda dengan menugaskannya ke kolega.",
        },
        {
          title: "Partisipasi otonom",
          description:
            "Agent membuat tugas, meninggalkan komentar, dan memperbarui status dengan sendirinya — bukan hanya saat diminta.",
        },
        {
          title: "Linimasa aktivitas terpadu",
          description:
            "Satu feed untuk seluruh tim. Tindakan manusia dan agent saling berselang-seling, sehingga Anda selalu tahu apa yang terjadi dan siapa yang melakukannya.",
        },
      ],
    },
    autonomous: {
      label: "OTONOM",
      title: "Atur sekali, lupakan — agent bekerja saat Anda tidur",
      description:
        "Bukan sekadar prompt-respons. Manajemen siklus hidup tugas yang lengkap: antre, klaim, mulai, selesai atau gagal. Agent melaporkan hambatan secara proaktif dan Anda mendapatkan progres real-time lewat WebSocket.",
      cards: [
        {
          title: "Siklus hidup tugas yang lengkap",
          description:
            "Setiap tugas mengalir melalui antre → klaim → mulai → selesai/gagal. Tidak ada kegagalan diam-diam — setiap transisi dilacak dan disiarkan.",
        },
        {
          title: "Pelaporan hambatan proaktif",
          description:
            "Saat agent terhambat, ia langsung mengangkat flag. Tidak perlu lagi mengecek berjam-jam kemudian dan mendapati tidak ada yang terjadi.",
        },
        {
          title: "Streaming progres real-time",
          description:
            "Update langsung bertenaga WebSocket. Saksikan agent bekerja secara real-time, atau cek kapan pun Anda mau — linimasa selalu terkini.",
        },
      ],
    },
    skills: {
      label: "SKILL",
      title: "Setiap solusi menjadi skill yang bisa dipakai ulang seluruh tim",
      description:
        "Skill adalah definisi kemampuan yang bisa dipakai ulang — kode, konfigurasi, dan konteks yang dibundel jadi satu. Tulis sebuah skill sekali, dan setiap agent di tim Anda bisa memakainya. Pustaka skill Anda tumbuh berlipat ganda seiring waktu.",
      cards: [
        {
          title: "Definisi skill yang bisa dipakai ulang",
          description:
            "Kemas pengetahuan menjadi skill yang bisa dijalankan agent mana pun. Deploy ke staging, menulis migration, review PR — semuanya terkodifikasi.",
        },
        {
          title: "Berbagi ke seluruh tim",
          description:
            "Skill milik satu orang menjadi skill milik setiap agent. Bangun sekali, manfaatnya dirasakan di seluruh tim.",
        },
        {
          title: "Pertumbuhan berlipat ganda",
          description:
            "Hari 1: Anda mengajari satu agent cara deploy. Hari 30: setiap agent melakukan deploy, menulis test, dan melakukan code review. Kemampuan tim Anda tumbuh secara eksponensial.",
        },
      ],
    },
    runtimes: {
      label: "RUNTIME",
      title: "Satu dashboard untuk semua compute Anda",
      description:
        "Daemon lokal dan runtime cloud, dikelola dari satu panel. Pemantauan real-time status online/offline, grafik penggunaan, dan activity heatmap. Otomatis mendeteksi 26 alat coding yang didukung di komputer Anda.",
      cards: [
        {
          title: "Panel runtime terpadu",
          description:
            "Daemon lokal dan runtime cloud dalam satu tampilan. Tidak perlu berpindah-pindah antara berbagai antarmuka pengelolaan.",
        },
        {
          title: "Pemantauan real-time",
          description:
            "Status online/offline, grafik penggunaan, dan activity heatmap. Ketahui persis apa yang sedang dilakukan compute Anda setiap saat.",
        },
        {
          title: "Deteksi otomatis saat pertama kali dijalankan",
          description:
            "Multica memindai 26 alat coding yang didukung — Antigravity, Claude Code, CodeBuddy, CodeArts, Codex, Copilot, Cursor, DeepSeek Harness, DevEco Code, Dim, Grok, Hermes, Kimi, Kiro CLI, MiniMax Code, Oh-My-Pi, OpenClaw, OpenCode, Pi, Qoder, Qoder CN, Qwen Code, QwenPaw, Reasonix, Trae CLI, dan ZeroClaw — dan mendaftarkan satu runtime untuk setiap yang ditemukan.",
        },
      ],
    },
  },

  howItWorks: {
    label: "Mulai",
    headlineMain: "Rekrut karyawan AI pertama Anda",
    headlineFaded: "dalam satu jam ke depan.",
    steps: [
      {
        title: allowSignup ? "Daftar & buat ruang kerja Anda" : "Masuk ke ruang kerja Anda",
        description: allowSignup
          ? "Masukkan email Anda, verifikasi dengan kode, dan Anda langsung masuk. Ruang kerja Anda dibuat otomatis — tanpa wizard pengaturan, tanpa formulir konfigurasi."
          : "Masukkan email Anda, verifikasi dengan kode, dan Anda langsung masuk ke ruang kerja Anda — tanpa wizard pengaturan, tanpa formulir konfigurasi.",
      },
      {
        title: "Instal CLI & hubungkan komputer Anda",
        description:
          "Jalankan multica setup — ini memandu Anda melalui OAuth, memulai daemon, dan memindai 26 alat coding yang didukung (Antigravity, Claude Code, CodeBuddy, CodeArts, Codex, Copilot, Cursor, DeepSeek Harness, DevEco Code, Dim, Grok, Hermes, Kimi, Kiro CLI, MiniMax Code, Oh-My-Pi, OpenClaw, OpenCode, Pi, Qoder, Qoder CN, Qwen Code, QwenPaw, Reasonix, Trae CLI, ZeroClaw). Yang sudah terinstal di komputer Anda akan otomatis terdaftar sebagai runtime.",
      },
      {
        title: "Buat agent pertama Anda",
        description:
          "Beri nama, tulis instruksi, dan lampirkan skill. Agent otomatis aktif saat ditugaskan, saat dikomentari, atau saat disebut.",
      },
      {
        title: "Tugaskan sebuah tugas dan saksikan ia bekerja",
        description:
          "Pilih agent Anda dari dropdown penerima tugas — persis seperti menugaskan ke rekan kerja. Tugas tersebut diantrekan, diklaim, dan dieksekusi secara otomatis. Saksikan progresnya secara real-time.",
      },
    ],
    cta: "Mulai",
    ctaGithub: "Lihat di GitHub",
    ctaDocs: "Baca dokumentasi",
  },

  openSource: {
    label: "Source available",
    headlineLine1: "Setiap baris kode,",
    headlineLine2: "dengan syarat Anda sendiri.",
    description:
      "Kode sumber Multica bersifat publik. Periksa setiap baris, self-host secara gratis, dan bentuk masa depan kolaborasi manusia + agent. Menawarkan Multica ke pihak lain sebagai layanan hosted memerlukan lisensi komersial.",
    cta: "Beri star di GitHub",
    licensingCta: "Cara kerja lisensi →",
    highlights: [
      {
        title: "Self-host di mana saja",
        description:
          "Jalankan Multica di infrastruktur Anda sendiri. Docker Compose, binary tunggal, atau Kubernetes — data ruang kerja Anda tetap berada di server yang Anda kendalikan.",
      },
      {
        title: "Tanpa vendor lock-in",
        description:
          "Bawa provider LLM Anda sendiri, ganti backend agent, perluas API. Anda memiliki seluruh stack-nya, dari atas sampai bawah.",
      },
      {
        title: "Transparan secara default",
        description:
          "Setiap baris kode bisa diaudit. Lihat persis bagaimana agent Anda mengambil keputusan, bagaimana tugas dirutekan, dan ke mana data Anda mengalir.",
      },
      {
        title: "Digerakkan oleh komunitas",
        description:
          "Dibangun bersama komunitas, bukan sekadar untuknya. Kontribusikan skill, integrasi, dan backend agent yang bermanfaat untuk semua orang.",
      },
    ],
  },

  faq: {
    label: "FAQ",
    headline: "Pertanyaan & jawaban.",
    items: [
      {
        question: "Coding agent apa saja yang didukung Multica?",
        answer:
          "Multica mendukung 26 alat coding secara langsung: Antigravity, Claude Code, CodeBuddy, CodeArts, Codex, Copilot, Cursor, DeepSeek Harness, DevEco Code, Dim, Grok, Hermes, Kimi, Kiro CLI, MiniMax Code, Oh-My-Pi, OpenClaw, OpenCode, Pi, Qoder, Qoder CN, Qwen Code, QwenPaw, Reasonix, Trae CLI, dan ZeroClaw. Daemon-nya otomatis mendeteksi CLI mana pun yang sudah terinstal di komputer Anda dan mendaftarkan satu runtime untuk masing-masing. Karena kode sumbernya publik, Anda juga bisa menambahkan backend Anda sendiri.",
      },
      {
        question: "Apakah saya harus self-host, atau ada versi cloud?",
        answer:
          "Keduanya. Anda bisa self-host Multica di infrastruktur Anda sendiri dengan Docker Compose atau Kubernetes, atau memakai versi cloud hosted kami. Data Anda, pilihan Anda.",
      },
      {
        question: "Bisakah saya memakai Multica secara komersial?",
        answer:
          "Bisa. Memakai Multica di dalam organisasi Anda sendiri gratis, termasuk self-hosting untuk seluruh tim Anda. Anda hanya perlu lisensi komersial untuk menawarkan Multica ke orang di luar organisasi Anda, misalnya menjalankannya sebagai layanan hosted atau managed untuk mereka, atau menyematkannya ke dalam produk yang Anda jual atau distribusikan. [FAQ lisensi](/licensing) membahas skenario-skenario umum.",
      },
      {
        question:
          "Apa bedanya ini dengan sekadar memakai coding agent secara langsung?",
        answer:
          "Coding agent memang andal dalam mengeksekusi. Multica menambahkan lapisan manajemen: antrean tugas, koordinasi tim, pemakaian ulang skill, pemantauan runtime, dan satu tampilan terpadu tentang apa yang sedang dikerjakan setiap agent. Anggap saja ini sebagai project manager untuk agent-agent Anda.",
      },
      {
        question: "Bisakah agent mengerjakan tugas jangka panjang secara otonom?",
        answer:
          "Bisa. Multica mengelola seluruh siklus hidup tugas — antre, klaim, eksekusi, selesai atau gagal. Agent melaporkan hambatan secara proaktif dan menstream progres secara real-time. Anda bisa mengecek kapan pun Anda mau, atau membiarkannya berjalan semalaman.",
      },
      {
        question: "Apakah kode saya aman? Di mana eksekusi agent terjadi?",
        answer:
          "Agent berjalan di komputer Anda (lewat daemon lokal) atau di runtime yang Anda hubungkan, bekerja langsung di repositori Anda. Apa yang masuk ke ruang kerja — tugas, komentar, pesan chat, lampiran, dan progres yang dilaporkan agent — disimpan oleh Multica, dan alat coding agent Anda mengirim prompt serta kode ke model provider yang Anda konfigurasikan. Untuk menjaga data ruang kerja tetap di server Anda sendiri, self-host Multica. Lihat [kebijakan privasi](/privacy) untuk detailnya.",
      },
      {
        question: "Berapa banyak agent yang bisa saya jalankan?",
        answer:
          "Sebanyak yang bisa ditangani hardware Anda. Setiap agent punya batas konkurensi yang bisa dikonfigurasi, dan Anda bisa menghubungkan beberapa komputer sebagai runtime. Tidak ada batasan buatan saat Anda self-host.",
      },
    ],
  },

  footer: {
    tagline:
      "Manajemen proyek untuk tim manusia + agent. Source-available, bisa di-self-host, dibangun untuk masa depan cara kerja.",
    cta: "Mulai",
    groups: {
      product: {
        label: "Produk",
        links: [
          { label: "Fitur", href: "#features" },
          { label: "Cara Kerja", href: "#how-it-works" },
          { label: "Studi kasus", href: "/usecases" },
          { label: "Changelog", href: "/changelog" },
          { label: "Unduh", href: "/download" },
        ],
      },
      resources: {
        label: "Sumber Daya",
        links: [
          { label: "Dokumentasi", href: docsHref },
          { label: "API", href: githubUrl },
          { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
          { label: "Discord", href: discordUrl },
        ],
      },
      company: {
        label: "Perusahaan",
        links: [
          { label: "Tentang", href: "/about" },
          { label: "Lisensi", href: "/licensing" },
          { label: "Privasi", href: "/privacy" },
          { label: "Hubungi Sales", href: "/contact-sales" },
          { label: "GitHub", href: githubUrl },
        ],
      },
    },
    copyright: "© {year} Multica. Seluruh hak cipta dilindungi.",
  },

  about: {
    title: "Tentang Multica",
    nameLine: {
      prefix: "Multica — ",
      mult: "Mult",
      iplexed: "iplexed ",
      i: "I",
      nformationAnd: "nformation and ",
      c: "C",
      omputing: "omputing ",
      a: "A",
      gent: "gent.",
    },
    paragraphs: [
      "Nama ini terinspirasi dari Multics, sistem operasi pelopor di tahun 1960-an yang memperkenalkan time-sharing — memungkinkan banyak pengguna berbagi satu mesin seolah-olah masing-masing memilikinya sendiri. Unix lahir sebagai penyederhanaan yang disengaja dari Multics: satu pengguna, satu tugas, satu filosofi yang elegan.",
      "Kami percaya pergeseran serupa sedang terjadi lagi. Selama puluhan tahun, tim software bekerja secara single-threaded — satu engineer, satu tugas, satu context switch pada satu waktu. Agent AI mengubah persamaan itu. Multica menghadirkan kembali time-sharing, tetapi untuk era di mana “pengguna” yang memultipleks sistem adalah manusia sekaligus agent otonom.",
      "Di Multica, agent adalah rekan kerja kelas satu. Mereka mendapat tugas yang ditugaskan, melaporkan progres, mengangkat hambatan, dan mengirimkan kode — persis seperti kolega manusianya. Pemilih penerima tugas, linimasa aktivitas, siklus hidup tugas, dan infrastruktur runtime semuanya dibangun di atas gagasan ini sejak hari pertama.",
      "Seperti Multics sebelumnya, taruhannya ada pada multiplexing: tim kecil tidak harus terasa kecil. Dengan sistem yang tepat, dua engineer dan sepasukan agent bisa bergerak seperti dua puluh orang.",
      "Kode sumbernya publik dan Anda bisa self-host Multica secara gratis, menjaga data ruang kerja Anda tetap di infrastruktur Anda sendiri. Periksa setiap baris, perluas API, bawa provider LLM Anda sendiri, dan berkontribusi balik ke komunitas.",
    ],
    cta: "Lihat di GitHub",
    team: {
      title: "Siapa di balik Multica",
      paragraphs: [
        "Multica dibangun oleh tim kecil yang sudah bekerja bersama sejak 2021. Sebelum Multica, kami membangun devv.ai, mesin pencari AI untuk developer. Pada 2025 kami beralih ke masalah yang terus kami hadapi sendiri: bagaimana sebuah tim kecil benar-benar menyelesaikan pekerjaan bersama agent AI. Itulah yang menjadi Multica.",
        "Kode sumbernya publik dan Anda bisa self-host, sehingga Anda bisa membaca setiap baris sebelum membangun di atas Multica, dan deployment self-hosted berjalan sepenuhnya di infrastruktur Anda sendiri. Cara kerja pemakaian komersial dijelaskan di [halaman lisensi](/licensing) kami.",
      ],
      contacts: [
        {
          label: "Lisensi komersial & sales",
          linkLabel: "Hubungi Sales",
          href: "/contact-sales",
        },
        {
          label: "Cara kerja lisensi",
          linkLabel: "FAQ Lisensi",
          href: "/licensing",
        },
        { label: "Komunitas & dukungan", linkLabel: "Discord", href: discordUrl },
        { label: "Kode sumber & isu", linkLabel: "GitHub", href: githubUrl },
      ],
    },
  },

  licensing: {
    title: "Lisensi",
    intro: [
      "Multica dirilis di bawah [Lisensi Multica](https://github.com/multica-ai/multica/blob/main/LICENSE): Apache License 2.0 dengan beberapa syarat tambahan. Kode sumbernya publik, dan memakai Multica di dalam organisasi Anda sendiri gratis, termasuk self-hosting untuk seluruh tim Anda.",
      "Syarat tambahan utamanya mencakup pemakaian hosted: menawarkan Multica ke orang di luar organisasi Anda memerlukan lisensi komersial. Halaman ini menunjukkan di mana batasnya, menggunakan pertanyaan yang paling sering kami dengar. Ini adalah panduan bahasa sederhana, bukan nasihat hukum. Jika ada yang berbeda dari LICENSE, LICENSE yang berlaku.",
    ],
    rule: {
      title: "Aturan praktis",
      text: "Apakah ada orang di luar organisasi Anda yang menjalankan instance tersebut — membuat tugas, berbicara dengan agent, atau memicu pekerjaan? Jika ya, lewat antarmuka apa pun (web, Slack, atau API), itu adalah layanan hosted. Jika mereka hanya menerima hasil yang dibuat tim Anda dengan Multica, itu adalah pemakaian internal.",
    },
    scenarios: {
      title: "Skenario umum",
      scenarioColumn: "Skenario",
      licenseColumn: "Lisensi komersial",
      required: "Diperlukan",
      notRequired: "Tidak diperlukan",
      items: [
        {
          scenario: "Organisasi Anda memakai Multica secara internal",
          example: "Self-hosted, di berapa pun jumlah ruang kerja.",
          required: false,
        },
        {
          scenario:
            "Anda men-deploy Multica untuk klien, yang memilikinya dan memakainya secara internal",
          example: "Pekerjaan implementasi, pelatihan, konsultasi, atau kustomisasi.",
          required: false,
        },
        {
          scenario:
            "Tim Anda memakai Multica untuk mengerjakan pekerjaan untuk klien, yang hanya menerima hasil akhirnya",
          example:
            "Sebuah agensi yang menjalankan produksi kontennya di Multica dan mengirimkan hasil jadinya.",
          required: false,
        },
        {
          scenario:
            "Agent hanya mengirim laporan atau notifikasi ke channel Slack klien",
          example:
            "Klien membacanya tetapi tidak pernah berinteraksi dengan instance-nya.",
          required: false,
        },
        {
          scenario:
            "Anda menjalankan dan mengelola instance Multica untuk klien di infrastruktur Anda sendiri",
          example: "Layanan managed, baik Anda membebankan biaya atau tidak.",
          required: true,
        },
        {
          scenario: "Orang di luar organisasi Anda masuk (sign in) ke instance Anda",
          example: "Klien, partner, atau publik mendapatkan akun mereka sendiri.",
          required: true,
        },
        {
          scenario:
            "Orang di luar organisasi Anda menjalankan instance Anda lewat entry point lain",
          example:
            "Website publik yang didukung Multica, integrasi Slack, atau API — bahkan jika gratis.",
          required: true,
        },
        {
          scenario: "Anda menyematkan Multica ke dalam produk yang Anda jual atau distribusikan",
          example: "Multica dikirimkan sebagai komponen dari penawaran komersial lain.",
          required: true,
        },
      ],
    },
    sections: [
      {
        heading: "Syarat lainnya",
        bullets: [
          "Branding: pertahankan logo Multica, nama produk, serta informasi hak cipta dan atribusi yang ditampilkan di antarmuka Multica, kecuali kami telah memberi Anda pengecualian branding secara tertulis.",
          "Atribusi: jika Anda membangun di atas backend, daemon, atau CLI Multica tanpa antarmuka Multica, pertahankan informasi hak cipta dan NOTICE, serta nyatakan di dokumentasi yang menghadap pengguna Anda bahwa produk Anda dibangun di atas Multica, dengan tautan ke [repositori GitHub](https://github.com/multica-ai/multica).",
          "Fork: mempublikasikan kode sumber sebuah fork bukanlah layanan hosted dan tidak memerlukan lisensi komersial. Siapa pun yang menjalankan layanan hosted dari fork tersebut memerlukan lisensinya sendiri.",
          "Lisensi komersial dan pengecualian branding adalah dua pemberian yang terpisah. Satu tidak mencakup yang lain.",
        ],
      },
      {
        heading: "Mendapatkan lisensi komersial",
        paragraphs: [
          "Ceritakan kasus pemakaian Anda lewat [Hubungi Sales](/contact-sales) dan kami akan menghubungi Anda kembali dalam tiga hari kerja. Tidak yakin apakah setup Anda memerlukan lisensi? Tanyakan kepada kami di [Discord](" + discordUrl + ") atau lewat formulir yang sama.",
        ],
      },
    ],
  },

  privacy: {
    title: "Kebijakan Privasi",
    lastUpdated: "Terakhir diperbarui: 24 September 2026",
    intro: [
      "Kebijakan Privasi ini menjelaskan bagaimana Index Labs (Hong Kong) Limited (“Multica”, “kami”) mengumpulkan, memakai, dan membagikan informasi pribadi saat Anda mengunjungi multica.ai, menghubungi kami, atau memakai Multica Cloud, layanan hosted kami, termasuk aplikasi web, desktop, dan mobile.",
      "Kebijakan ini tidak mencakup deployment Multica yang Anda self-host sendiri. Operator deployment self-hosted mengendalikan datanya sendiri, dan provider AI, integrasi, atau analytics apa pun yang dipakainya bergantung pada bagaimana mereka mengonfigurasinya. Satu-satunya hal yang dikirim server self-hosted kepada kami adalah snapshot pemakaian harian: ID acak untuk deployment tersebut, sehingga snapshot dari server yang sama bisa dihubungkan; versi server; perkiraan jumlah ruang kerja, anggota, agent, dan daemon yang terhubung; serta jumlah eksekusi agent yang dimulai, selesai, gagal, dan dibatalkan pada hari itu. Snapshot ini tidak berisi nama, alamat email, atau konten. Mengatur DO_NOT_TRACK=1 mematikan snapshot ini.",
    ],
    sections: [
      {
        heading: "Informasi yang kami kumpulkan",
        bullets: [
          "Informasi akun: nama, alamat email, dan foto profil Anda. Jika Anda masuk dengan Google, kami menerima nama, alamat email, dan foto profil Anda dari Google. Anda juga bisa menambahkan detail profil seperti bahasa, zona waktu, dan bio singkat, serta menjawab pertanyaan onboarding seperti peran Anda, kasus pemakaian Anda, dan dari mana Anda mengetahui Multica.",
          "Konten yang Anda buat: ruang kerja, tugas, komentar, pesan chat, lampiran, instruksi agent, dan apa pun lain yang Anda atau agent Anda masukkan ke Multica Cloud.",
          "Pertanyaan Hubungi Sales: nama, email bisnis, nama dan ukuran perusahaan, negara atau wilayah, kasus pemakaian, tujuan, dan preferensi komunikasi Anda. Untuk mencegah penyalahgunaan, kami juga mencatat alamat IP dan user agent browser dari mana formulir dikirim.",
          "Informasi penagihan: pembayaran langganan ditangani oleh Stripe di halaman yang dihosting oleh Stripe. Kami tidak pernah menerima atau menyimpan detail kartu lengkap Anda.",
          "Informasi pemakaian dan perangkat: versi aplikasi, sistem operasi, jenis client, dan ID instalasi yang dibuat secara acak; nama setiap komputer yang Anda hubungkan sebagai runtime (hostname-nya secara default); serta laporan crash dan error. Sebelum sebuah laporan dikirim, kami menyaring alamat email dan kredensial yang dapat dikenali dari pesan error, tetapi laporan tetap bisa berisi detail lain tentang apa yang salah.",
          "Feedback: saat Anda mengirim feedback, kami menerima pesan Anda beserta halaman, versi aplikasi, sistem operasi, dan detail error apa pun.",
        ],
      },
      {
        heading: "Bagaimana kami memakai informasi",
        bullets: [
          "Untuk menyediakan, mengoperasikan, dan mengamankan Multica Cloud, termasuk memasukkan Anda, menyinkronkan ruang kerja Anda, dan mengirimkan notifikasi serta undangan.",
          "Untuk merespons pertanyaan Hubungi Sales dan permintaan dukungan.",
          "Untuk mengirim pesan layanan seperti kode masuk dan undangan ruang kerja. Kami hanya mengirim update produk atau marketing jika Anda memilih untuk menerimanya (opt-in), dan Anda bisa berhenti berlangganan kapan saja.",
          "Untuk memahami bagaimana Multica dipakai, memperbaiki bug, dan meningkatkan produk.",
          "Untuk mencegah penyalahgunaan dan memenuhi kewajiban hukum kami.",
        ],
      },
      {
        heading: "Dasar hukum",
        paragraphs: [
          "Di tempat yang hukumnya mewajibkan dasar hukum untuk pemrosesan, kami mengandalkan pelaksanaan kontrak kami dengan Anda, untuk menyediakan Multica Cloud; kepentingan sah kami dalam mengamankan, mendukung, dan meningkatkan Multica serta merespons pertanyaan; persetujuan Anda, untuk pesan marketing; dan kepatuhan terhadap kewajiban hukum kami.",
        ],
      },
      {
        heading: "Fitur AI",
        paragraphs: [
          "Coding agent Anda berjalan di komputer Anda sendiri atau di runtime yang Anda hubungkan, memakai alat coding dan akun yang Anda siapkan. Agent yang berjalan di komputer Anda tidak berarti model-nya berjalan di sana: alat-alat tersebut mengirim prompt, kode, file, dan hasil tool ke provider model mereka, sesuai ketentuan alat dan akun yang Anda pakai. Multica mengoordinasikan pekerjaan mereka.",
          "Beberapa fitur Multica Cloud, seperti judul chat dan saran follow-up, mengirim pesan chat pertama Anda atau beberapa pesan terakhir ke provider large language model pihak ketiga yang kami pilih, untuk menghasilkan hasilnya. Multica tidak memakai konten Anda untuk melatih model AI.",
        ],
      },
      {
        heading: "Cookie dan analytics",
        paragraphs: [
          "Kami memakai cookie yang diperlukan untuk menjaga Anda tetap masuk, melindungi dari cross-site request forgery, dan memberi Anda akses ke file yang Anda unggah. Kami juga memakai cookie yang mengingat kampanye atau website mana yang merujuk Anda, hingga 30 hari, serta cookie yang mengingat bahasa Anda dan ruang kerja terakhir yang Anda buka.",
          "Kami memakai PostHog untuk memahami pemakaian produk dan mengumpulkan laporan crash. Saat Anda masuk, PostHog menerima nama akun dan email Anda agar kami bisa mencocokkan laporan dengan akun Anda. Kami tidak memakai cookie iklan, dan kami tidak menjual informasi pribadi Anda.",
        ],
      },
      {
        heading: "Dengan siapa kami membagikan informasi",
        paragraphs: [
          "Informasi yang Anda masukkan ke sebuah ruang kerja terlihat oleh anggota dan admin lainnya, serta oleh agent dan integrasi yang mereka izinkan, sesuai izin ruang kerja tersebut. Jika ruang kerja Anda milik sebuah organisasi, organisasi tersebut mengelola kontennya dan dapat menangani permintaan terkait itu.",
          "Kami juga mengungkapkan informasi saat hukum mewajibkannya, dan kepada pembeli atau penerus jika Multica terlibat dalam merger, akuisisi, atau penjualan aset.",
          "Selain itu, kami hanya membagikan informasi pribadi dengan penyedia layanan yang membantu kami menjalankan Multica dan dengan integrasi yang Anda pilih untuk dihubungkan:",
        ],
        bullets: [
          "Amazon Web Services: hosting, penyimpanan file, dan content delivery",
          "Vercel: hosting untuk website dan aplikasi web",
          "Stripe: pembayaran dan penagihan",
          "Resend: email masuk dan undangan",
          "PostHog: analytics produk dan laporan crash",
          "Google: sign-in, jika Anda memilih Sign in with Google",
          "Provider large language model: fitur AI yang dijelaskan di atas",
          "Integrasi yang Anda hubungkan, seperti Slack, Lark, DingTalk, WeCom, Telegram, GitHub, GitLab, atau aplikasi yang terhubung lewat Composio: data yang Anda pilih untuk dipertukarkan dengan mereka, yang juga tunduk pada ketentuan mereka sendiri",
        ],
      },
      {
        heading: "Di mana informasi disimpan",
        paragraphs: [
          "Multica Cloud dihosting di Amazon Web Services dan Vercel. Kami dan penyedia layanan kami dapat memproses informasi Anda di Amerika Serikat dan negara lain. Di mana pun diproses, kami melindunginya seperti yang dijelaskan dalam kebijakan ini.",
        ],
      },
      {
        heading: "Berapa lama kami menyimpan informasi",
        paragraphs: [
          "Kami menyimpan informasi akun dan konten ruang kerja selama akun atau ruang kerja Anda masih ada. Saat pemilik ruang kerja menghapus sebuah ruang kerja, tugas, komentar, dan konten lainnya dihapus dari Multica Cloud, meskipun backup yang kami simpan untuk pemulihan mungkin masih berisi salinannya untuk beberapa waktu setelahnya. Untuk menghapus file yang diunggah ke ruang kerja yang dihapus dari penyimpanan file kami, kirim email ke [support@multica.ai](mailto:support@multica.ai). Kami menyimpan catatan penagihan selama diwajibkan oleh aturan akuntansi dan pajak, serta analytics produk, laporan crash, pertanyaan Hubungi Sales, dan feedback selama itu berguna untuk mendukung Anda dan meningkatkan Multica. Kami menghapus pertanyaan dan feedback atas permintaan.",
        ],
      },
      {
        heading: "Pilihan dan hak Anda",
        paragraphs: [
          "Bergantung pada tempat Anda tinggal, Anda mungkin memiliki hak untuk mengakses, mengoreksi, menghapus, atau mengekspor informasi pribadi Anda; untuk menolak atau membatasi pemrosesan tertentu; untuk menarik persetujuan yang telah Anda berikan, seperti untuk pesan marketing; dan untuk mengadu ke otoritas perlindungan data setempat. Anda bisa memperbarui profil Anda di Multica kapan saja dan menghapus ruang kerja yang Anda miliki dari pengaturannya. Untuk hal lain, termasuk menghapus akun Anda, kirim email ke [support@multica.ai](mailto:support@multica.ai). Kami akan merespons dalam 30 hari.",
        ],
      },
      {
        heading: "Keamanan",
        paragraphs: [
          "Kami melindungi informasi Anda dengan enkripsi saat transit, kontrol akses, dan penyimpanan terenkripsi untuk kredensial integrasi. Tidak ada sistem yang sempurna aman, jadi segera hubungi kami jika Anda yakin akun Anda telah disusupi.",
        ],
      },
      {
        heading: "Anak-anak",
        paragraphs: [
          "Multica tidak ditujukan untuk anak di bawah 16 tahun, dan kami tidak dengan sengaja mengumpulkan informasi pribadi mereka.",
        ],
      },
      {
        heading: "Perubahan pada kebijakan ini",
        paragraphs: [
          "Kami dapat memperbarui kebijakan ini dari waktu ke waktu. Kami akan mempublikasikan versi baru di halaman ini dan memperbarui tanggal di bagian atas. Jika perubahannya signifikan, kami akan memberi tahu Anda sebelum berlaku.",
        ],
      },
      {
        heading: "Hubungi kami",
        paragraphs: [
          "Multica dioperasikan oleh Index Labs (Hong Kong) Limited, yang bertanggung jawab atas informasi pribadi Anda. Untuk pertanyaan atau permintaan privasi, kirim email ke [support@multica.ai](mailto:support@multica.ai).",
        ],
      },
    ],
  },

  changelog: createEnDict(allowSignup, docsHref).changelog,

  download: {
    hero: {
      macArm64: {
        title: "Multica untuk macOS",
        sub: "Apple Silicon · daemon terpasang, tanpa setup",
        primary: "Unduh (.dmg)",
        altZip: "atau unduh .zip",
      },
      macIntel: {
        title: "Multica untuk macOS",
        sub: "Intel · daemon terpasang, tanpa setup",
        primary: "Unduh (.dmg)",
        altZip: "atau unduh .zip",
      },
      winX64: {
        title: "Multica untuk Windows",
        sub: "Daemon terpasang, tanpa setup",
        primary: "Unduh (.exe)",
      },
      winArm64: {
        title: "Multica untuk Windows",
        sub: "ARM · daemon terpasang, tanpa setup",
        primary: "Unduh (.exe)",
      },
      linux: {
        title: "Multica untuk Linux",
        sub: "Daemon terpasang, tanpa setup",
        primary: "Unduh AppImage",
        altFormats: "atau .deb / .rpm",
      },
      unknown: {
        title: "Pilih platform Anda",
        sub: "Semua installer tercantum di bawah.",
      },
      safariMacHint: "Memakai Mac Intel? Pilih unduhan Intel di bawah.",
      archFallbackHint: "Arsitektur salah? Lihat semua format di bawah.",
    },
    allPlatforms: {
      title: "Semua platform",
      macArm64Label: "macOS · Apple Silicon",
      macX64Label: "macOS · Intel",
      winX64Label: "Windows · x64",
      winArm64Label: "Windows · ARM64",
      linuxX64Label: "Linux · x64",
      linuxArm64Label: "Linux · ARM64",
      formatDmg: ".dmg",
      formatZip: ".zip",
      formatExe: ".exe",
      formatAppImage: ".AppImage",
      formatDeb: ".deb",
      formatRpm: ".rpm",
      unavailable: "Tidak tersedia",
    },
    cli: {
      title: "Lebih suka CLI?",
      sub: "Untuk server, mesin dev remote, dan setup headless. Daemon yang sama dengan Desktop, diinstal lewat terminal.",
      installLabel: "Instal",
      platformGroup: "Pilih platform Anda",
      platformMacosLinux: "macOS / Linux",
      platformWindows: "Windows",
      startLabel: "Mulai daemon",
      sshNote: "Sudah punya server? Perintah yang sama berfungsi lewat SSH.",
      copyLabel: "Salin",
      copiedLabel: "Disalin",
    },
    cloud: {
      title: "Runtime cloud (waitlist)",
      sub: "Kami akan hosting runtime-nya untuk Anda. Belum live — tinggalkan email Anda untuk diberi tahu.",
    },
    footer: {
      releaseNotes: "Yang baru di {version}",
      allReleases: "Lihat semua rilis",
      currentVersion: "Versi saat ini: {version}",
      versionUnavailable: "Versi tidak tersedia — cek GitHub",
    },
  },
  contactSales: {
    pageTitle: "Hubungi Sales",
    pageDescription:
      "Bicara dengan tim Multica tentang menerapkan workflow manusia + agent di perusahaan Anda.",
    eyebrow: "Hubungi Sales",
    title: "Mari kami pahami kebutuhan Anda",
    fields: {
      firstName: "Nama depan",
      lastName: "Nama belakang",
      businessEmail: "Email bisnis",
      businessEmailHint:
        "Gunakan email perusahaan. Gmail, Outlook, dan penyedia email pribadi lainnya tidak diterima.",
      companyName: "Nama perusahaan",
      companySize: "Ukuran perusahaan",
      countryRegion: "Negara / Wilayah",
      useCase: "Bagaimana rencana Anda memakai atau berkolaborasi dengan Multica?",
      goals: "Tujuan atau tantangan Anda",
      selectPlaceholder: "Silakan pilih",
      submit: "Kirim",
      submitting: "Mengirim…",
    },
    companySizes: [
      { value: "1-10", label: "1 – 10 karyawan" },
      { value: "11-50", label: "11 – 50 karyawan" },
      { value: "51-200", label: "51 – 200 karyawan" },
      { value: "201-500", label: "201 – 500 karyawan" },
      { value: "501-1000", label: "501 – 1.000 karyawan" },
      { value: "1000+", label: "1.000+ karyawan" },
    ],
    useCases: [
      { value: "evaluate", label: "Mengevaluasi Multica untuk tim saya" },
      { value: "adopt_team", label: "Menerapkan Multica ke sebuah tim atau perusahaan" },
      { value: "self_host", label: "Self-hosting di infrastruktur kami sendiri" },
      { value: "integrate", label: "Mengintegrasikan Multica dengan tools yang sudah ada" },
      { value: "partner", label: "Pertanyaan kemitraan atau reseller" },
      { value: "other", label: "Hal lainnya" },
    ],
    countries: [
      "Indonesia",
      "Singapura",
      "Malaysia",
      "Thailand",
      "Vietnam",
      "Filipina",
      "Jepang",
      "Korea Selatan",
      "Tiongkok (Daratan)",
      "Hong Kong SAR",
      "Taiwan",
      "India",
      "Australia",
      "Selandia Baru",
      "Uni Emirat Arab",
      "Arab Saudi",
      "Israel",
      "Turki",
      "Amerika Serikat",
      "Kanada",
      "Inggris Raya",
      "Jerman",
      "Prancis",
      "Belanda",
      "Swedia",
      "Swiss",
      "Spanyol",
      "Italia",
      "Irlandia",
      "Norwegia",
      "Denmark",
      "Finlandia",
      "Belgia",
      "Portugal",
      "Afrika Selatan",
      "Brasil",
      "Meksiko",
      "Argentina",
      "Chili",
      "Lainnya",
    ],
    consent: {
      intro:
        "Multica menghargai privasi Anda. Kami hanya akan memakai informasi pribadi Anda untuk mengelola akun Anda dan mengirimkan produk atau layanan yang Anda minta. Sesekali, kami ingin berbagi update produk, praktik terbaik, dan insight yang mungkin relevan bagi Anda. Beri tahu kami di bawah jika Anda ingin mendengar kabar dari kami.",
      outreach:
        "Saya ingin menerima komunikasi satu-ke-satu dari Multica, termasuk update layanan, pertanyaan dukungan, dan tindak lanjut terkait bisnis.",
      updates:
        "Saya ingin menerima update produk, insight, dan undangan acara dari Multica.",
      unsubscribe:
        "Anda bisa berhenti berlangganan komunikasi kami kapan saja. Untuk detail lebih lanjut tentang cara kami menangani data dan hak privasi Anda, silakan baca",
      submitConsent:
        "Dengan mengklik “Kirim,” Anda menyetujui Multica untuk menyimpan dan memproses informasi Anda demi mengirimkan konten yang diminta.",
      privacyLinkLabel: "Kebijakan Privasi.",
      privacyLinkHref: "/privacy",
    },
    success: {
      title: "Terima kasih — sudah kami terima.",
      message:
        "Anggota tim Multica akan merespons dalam tiga hari kerja. Sementara itu, jangan ragu untuk menjelajahi dokumentasi atau beri kami star di GitHub.",
      cta: "Kembali ke beranda",
    },
    errors: {
      generic: "Ada yang salah — silakan coba lagi sesaat lagi.",
      rateLimit:
        "Kami telah menerima beberapa pertanyaan dari alamat ini baru-baru ini. Silakan coba lagi sebentar lagi.",
      freeEmail:
        "Silakan gunakan alamat email bisnis — penyedia gratis (gmail, outlook, dll.) tidak diterima.",
      invalidEmail: "Itu bukan alamat email yang valid.",
    },
  },
  };
}
