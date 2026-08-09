import type { SupportedLocale } from "@multica/core/i18n";

export interface CelestialWorkspaceName {
  slugBase: string;
  names: Record<SupportedLocale, string>;
}

export const CELESTIAL_WORKSPACE_NAMES = [
  {
    slugBase: "alpha-centauri",
    names: {
      en: "Alpha Centauri",
      "zh-Hans": "南门二",
      ja: "アルファ・ケンタウリ",
      ko: "알파 센타우리",
    },
  },
  {
    slugBase: "andromeda",
    names: {
      en: "Andromeda",
      "zh-Hans": "仙女座星系",
      ja: "アンドロメダ銀河",
      ko: "안드로메다 은하",
    },
  },
  {
    slugBase: "antares",
    names: {
      en: "Antares",
      "zh-Hans": "心宿二",
      ja: "アンタレス",
      ko: "안타레스",
    },
  },
  {
    slugBase: "ariel",
    names: {
      en: "Ariel",
      "zh-Hans": "天卫一",
      ja: "アリエル",
      ko: "아리엘",
    },
  },
  {
    slugBase: "betelgeuse",
    names: {
      en: "Betelgeuse",
      "zh-Hans": "参宿四",
      ja: "ベテルギウス",
      ko: "베텔게우스",
    },
  },
  {
    slugBase: "callisto",
    names: {
      en: "Callisto",
      "zh-Hans": "木卫四",
      ja: "カリスト",
      ko: "칼리스토",
    },
  },
  {
    slugBase: "capella",
    names: {
      en: "Capella",
      "zh-Hans": "五车二",
      ja: "カペラ",
      ko: "카펠라",
    },
  },
  {
    slugBase: "ceres",
    names: {
      en: "Ceres",
      "zh-Hans": "谷神星",
      ja: "ケレス",
      ko: "세레스",
    },
  },
  {
    slugBase: "deimos",
    names: {
      en: "Deimos",
      "zh-Hans": "火卫二",
      ja: "ダイモス",
      ko: "데이모스",
    },
  },
  {
    slugBase: "deneb",
    names: {
      en: "Deneb",
      "zh-Hans": "天津四",
      ja: "デネブ",
      ko: "데네브",
    },
  },
  {
    slugBase: "dione",
    names: {
      en: "Dione",
      "zh-Hans": "土卫四",
      ja: "ディオネ",
      ko: "디오네",
    },
  },
  {
    slugBase: "enceladus",
    names: {
      en: "Enceladus",
      "zh-Hans": "土卫二",
      ja: "エンケラドゥス",
      ko: "엔셀라두스",
    },
  },
  {
    slugBase: "eris",
    names: {
      en: "Eris",
      "zh-Hans": "阋神星",
      ja: "エリス",
      ko: "에리스",
    },
  },
  {
    slugBase: "europa",
    names: {
      en: "Europa",
      "zh-Hans": "木卫二",
      ja: "エウロパ",
      ko: "유로파",
    },
  },
  {
    slugBase: "ganymede",
    names: {
      en: "Ganymede",
      "zh-Hans": "木卫三",
      ja: "ガニメデ",
      ko: "가니메데",
    },
  },
  {
    slugBase: "halley",
    names: {
      en: "Halley",
      "zh-Hans": "哈雷彗星",
      ja: "ハレー彗星",
      ko: "핼리 혜성",
    },
  },
  {
    slugBase: "hyperion",
    names: {
      en: "Hyperion",
      "zh-Hans": "土卫七",
      ja: "ヒペリオン",
      ko: "히페리온",
    },
  },
  {
    slugBase: "io",
    names: {
      en: "Io",
      "zh-Hans": "木卫一",
      ja: "イオ",
      ko: "이오",
    },
  },
  {
    slugBase: "mars",
    names: {
      en: "Mars",
      "zh-Hans": "火星",
      ja: "火星",
      ko: "화성",
    },
  },
  {
    slugBase: "mercury",
    names: {
      en: "Mercury",
      "zh-Hans": "水星",
      ja: "水星",
      ko: "수성",
    },
  },
  {
    slugBase: "mimas",
    names: {
      en: "Mimas",
      "zh-Hans": "土卫一",
      ja: "ミマス",
      ko: "미마스",
    },
  },
  {
    slugBase: "miranda",
    names: {
      en: "Miranda",
      "zh-Hans": "天卫五",
      ja: "ミランダ",
      ko: "미란다",
    },
  },
  {
    slugBase: "neptune",
    names: {
      en: "Neptune",
      "zh-Hans": "海王星",
      ja: "海王星",
      ko: "해왕성",
    },
  },
  {
    slugBase: "oberon",
    names: {
      en: "Oberon",
      "zh-Hans": "天卫四",
      ja: "オベロン",
      ko: "오베론",
    },
  },
  {
    slugBase: "orion-nebula",
    names: {
      en: "Orion Nebula",
      "zh-Hans": "猎户座星云",
      ja: "オリオン大星雲",
      ko: "오리온 성운",
    },
  },
  {
    slugBase: "phobos",
    names: {
      en: "Phobos",
      "zh-Hans": "火卫一",
      ja: "フォボス",
      ko: "포보스",
    },
  },
  {
    slugBase: "pluto",
    names: {
      en: "Pluto",
      "zh-Hans": "冥王星",
      ja: "冥王星",
      ko: "명왕성",
    },
  },
  {
    slugBase: "polaris",
    names: {
      en: "Polaris",
      "zh-Hans": "北极星",
      ja: "北極星",
      ko: "북극성",
    },
  },
  {
    slugBase: "proxima-centauri",
    names: {
      en: "Proxima Centauri",
      "zh-Hans": "比邻星",
      ja: "プロキシマ・ケンタウリ",
      ko: "프록시마 센타우리",
    },
  },
  {
    slugBase: "rhea",
    names: {
      en: "Rhea",
      "zh-Hans": "土卫五",
      ja: "レア",
      ko: "레아",
    },
  },
  {
    slugBase: "rigel",
    names: {
      en: "Rigel",
      "zh-Hans": "参宿七",
      ja: "リゲル",
      ko: "리겔",
    },
  },
  {
    slugBase: "saturn",
    names: {
      en: "Saturn",
      "zh-Hans": "土星",
      ja: "土星",
      ko: "토성",
    },
  },
  {
    slugBase: "sirius",
    names: {
      en: "Sirius",
      "zh-Hans": "天狼星",
      ja: "シリウス",
      ko: "시리우스",
    },
  },
  {
    slugBase: "sombrero-galaxy",
    names: {
      en: "Sombrero Galaxy",
      "zh-Hans": "草帽星系",
      ja: "ソンブレロ銀河",
      ko: "솜브레로 은하",
    },
  },
  {
    slugBase: "titan",
    names: {
      en: "Titan",
      "zh-Hans": "土卫六",
      ja: "タイタン",
      ko: "타이탄",
    },
  },
  {
    slugBase: "titania",
    names: {
      en: "Titania",
      "zh-Hans": "天卫三",
      ja: "チタニア",
      ko: "티타니아",
    },
  },
  {
    slugBase: "triton",
    names: {
      en: "Triton",
      "zh-Hans": "海卫一",
      ja: "トリトン",
      ko: "트리톤",
    },
  },
  {
    slugBase: "vega",
    names: {
      en: "Vega",
      "zh-Hans": "织女星",
      ja: "ベガ",
      ko: "베가",
    },
  },
  {
    slugBase: "venus",
    names: {
      en: "Venus",
      "zh-Hans": "金星",
      ja: "金星",
      ko: "금성",
    },
  },
  {
    slugBase: "vesta",
    names: {
      en: "Vesta",
      "zh-Hans": "灶神星",
      ja: "ベスタ",
      ko: "베스타",
    },
  },
  {
    slugBase: "achernar",
    names: {
      en: "Achernar",
      "zh-Hans": "水委一",
      ja: "アケルナル",
      ko: "아케르나르",
    },
  },
  {
    slugBase: "acrux",
    names: {
      en: "Acrux",
      "zh-Hans": "十字架二",
      ja: "アクルックス",
      ko: "아크룩스",
    },
  },
  {
    slugBase: "adhara",
    names: {
      en: "Adhara",
      "zh-Hans": "弧矢七",
      ja: "アダラ",
      ko: "아다라",
    },
  },
  {
    slugBase: "adrastea",
    names: {
      en: "Adrastea",
      "zh-Hans": "木卫十五",
      ja: "アドラステア",
      ko: "아드라스테아",
    },
  },
  {
    slugBase: "alcyone",
    names: {
      en: "Alcyone",
      "zh-Hans": "昴宿六",
      ja: "アルキオネ",
      ko: "알키오네",
    },
  },
  {
    slugBase: "aldebaran",
    names: {
      en: "Aldebaran",
      "zh-Hans": "毕宿五",
      ja: "アルデバラン",
      ko: "알데바란",
    },
  },
  {
    slugBase: "algol",
    names: {
      en: "Algol",
      "zh-Hans": "大陵五",
      ja: "アルゴル",
      ko: "알골",
    },
  },
  {
    slugBase: "alhena",
    names: {
      en: "Alhena",
      "zh-Hans": "井宿三",
      ja: "アルヘナ",
      ko: "알헤나",
    },
  },
  {
    slugBase: "alnair",
    names: {
      en: "Alnair",
      "zh-Hans": "鹤一",
      ja: "アルナイル",
      ko: "알나이르",
    },
  },
  {
    slugBase: "alnilam",
    names: {
      en: "Alnilam",
      "zh-Hans": "参宿二",
      ja: "アルニラム",
      ko: "알닐람",
    },
  },
  {
    slugBase: "alnitak",
    names: {
      en: "Alnitak",
      "zh-Hans": "参宿一",
      ja: "アルニタク",
      ko: "알니탁",
    },
  },
  {
    slugBase: "altair",
    names: {
      en: "Altair",
      "zh-Hans": "牛郎星",
      ja: "アルタイル",
      ko: "알타이르",
    },
  },
  {
    slugBase: "amalthea",
    names: {
      en: "Amalthea",
      "zh-Hans": "木卫五",
      ja: "アマルテア",
      ko: "아말테아",
    },
  },
  {
    slugBase: "ananke",
    names: {
      en: "Ananke",
      "zh-Hans": "木卫十二",
      ja: "アナンケ",
      ko: "아난케",
    },
  },
  {
    slugBase: "arcturus",
    names: {
      en: "Arcturus",
      "zh-Hans": "大角星",
      ja: "アルクトゥルス",
      ko: "아르크투루스",
    },
  },
  {
    slugBase: "bellatrix",
    names: {
      en: "Bellatrix",
      "zh-Hans": "参宿五",
      ja: "ベラトリックス",
      ko: "벨라트릭스",
    },
  },
  {
    slugBase: "bianca",
    names: {
      en: "Bianca",
      "zh-Hans": "天卫八",
      ja: "ビアンカ",
      ko: "비앙카",
    },
  },
  {
    slugBase: "canopus",
    names: {
      en: "Canopus",
      "zh-Hans": "老人星",
      ja: "カノープス",
      ko: "카노푸스",
    },
  },
  {
    slugBase: "carme",
    names: {
      en: "Carme",
      "zh-Hans": "木卫十一",
      ja: "カルメ",
      ko: "카르메",
    },
  },
  {
    slugBase: "cartwheel-galaxy",
    names: {
      en: "Cartwheel Galaxy",
      "zh-Hans": "车轮星系",
      ja: "カートホイール銀河",
      ko: "수레바퀴 은하",
    },
  },
  {
    slugBase: "castor",
    names: {
      en: "Castor",
      "zh-Hans": "北河二",
      ja: "カストル",
      ko: "카스토르",
    },
  },
  {
    slugBase: "charon",
    names: {
      en: "Charon",
      "zh-Hans": "冥卫一",
      ja: "カロン",
      ko: "카론",
    },
  },
  {
    slugBase: "cordelia",
    names: {
      en: "Cordelia",
      "zh-Hans": "天卫六",
      ja: "コーディリア",
      ko: "코델리아",
    },
  },
  {
    slugBase: "crab-nebula",
    names: {
      en: "Crab Nebula",
      "zh-Hans": "蟹状星云",
      ja: "かに星雲",
      ko: "게 성운",
    },
  },
  {
    slugBase: "cygnus-x-1",
    names: {
      en: "Cygnus X-1",
      "zh-Hans": "天鹅座 X-1",
      ja: "はくちょう座X-1",
      ko: "백조자리 X-1",
    },
  },
  {
    slugBase: "despina",
    names: {
      en: "Despina",
      "zh-Hans": "海卫五",
      ja: "デスピナ",
      ko: "데스피나",
    },
  },
  {
    slugBase: "elara",
    names: {
      en: "Elara",
      "zh-Hans": "木卫七",
      ja: "エララ",
      ko: "엘라라",
    },
  },
  {
    slugBase: "electra",
    names: {
      en: "Electra",
      "zh-Hans": "昴宿一",
      ja: "エレクトラ",
      ko: "엘렉트라",
    },
  },
  {
    slugBase: "fomalhaut",
    names: {
      en: "Fomalhaut",
      "zh-Hans": "北落师门",
      ja: "フォーマルハウト",
      ko: "포말하우트",
    },
  },
  {
    slugBase: "haumea",
    names: {
      en: "Haumea",
      "zh-Hans": "妊神星",
      ja: "ハウメア",
      ko: "하우메아",
    },
  },
  {
    slugBase: "helene",
    names: {
      en: "Helene",
      "zh-Hans": "土卫十二",
      ja: "ヘレネ",
      ko: "헬레네",
    },
  },
  {
    slugBase: "iapetus",
    names: {
      en: "Iapetus",
      "zh-Hans": "土卫八",
      ja: "イアペトゥス",
      ko: "이아페투스",
    },
  },
  {
    slugBase: "janus",
    names: {
      en: "Janus",
      "zh-Hans": "土卫十",
      ja: "ヤヌス",
      ko: "야누스",
    },
  },
  {
    slugBase: "juliet",
    names: {
      en: "Juliet",
      "zh-Hans": "天卫十一",
      ja: "ジュリエット",
      ko: "줄리엣",
    },
  },
  {
    slugBase: "larissa",
    names: {
      en: "Larissa",
      "zh-Hans": "海卫七",
      ja: "ラリッサ",
      ko: "라리사",
    },
  },
  {
    slugBase: "leda",
    names: {
      en: "Leda",
      "zh-Hans": "木卫十三",
      ja: "レダ",
      ko: "레다",
    },
  },
  {
    slugBase: "makemake",
    names: {
      en: "Makemake",
      "zh-Hans": "鸟神星",
      ja: "マケマケ",
      ko: "마케마케",
    },
  },
  {
    slugBase: "merope",
    names: {
      en: "Merope",
      "zh-Hans": "昴宿五",
      ja: "メローペ",
      ko: "메로페",
    },
  },
  {
    slugBase: "metis",
    names: {
      en: "Metis",
      "zh-Hans": "木卫十六",
      ja: "メティス",
      ko: "메티스",
    },
  },
  {
    slugBase: "mintaka",
    names: {
      en: "Mintaka",
      "zh-Hans": "参宿三",
      ja: "ミンタカ",
      ko: "민타카",
    },
  },
  {
    slugBase: "naiad",
    names: {
      en: "Naiad",
      "zh-Hans": "海卫三",
      ja: "ナイアド",
      ko: "나이아드",
    },
  },
  {
    slugBase: "nereid",
    names: {
      en: "Nereid",
      "zh-Hans": "海卫二",
      ja: "ネレイド",
      ko: "네레이드",
    },
  },
  {
    slugBase: "ophelia",
    names: {
      en: "Ophelia",
      "zh-Hans": "天卫七",
      ja: "オフィーリア",
      ko: "오필리아",
    },
  },
  {
    slugBase: "pan",
    names: {
      en: "Pan",
      "zh-Hans": "土卫十八",
      ja: "パン",
      ko: "판",
    },
  },
  {
    slugBase: "pandora",
    names: {
      en: "Pandora",
      "zh-Hans": "土卫十七",
      ja: "パンドラ",
      ko: "판도라",
    },
  },
  {
    slugBase: "pasiphae",
    names: {
      en: "Pasiphae",
      "zh-Hans": "木卫八",
      ja: "パシファエ",
      ko: "파시파에",
    },
  },
  {
    slugBase: "phoebe",
    names: {
      en: "Phoebe",
      "zh-Hans": "土卫九",
      ja: "フェーベ",
      ko: "포에베",
    },
  },
  {
    slugBase: "pinwheel-galaxy",
    names: {
      en: "Pinwheel Galaxy",
      "zh-Hans": "风车星系",
      ja: "回転花火銀河",
      ko: "바람개비 은하",
    },
  },
  {
    slugBase: "pollux",
    names: {
      en: "Pollux",
      "zh-Hans": "北河三",
      ja: "ポルックス",
      ko: "폴룩스",
    },
  },
  {
    slugBase: "portia",
    names: {
      en: "Portia",
      "zh-Hans": "天卫十二",
      ja: "ポーシャ",
      ko: "포샤",
    },
  },
  {
    slugBase: "proteus",
    names: {
      en: "Proteus",
      "zh-Hans": "海卫八",
      ja: "プロテウス",
      ko: "프로테우스",
    },
  },
  {
    slugBase: "puck",
    names: {
      en: "Puck",
      "zh-Hans": "天卫十五",
      ja: "パック",
      ko: "퍽",
    },
  },
  {
    slugBase: "regulus",
    names: {
      en: "Regulus",
      "zh-Hans": "轩辕十四",
      ja: "レグルス",
      ko: "레굴루스",
    },
  },
  {
    slugBase: "rosalind",
    names: {
      en: "Rosalind",
      "zh-Hans": "天卫十三",
      ja: "ロザリンド",
      ko: "로잘린드",
    },
  },
  {
    slugBase: "spica",
    names: {
      en: "Spica",
      "zh-Hans": "角宿一",
      ja: "スピカ",
      ko: "스피카",
    },
  },
  {
    slugBase: "sycorax",
    names: {
      en: "Sycorax",
      "zh-Hans": "天卫十七",
      ja: "シコラクス",
      ko: "시코락스",
    },
  },
  {
    slugBase: "telesto",
    names: {
      en: "Telesto",
      "zh-Hans": "土卫十三",
      ja: "テレスト",
      ko: "텔레스토",
    },
  },
  {
    slugBase: "thebe",
    names: {
      en: "Thebe",
      "zh-Hans": "木卫十四",
      ja: "テーベ",
      ko: "테베",
    },
  },
  {
    slugBase: "umbriel",
    names: {
      en: "Umbriel",
      "zh-Hans": "天卫二",
      ja: "ウンブリエル",
      ko: "움브리엘",
    },
  },
  {
    slugBase: "whirlpool-galaxy",
    names: {
      en: "Whirlpool Galaxy",
      "zh-Hans": "涡状星系",
      ja: "子持ち銀河",
      ko: "소용돌이 은하",
    },
  },
] as const satisfies readonly CelestialWorkspaceName[];
