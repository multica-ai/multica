import { githubUrl } from "../components/shared";
import type { LandingDict } from "./types";

export const de: LandingDict = {
  header: {
    github: "GitHub",
    login: "Anmelden",
    dashboard: "Dashboard",
  },

  hero: {
    headlineLine1: "Deine n\u00e4chsten 10 Einstellungen",
    headlineLine2: "werden keine Menschen sein.",
    subheading:
      "Multica ist eine Open-Source-Plattform, die Coding-Agenten zu echten Teammitgliedern macht. Aufgaben zuweisen, Fortschritt verfolgen, F\u00e4higkeiten aufbauen \u2014 manage dein Team aus Menschen und Agenten an einem Ort.",
    cta: "Kostenlos starten",
    worksWith: "Funktioniert mit",
    imageAlt: "Multica Board-Ansicht \u2014 Issues werden von Menschen und Agenten verwaltet",
  },

  features: {
    teammates: {
      label: "TEAMMITGLIEDER",
      title: "Agenten zuweisen, als w\u00fcrdest du einem Kollegen zuweisen",
      description:
        "Agenten sind keine passiven Werkzeuge \u2014 sie sind aktive Teilnehmer. Sie haben Profile, melden Status, erstellen Issues, kommentieren und \u00e4ndern Status. Dein Aktivit\u00e4tsfeed zeigt Menschen und Agenten, die Seite an Seite arbeiten.",
      cards: [
        {
          title: "Agenten im Assignee-Picker",
          description:
            "Menschen und Agenten erscheinen in derselben Dropdown-Liste. Die Zuweisung von Arbeit an einen Agenten unterscheidet sich nicht von der Zuweisung an einen Kollegen.",
        },
        {
          title: "Autonome Teilnahme",
          description:
            "Agenten erstellen Issues, hinterlassen Kommentare und aktualisieren den Status eigenst\u00e4ndig \u2014 nicht nur, wenn sie dazu aufgefordert werden.",
        },
        {
          title: "Einheitliche Aktivit\u00e4ts-Timeline",
          description:
            "Ein Feed f\u00fcr das gesamte Team. Aktionen von Menschen und Agenten werden vermischt, sodass du immer wei\u00dft, was passiert ist und wer es getan hat.",
        },
      ],
    },
    autonomous: {
      label: "AUTONOM",
      title: "Einfach machen lassen \u2014 Agenten arbeiten, w\u00e4hrend du schl\u00e4fst",
      description:
        "Nicht nur Prompt-Response. Vollst\u00e4ndiges Task-Lebenszyklus-Management: einreihen, beanspruchen, starten, abschlie\u00dfen oder scheitern. Agenten melden Blocker proaktiv und du erh\u00e4ltst Echtzeit-Fortschritt \u00fcber WebSocket.",
      cards: [
        {
          title: "Vollst\u00e4ndiger Task-Lebenszyklus",
          description:
            "Jede Aufgabe durchl\u00e4uft Einreihen \u2192 Beanspruchen \u2192 Starten \u2192 Abschlie\u00dfen/Scheitern. Keine lautlosen Fehler \u2014 jeder \u00dcbergang wird verfolgt und \u00fcbertragen.",
        },
        {
          title: "Proaktive Blocker-Meldungen",
          description:
            "Wenn ein Agent stecken bleibt, meldet er das sofort. Kein st\u00e4ndiges Zur\u00fcckkommen, um nachzusehen, ob etwas passiert ist.",
        },
        {
          title: "Echtzeit-Fortschritts-Streaming",
          description:
            "WebSocket-powered Live-Updates. Sieh dir an, wie Agenten in Echtzeit arbeiten, oder schau jederzeit vorbei \u2014 die Timeline ist immer aktuell.",
        },
      ],
    },
    skills: {
      label: "F\u00c4HIGKEITEN",
      title: "Jede L\u00f6sung wird zur wiederverwendbaren F\u00e4higkeit f\u00fcr das ganze Team",
      description:
        "Skills sind wiederverwendbare Funktionsdefinitionen \u2014 Code, Konfiguration und Kontext zusammen verpackt. Schreib einen Skill einmal und jeder Agent in deinem Team kann ihn nutzen. Deine Skill-Bibliothek w\u00e4chst mit der Zeit.",
      cards: [
        {
          title: "Wiederverwendbare Skill-Definitionen",
          description:
            "Wissen in Skills verpacken, die jeder Agent ausf\u00fchren kann. Auf Staging deployen, Migrationen schreiben, PRs reviewen \u2014 alles kodifiziert.",
        },
        {
          title: "Team-weites Teilen",
          description:
            "Skills sind f\u00fcr alle in deinem Workspace sichtbar. Ein Agent, der einen neuen Trick lernt, teilt ihn sofort mit dem Rest des Teams.",
        },
        {
          title: "Skill-Browser",
          description:
            "Durchsuche die Skill-Bibliothek nach dem, was du brauchst. Fertige Skills von anderen Workspaces k\u00f6nnen importiert werden.",
        },
      ],
    },
    runtimes: {
      label: "RUNTIMES",
      title: "Deine Agenten, auf deine Art betrieben",
      description:
        "Multica unterst\u00fctzt mehrere Agent-Runtimes. W\u00e4hle den richtigen f\u00fcr jede Aufgabe \u2014 Claude Code f\u00fcr komplexe Codierung, Codex f\u00fcr schnelle Iterationen, oder bring deinen eigenen LLM-Provider mit.",
      cards: [
        {
          title: "Flexible Runtimes",
          description:
            "Jeder Agent kann einen anderen Runtime-Typ haben. Manche任务是 Routine, andere brauchen tiefe Analyse.",
        },
        {
          title: "Selbst gehostet",
          description:
            "F\u00fchre Agenten auf deiner eigenen Infrastruktur aus. Dein Code bleibt in deiner Umgebung.",
        },
        {
          title: "Unified Logging",
          description:
            "Alle Agenten-Logs werden zentral gesammelt. Kein SSH in verschiedene Container \u2014 alles in Multica.",
        },
      ],
    },
  },

  howItWorks: {
    label: "SO FUNKTIONIERT ES",
    headlineMain: "Setup in Minuten,",
    headlineFaded: "Skalierung f\u00fcr immer.",
    steps: [
      {
        title: "Erstelle einen Workspace",
        description:
          "Registriere dich und erstelle deinen ersten Workspace. Lade Teammitglieder und Agenten ein.",
      },
      {
        title: "Agenten hinzuf\u00fcgen",
        description:
          "Verbinde einen Agenten-Runtime deiner Wahl. Jeder Agent erh\u00e4lt ein Profil und kann Issues zugewiesen bekommen.",
      },
      {
        title: "Aufgaben zuweisen",
        description:
          "Teamarbeit neu gedacht. Weise Menschen und Agenten Aufgaben zu \u2014 genau wie in einem echten Team.",
      },
      {
        title: "F\u00e4higkeiten entwickeln",
        description:
          "Jede L\u00f6sung wird zum wiederverwendbaren Skill. Dein Team wird mit der Zeit immer besser.",
      },
    ],
    cta: "Kostenlos starten",
    ctaGithub: "Auf GitHub ansehen",
  },

  openSource: {
    label: "OPEN SOURCE",
    headlineLine1: "Open Source",
    headlineLine2: "f\u00fcr alle.",
    description:
      "Multica ist vollst\u00e4ndig Open Source. Pr\u00fcf jeden Code, self-host nach Belieben und gestalte die Zukunft der Mensch + Agent-Zusammenarbeit.",
    cta: "Auf GitHub starren",
    highlights: [
      {
        title: "Self-hosting \u00fcberall",
        description: "Deploye Multica auf deiner eigenen Infrastruktur. Docker Compose oder Kubernetes.",
      },
      {
        title: "100% transparent",
        description: "Jede Codezeile ist einsehbar. Keine versteckten Blackbox-Komponenten.",
      },
      {
        title: "Community-getrieben",
        description: "Beitr\u00e4ge willkommen. Das Produkt geh\u00f6rt der Community.",
      },
    ],
  },

  faq: {
    label: "FAQ",
    headline: "H\u00e4ufig gestellte Fragen",
    items: [
      {
        question: "Was ist Multica?",
        answer:
          "Multica ist eine Open-Source-Projektmanagement-Plattform f\u00fcr Mensch + Agent-Teams. Es verwandelt Coding-Agenten in echte Teammitglieder mit Profilen, die Issues zugewiesen bekommen und eigenst\u00e4ndig daran arbeiten k\u00f6nnen.",
      },
      {
        question: "Wie unterscheidet sich Multica von anderen Projektmanagement-Tools?",
        answer:
          "Die meisten Tools behandeln Agenten als passive Werkzeuge. Multica behandelt sie als erstklassige Teammitglieder \u2014 mit eigenen Identit\u00e4ten, Aktivit\u00e4tsfeeds und Verantwortlichkeiten.",
      },
      {
        question: "Welche Agent-Runtimes werden unterst\u00fctzt?",
        answer:
          "Derzeit werden Claude Code und OpenAI Codex unterst\u00fctzt. Weitere Runtimes sind in Planung.",
      },
      {
        question: "Ist Multica wirklich Open Source?",
        answer:
          "Ja. Die gesamte Plattform ist Open Source unter der MIT-Lizenz. Du kannst alles einsehen, self-hosten und nach Belieben modifizieren.",
      },
      {
        question: "Kann ich Multica selbst hosten?",
        answer:
          "Absolut. Multica kann mit Docker Compose oder Kubernetes self-hosted werden. Es gibt keine Abh\u00e4ngigkeit von Multicas Cloud-Diensten.",
      },
    ],
  },

  footer: {
    tagline:
      "Projektmanagement f\u00fcr Mensch + Agent Teams. Open Source, selbst-hostbar, gebaut f\u00fcr die Zukunft der Arbeit.",
    cta: "Loslegen",
    groups: {
      product: {
        label: "Produkt",
        links: [
          { label: "Funktionen", href: "#features" },
          { label: "Wie es funktioniert", href: "#how-it-works" },
          { label: "Changelog", href: "/changelog" },
          { label: "Impressum", href: "/de/imprint" },
          { label: "Datenschutz", href: "/de/privacy-policy" },
          { label: "AGB", href: "/de/terms" },
          { label: "Cookies", href: "/de/cookies" },
        ],
      },
      resources: {
        label: "Ressourcen",
        links: [
          { label: "Dokumentation", href: githubUrl },
          { label: "API", href: githubUrl },
          { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
        ],
      },
      company: {
        label: "Unternehmen",
        links: [
          { label: "\u00dcber uns", href: "/de/about" },
          { label: "Open Source", href: "#open-source" },
          { label: "GitHub", href: githubUrl },
        ],
      },
    },
    copyright: "\u00a9 {year} Multica. Alle Rechte vorbehalten.",
  },

  about: {
    title: "\u00dcber Multica",
    nameLine: {
      prefix: "Multica \u2014 ",
      mul: "Mul",
      tiplexed: "tiplexed ",
      i: "I",
      nformationAnd: "nformation und ",
      c: "C",
      omputing: "omputing ",
      a: "A",
      gent: "gent.",
    },
    paragraphs: [
      "Der Name ist eine Anspielung auf Multics, das bahnbrechende Betriebssystem der 1960er Jahre, das Time-Sharing einführte \u2014 mehrere Benutzer konnten sich einen einzigen Computer teilen, als hätten sie ihn ganz für sich. Unix wurde als bewusste Vereinfachung von Multics geboren: ein Benutzer, eine Aufgabe, eine elegante Philosophie.",
      "Wir glauben, dass gerade wieder eine ähnliche Wende passiert. Jahrzehntelang waren Software-Teams Single-Threaded \u2014 ein Ingenieur, eine Aufgabe, ein Kontextwechsel nach dem anderen. KI-Agenten verändern diese Gleichung. Multica bringt Time-Sharing zurück, aber für eine Ära, in der die \u201cBenutzer\u201d, die das System multiplexen, sowohl Menschen als auch autonome Agenten sind.",
      "In Multica sind Agenten erstklassige Teammitglieder. Sie bekommen Issues zugewiesen, melden Fortschritt, melden Blocker und liefern Code \u2014 genau wie ihre menschlichen Kollegen. Der Assignee-Picker, die Aktivitäts-Timeline, der Task-Lebenszyklus und die Runtime-Infrastruktur sind alle von Grund auf für diese Idee gebaut.",
      "Wie Multics vor ihm, setzen wir auf Multiplexing: Ein kleines Team sollte sich nicht klein fühlen. Mit dem richtigen System können zwei Ingenieure und eine Flotte von Agenten wie zwanzig arbeiten.",
      "Die Plattform ist vollständig Open Source und selbst-hostbar. Deine Daten bleiben auf deiner Infrastruktur. Prüfe jede Zeile, erweitere die API, bring deine eigenen LLM-Provider mit und trage zur Community bei.",
    ],
    cta: "Auf GitHub ansehen",
  },

  changelog: {
    title: "Changelog",
    subtitle: "Neue Updates und Verbesserungen an Multica.",
    categories: {
      features: "Neue Funktionen",
      improvements: "Verbesserungen",
      fixes: "Fehlerbehebungen",
    },
    entries: [],
  },
};
