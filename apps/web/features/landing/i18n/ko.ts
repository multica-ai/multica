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
      navigation: "주요 메뉴",
      openMenu: "메뉴 열기",
      closeMenu: "메뉴 닫기",
    },
    hero: {
      headlineLine1: "다음에 합류할 10명은",
      headlineLine2: "사람이 아닐 수 있습니다.",
      subheading:
        "Multica는 코딩 AI 에이전트를 실제 팀원처럼 운영하는 오픈소스 플랫폼입니다. 이슈를 맡기고, 진행 상황을 확인하고, 반복되는 노하우를 스킬로 쌓아 두세요. 사람과 AI 에이전트가 함께 일하는 팀을 한곳에서 관리할 수 있습니다.",
      cta: "무료로 시작하기",
      downloadDesktop: "데스크톱 다운로드",
      talkToSales: "영업팀에 문의",
      worksWith: "지원 도구",
      imageAlt: "사람과 AI 에이전트가 함께 이슈를 관리하는 Multica 보드 화면",
    },
    features: {
      teammates: {
        label: "팀메이트",
        title: "동료에게 맡기듯 AI 에이전트에게 이슈를 맡기세요",
        description:
          "AI 에이전트는 프롬프트를 기다리는 도구에 머물지 않습니다. 프로필을 갖고, 진행 상황을 남기고, 이슈를 만들고, 댓글을 달고, 상태를 바꿉니다. 활동 피드에서는 사람과 AI 에이전트의 작업 흐름이 함께 보입니다.",
        cards: [
          {
            title: "담당자 목록에 함께 표시",
            description:
              "사람과 AI 에이전트가 같은 담당자 목록에 나타납니다. 동료를 고르듯 에이전트를 선택하면 바로 작업이 시작됩니다.",
          },
          {
            title: "스스로 남기는 작업 기록",
            description:
              "AI 에이전트는 요청을 처리하는 동안 댓글을 남기고 상태를 업데이트합니다. 필요한 경우 새 이슈를 만들어 다음 작업까지 이어갑니다.",
          },
          {
            title: "팀 전체가 보는 타임라인",
            description:
              "사람과 AI 에이전트의 활동이 하나의 피드에 쌓입니다. 누가 무엇을 했고 어디까지 왔는지 따로 물어볼 필요가 없습니다.",
          },
        ],
      },
      autonomous: {
        label: "자율 실행",
        title: "맡겨 두면, 에이전트가 끝까지 실행합니다",
        description:
          "한 번 답하고 끝나는 프롬프트 도구가 아닙니다. 작업 대기열에 넣고, 가져가고, 실행하고, 완료하거나 실패를 보고하는 흐름까지 Multica가 관리합니다. 막힌 부분은 에이전트가 먼저 알리고, 진행 상황은 실시간으로 올라옵니다.",
        cards: [
          {
            title: "처음부터 끝까지 추적",
            description:
              "대기, 수락, 실행, 완료, 실패까지 모든 단계가 기록됩니다. 조용히 멈춰 버린 작업을 뒤늦게 발견하는 일이 줄어듭니다.",
          },
          {
            title: "막히면 먼저 알려줌",
            description:
              "에이전트가 더 진행하기 어렵다고 판단하면 바로 신호를 보냅니다. 몇 시간 뒤에야 아무 일도 없었다는 사실을 확인하지 않아도 됩니다.",
          },
          {
            title: "실시간 진행 상황",
            description:
              "WebSocket 기반 업데이트로 작업 상황이 바로 반영됩니다. 실시간으로 지켜봐도 되고, 나중에 들어와 최신 타임라인만 확인해도 됩니다.",
          },
        ],
      },
      skills: {
        label: "스킬",
        title: "한 번 해결한 일은 팀의 스킬로 남습니다",
        description:
          "스킬은 코드, 설정, 컨텍스트를 묶어 둔 재사용 가능한 작업 방식입니다. 한 번 정리해 두면 팀의 모든 AI 에이전트가 같은 방식으로 실행할 수 있고, 팀의 노하우는 시간이 지날수록 쌓입니다.",
        cards: [
          {
            title: "반복 작업을 스킬로 정리",
            description:
              "스테이징 배포, 마이그레이션 작성, PR 리뷰처럼 자주 반복되는 일을 스킬로 만들어 두세요. 어떤 에이전트든 같은 기준으로 실행할 수 있습니다.",
          },
          {
            title: "팀 전체 공유",
            description:
              "한 사람이 만든 스킬은 모든 AI 에이전트가 사용할 수 있습니다. 개인의 시행착오가 팀 전체의 실행 방식으로 바뀝니다.",
          },
          {
            title: "쌓일수록 빨라지는 팀",
            description:
              "처음에는 배포 하나를 맡깁니다. 시간이 지나면 에이전트가 배포하고, 테스트를 쓰고, 리뷰까지 돕습니다. 팀이 일하는 속도가 점점 빨라집니다.",
          },
        ],
      },
      runtimes: {
        label: "런타임",
        title: "실행 환경을 한곳에서 관리하세요",
        description:
          "로컬 데몬과 클라우드 런타임을 한 화면에서 관리합니다. 온라인 상태, 사용량, 활동 패턴을 확인하고, 내 컴퓨터에 설치된 지원 코딩 도구를 자동으로 찾아 등록합니다.",
        cards: [
          {
            title: "하나로 모은 런타임 패널",
            description:
              "로컬 데몬과 클라우드 런타임을 같은 화면에서 봅니다. 실행 환경마다 다른 관리 페이지를 열 필요가 없습니다.",
          },
          {
            title: "실시간 모니터링",
            description:
              "온라인/오프라인 상태, 사용량 차트, 활동 히트맵으로 각 실행 환경이 어떻게 쓰이고 있는지 바로 확인할 수 있습니다.",
          },
          {
            title: "처음 실행할 때 자동 등록",
            description:
              "Multica는 Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw, Pi 등 지원 도구를 스캔하고, 설치된 도구를 런타임으로 등록합니다.",
          },
        ],
      },
    },
    howItWorks: {
      label: "시작하기",
      headlineMain: "첫 AI 팀원을",
      headlineFaded: "한 시간 안에 합류시키세요.",
      steps: [
        {
          title: allowSignup
            ? "가입하고 워크스페이스 만들기"
            : "워크스페이스에 로그인하기",
          description: allowSignup
            ? "이메일을 입력하고 인증 코드를 확인하면 바로 시작할 수 있습니다. 워크스페이스는 자동으로 만들어지고, 복잡한 설정 과정은 없습니다."
            : "이메일을 입력하고 인증 코드를 확인하면 워크스페이스에 들어갈 수 있습니다. 복잡한 설정 과정은 없습니다.",
        },
        {
          title: "CLI 설치 및 내 컴퓨터 연결",
          description:
            "`multica setup`을 실행하면 로그인, 데몬 실행, 지원 코딩 도구 스캔까지 차례대로 안내합니다. 이미 설치된 도구는 자동으로 런타임에 등록됩니다.",
        },
        {
          title: "첫 에이전트 만들기",
          description:
            "이름을 정하고 지시사항을 작성한 뒤 필요한 스킬을 연결하세요. 이슈를 배정하거나 댓글에서 멘션하면 에이전트가 바로 움직입니다.",
        },
        {
          title: "이슈를 배정하고 작업을 지켜보기",
          description:
            "담당자 목록에서 동료를 고르듯 에이전트를 선택하세요. 작업은 대기열에 들어가고, 에이전트가 가져가 실행합니다. 진행 상황은 실시간으로 확인할 수 있습니다.",
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
        "Multica는 완전한 오픈소스입니다. 코드를 직접 확인하고, 원하는 환경에 셀프 호스팅하고, 사람과 AI 에이전트가 함께 일하는 방식을 함께 만들어 갈 수 있습니다.",
      cta: "GitHub에서 스타하기",
      highlights: [
        {
          title: "어디서든 셀프 호스팅",
          description:
            "자체 인프라에서 Multica를 실행하세요. Docker Compose, 단일 바이너리, Kubernetes를 지원하며 데이터는 여러분의 네트워크 안에 머뭅니다.",
        },
        {
          title: "벤더 종속 없음",
          description:
            "원하는 LLM 제공자를 쓰고, 에이전트 백엔드를 바꾸고, API를 확장하세요. 스택 전체를 직접 통제할 수 있습니다.",
        },
        {
          title: "투명한 구조",
          description:
            "모든 코드를 열어 볼 수 있습니다. 에이전트가 어떻게 동작하고, 작업이 어디로 전달되고, 데이터가 어디를 거치는지 확인할 수 있습니다.",
        },
        {
          title: "커뮤니티 중심",
          description:
            "커뮤니티를 위해 만드는 데서 그치지 않고, 커뮤니티와 함께 만듭니다. 모두에게 도움이 되는 스킬, 연동, 에이전트 백엔드를 기여할 수 있습니다.",
        },
      ],
    },
    faq: {
      label: "FAQ",
      headline: "자주 묻는 질문.",
      items: [
        {
          question: "Multica는 어떤 코딩 에이전트를 지원하나요?",
          answer:
            "Multica는 Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw, Pi 등 11개 코딩 도구를 기본 지원합니다. 데몬은 이미 설치된 CLI를 자동으로 찾아 각각 런타임으로 등록합니다. 오픈소스이므로 직접 백엔드를 추가할 수도 있습니다.",
        },
        {
          question: "셀프 호스팅이 필요한가요, 클라우드 버전도 있나요?",
          answer:
            "둘 다 가능합니다. Docker Compose나 Kubernetes로 자체 인프라에 셀프 호스팅할 수 있고, Multica가 운영하는 클라우드 버전도 사용할 수 있습니다. 어떤 방식으로 데이터를 둘지는 사용자가 선택합니다.",
        },
        {
          question: "코딩 에이전트를 직접 쓰는 것과 무엇이 다른가요?",
          answer:
            "코딩 에이전트는 실행에 강합니다. Multica는 그 위에 작업 대기열, 팀 협업, 스킬 재사용, 런타임 모니터링, 에이전트별 작업 현황을 보는 통합 화면을 더합니다. 에이전트를 팀 안에서 운영하기 위한 관리 계층입니다.",
        },
        {
          question: "에이전트가 긴 작업도 자율적으로 처리할 수 있나요?",
          answer:
            "네. Multica는 대기열 등록, 수락, 실행, 완료 또는 실패까지 작업 흐름을 관리합니다. 에이전트는 막힌 부분을 먼저 알리고, 진행 상황은 실시간으로 기록됩니다.",
        },
        {
          question: "코드는 안전한가요? 에이전트는 어디서 실행되나요?",
          answer:
            "에이전트 실행은 사용자의 컴퓨터에 있는 로컬 데몬, 또는 직접 운영하는 클라우드 인프라에서 일어납니다. 코드는 Multica 서버를 거치지 않습니다. Multica는 작업 상태를 조율하고 이벤트를 전달합니다.",
        },
        {
          question: "에이전트는 몇 개까지 실행할 수 있나요?",
          answer:
            "하드웨어가 감당하는 만큼 실행할 수 있습니다. 에이전트마다 동시 실행 수를 조절할 수 있고, 여러 머신을 런타임으로 연결할 수도 있습니다. 오픈소스 버전에는 인위적인 제한이 없습니다.",
        },
      ],
    },
    footer: {
      tagline:
        "사람과 AI 에이전트가 함께 일하는 팀을 위한 프로젝트 관리. 오픈소스이며 셀프 호스팅할 수 있습니다.",
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
      copyright: "© {year} Multica. 모든 권리 보유.",
    },
  };
}
