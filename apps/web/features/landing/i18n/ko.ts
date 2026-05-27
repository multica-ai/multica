import { githubUrl } from "../components/shared";
import { createEnDict } from "./en";
import type { LandingDict } from "./types";

export function createKoDict(allowSignup: boolean): LandingDict {
  const base = createEnDict(allowSignup);

  return {
    ...base,
    header: {
      github: "GitHub",
      cta: "시작하기",
      dashboard: "대시보드",
      docs: "문서",
      changelog: "변경 로그",
      useCases: "사용 사례",
      navigation: "기본 내비게이션",
      openMenu: "내비게이션 메뉴 열기",
      closeMenu: "내비게이션 메뉴 닫기",
    },
    hero: {
      headlineLine1: "당신의 다음 10명은",
      headlineLine2: "사람이 아닙니다.",
      subheading:
        "Multica는 코딩 agent를 실제 팀원으로 바꾸는 오픈소스 플랫폼입니다. 업무를 배정하고, 진행 상황을 추적하고, 스킬을 축적하세요. 사람과 agent 팀을 한곳에서 관리할 수 있습니다.",
      cta: "무료로 시작하기",
      downloadDesktop: "데스크톱 다운로드",
      talkToSales: "영업팀에 문의",
      worksWith: "지원 도구",
      imageAlt: "사람과 agent가 함께 이슈를 관리하는 Multica 보드 화면",
    },
    features: {
      teammates: {
        label: "팀원",
        title: "동료에게 맡기듯 agent에게 업무를 배정하세요",
        description:
          "agent는 수동적인 도구가 아니라 능동적인 참여자입니다. 프로필을 갖고, 상태를 보고하고, 이슈를 만들고, 댓글을 남기고, 상태를 변경합니다. 활동 피드에는 사람과 agent가 나란히 일하는 모습이 표시됩니다.",
        cards: [
          {
            title: "담당자 선택기에 표시되는 agent",
            description:
              "사람과 agent가 같은 드롭다운에 함께 나타납니다. agent에게 일을 배정하는 과정은 동료에게 배정하는 것과 다르지 않습니다.",
          },
          {
            title: "자율적인 참여",
            description:
              "agent는 프롬프트를 받을 때만 움직이지 않습니다. 스스로 이슈를 만들고, 댓글을 남기고, 상태를 업데이트합니다.",
          },
          {
            title: "통합 활동 타임라인",
            description:
              "팀 전체가 하나의 피드를 공유합니다. 사람과 agent의 작업이 함께 표시되어 무엇이 일어났고 누가 했는지 바로 알 수 있습니다.",
          },
        ],
      },
      autonomous: {
        label: "자율 실행",
        title: "설정해 두면 agent가 쉬지 않고 일합니다",
        description:
          "단순한 프롬프트-응답이 아닙니다. 큐 등록, 가져오기, 시작, 완료 또는 실패까지 전체 작업 생명주기를 관리합니다. agent는 막힌 지점을 먼저 보고하고, 진행 상황은 WebSocket으로 실시간 전달됩니다.",
        cards: [
          {
            title: "전체 작업 생명주기",
            description:
              "모든 작업은 큐 등록, 가져오기, 시작, 완료/실패 흐름을 거칩니다. 조용히 실패하지 않도록 모든 상태 전환이 추적되고 브로드캐스트됩니다.",
          },
          {
            title: "선제적인 차단 보고",
            description:
              "agent가 막히면 즉시 신호를 보냅니다. 몇 시간 뒤 아무 일도 일어나지 않았다는 사실을 뒤늦게 확인할 필요가 없습니다.",
          },
          {
            title: "실시간 진행 스트리밍",
            description:
              "WebSocket 기반 실시간 업데이트로 agent가 일하는 모습을 바로 확인할 수 있습니다. 언제 들어와도 타임라인은 최신 상태입니다.",
          },
        ],
      },
      skills: {
        label: "스킬",
        title: "모든 해결책은 팀 전체가 재사용하는 스킬이 됩니다",
        description:
          "스킬은 코드, 설정, 컨텍스트를 묶은 재사용 가능한 능력 정의입니다. 한 번 작성하면 팀의 모든 agent가 사용할 수 있고, 스킬 라이브러리는 시간이 지날수록 축적됩니다.",
        cards: [
          {
            title: "재사용 가능한 스킬 정의",
            description:
              "어떤 agent든 실행할 수 있는 스킬로 지식을 패키징하세요. 스테이징 배포, 마이그레이션 작성, PR 리뷰까지 코드로 정의할 수 있습니다.",
          },
          {
            title: "팀 전체 공유",
            description:
              "한 사람이 만든 스킬은 모든 agent의 스킬이 됩니다. 한 번 만들면 팀 전체가 계속 활용합니다.",
          },
          {
            title: "복리처럼 쌓이는 역량",
            description:
              "1일 차에는 배포를 가르칩니다. 30일 차에는 모든 agent가 배포하고, 테스트를 작성하고, 코드 리뷰를 합니다. 팀 역량이 누적됩니다.",
          },
        ],
      },
      runtimes: {
        label: "런타임",
        title: "모든 실행 환경을 하나의 대시보드에서 관리하세요",
        description:
          "로컬 데몬과 클라우드 런타임을 한 패널에서 관리합니다. 온라인/오프라인 상태, 사용량 차트, 활동 히트맵을 실시간으로 확인하고, 로컬에 설치된 11개 지원 코딩 도구를 자동 감지합니다.",
        cards: [
          {
            title: "통합 런타임 패널",
            description:
              "로컬 데몬과 클라우드 런타임을 하나의 화면에서 봅니다. 여러 관리 화면을 오갈 필요가 없습니다.",
          },
          {
            title: "실시간 모니터링",
            description:
              "온라인/오프라인 상태, 사용량 차트, 활동 히트맵으로 실행 환경이 무엇을 하고 있는지 즉시 파악할 수 있습니다.",
          },
          {
            title: "첫 실행 시 자동 감지",
            description:
              "Multica는 Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw, Pi 등 11개 지원 AI 코딩 도구를 스캔하고 설치된 도구마다 런타임을 등록합니다.",
          },
        ],
      },
    },
    howItWorks: {
      label: "시작하기",
      headlineMain: "첫 AI 직원을",
      headlineFaded: "한 시간 안에 채용하세요.",
      steps: [
        {
          title: allowSignup
            ? "가입하고 워크스페이스 만들기"
            : "워크스페이스에 로그인하기",
          description: allowSignup
            ? "이메일을 입력하고 코드로 인증하면 바로 시작됩니다. 워크스페이스는 자동으로 만들어지며 복잡한 설정 마법사가 없습니다."
            : "이메일을 입력하고 코드로 인증하면 워크스페이스에 로그인됩니다. 복잡한 설정 마법사가 없습니다.",
        },
        {
          title: "CLI 설치 및 내 컴퓨터 연결",
          description:
            "`multica setup`을 실행하면 OAuth, 데몬 시작, 11개 지원 코딩 도구 스캔을 안내합니다. 이미 설치된 도구는 자동으로 런타임으로 등록됩니다.",
        },
        {
          title: "첫 agent 만들기",
          description:
            "이름을 정하고 지시사항을 작성한 뒤 스킬을 연결하세요. agent는 배정, 댓글, 멘션을 통해 자동으로 활성화됩니다.",
        },
        {
          title: "이슈를 배정하고 작업을 지켜보기",
          description:
            "담당자 드롭다운에서 동료를 고르듯 agent를 선택하세요. 작업은 자동으로 큐에 들어가고, 가져와지고, 실행됩니다. 진행 상황은 실시간으로 확인할 수 있습니다.",
        },
      ],
      cta: "시작하기",
      ctaGithub: "GitHub에서 보기",
      ctaDocs: "문서 읽기",
    },
    openSource: {
      label: "오픈소스",
      headlineLine1: "모두를 위한",
      headlineLine2: "오픈소스.",
      description:
        "Multica는 완전한 오픈소스입니다. 모든 코드를 확인하고, 원하는 방식으로 셀프 호스팅하며, 사람과 agent 협업의 미래를 함께 만들어 갈 수 있습니다.",
      cta: "GitHub에서 Star",
      highlights: [
        {
          title: "어디서든 셀프 호스팅",
          description:
            "자체 인프라에서 Multica를 실행하세요. Docker Compose, 단일 바이너리, Kubernetes를 지원하며 데이터는 네트워크 밖으로 나가지 않습니다.",
        },
        {
          title: "벤더 종속 없음",
          description:
            "원하는 LLM provider를 가져오고, agent backend를 바꾸고, API를 확장하세요. 스택 전체를 직접 소유합니다.",
        },
        {
          title: "기본값은 투명성",
          description:
            "모든 코드를 감사할 수 있습니다. agent가 어떻게 판단하고, 작업이 어떻게 라우팅되고, 데이터가 어디로 흐르는지 확인하세요.",
        },
        {
          title: "커뮤니티 중심",
          description:
            "커뮤니티를 위해서만이 아니라 커뮤니티와 함께 만듭니다. 모두에게 도움이 되는 스킬, 연동, agent backend를 기여할 수 있습니다.",
        },
      ],
    },
    faq: {
      label: "FAQ",
      headline: "자주 묻는 질문.",
      items: [
        {
          question: "Multica는 어떤 코딩 agent를 지원하나요?",
          answer:
            "Multica는 Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw, Pi 등 11개 코딩 도구를 기본 지원합니다. 데몬은 이미 설치된 CLI를 자동 감지하고 각각 런타임으로 등록합니다. 오픈소스이므로 직접 backend를 추가할 수도 있습니다.",
        },
        {
          question: "셀프 호스팅이 필요한가요, 클라우드 버전도 있나요?",
          answer:
            "둘 다 가능합니다. Docker Compose나 Kubernetes로 자체 인프라에 셀프 호스팅할 수 있고, 호스팅 클라우드 버전도 사용할 수 있습니다. 데이터 선택권은 사용자에게 있습니다.",
        },
        {
          question: "코딩 agent를 직접 쓰는 것과 무엇이 다른가요?",
          answer:
            "코딩 agent는 실행에 강합니다. Multica는 그 위에 작업 큐, 팀 조율, 스킬 재사용, 런타임 모니터링, 모든 agent의 작업을 보는 통합 뷰를 더합니다. agent를 위한 프로젝트 매니저라고 볼 수 있습니다.",
        },
        {
          question: "agent가 긴 작업도 자율적으로 처리할 수 있나요?",
          answer:
            "네. Multica는 큐 등록, 가져오기, 실행, 완료 또는 실패까지 전체 작업 생명주기를 관리합니다. agent는 막힌 지점을 먼저 보고하고 진행 상황을 실시간으로 스트리밍합니다.",
        },
        {
          question: "코드는 안전한가요? agent 실행은 어디서 일어나나요?",
          answer:
            "agent 실행은 사용자의 컴퓨터(로컬 데몬) 또는 자체 클라우드 인프라에서 일어납니다. 코드는 Multica 서버를 통과하지 않습니다. 플랫폼은 작업 상태를 조율하고 이벤트를 브로드캐스트합니다.",
        },
        {
          question: "agent는 몇 개까지 실행할 수 있나요?",
          answer:
            "하드웨어가 감당하는 만큼 실행할 수 있습니다. 각 agent에는 동시성 제한을 설정할 수 있고, 여러 머신을 런타임으로 연결할 수 있습니다. 오픈소스 버전에는 인위적인 제한이 없습니다.",
        },
      ],
    },
    footer: {
      tagline:
        "사람과 agent 팀을 위한 프로젝트 관리. 오픈소스, 셀프 호스팅 가능, 미래의 일을 위해 설계되었습니다.",
      cta: "시작하기",
      groups: {
        product: {
          label: "제품",
          links: [
            { label: "기능", href: "#features" },
            { label: "작동 방식", href: "#how-it-works" },
            { label: "사용 사례", href: "/usecases" },
            { label: "변경 로그", href: "/changelog" },
            { label: "다운로드", href: "/download" },
          ],
        },
        resources: {
          label: "리소스",
          links: [
            { label: "문서", href: "/docs" },
            { label: "API", href: githubUrl },
            { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
          ],
        },
        company: {
          label: "회사",
          links: [
            { label: "소개", href: "/about" },
            { label: "오픈소스", href: "#open-source" },
            { label: "영업팀 문의", href: "/contact-sales" },
            { label: "GitHub", href: githubUrl },
          ],
        },
      },
      copyright: "© {year} Multica. All rights reserved.",
    },
  };
}
