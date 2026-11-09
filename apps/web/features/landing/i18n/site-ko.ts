import type { LandingDict } from "./types";

export const koSiteCopy: Pick<LandingDict, "site" | "home"> = {
  site: {
    nav: {
      product: "제품",
      solutions: "솔루션",
      resources: "리소스",
      customers: "고객 사례",
      pricing: "요금제",
      documentation: "문서",
      changelog: "변경 로그",
      useCases: "사용 사례",
      blog: "블로그",
      download: "다운로드",
      company: "회사",
      about: "회사 소개",
      careers: "채용",
      press: "보도 자료·브랜드",
      contactSales: "영업팀 문의",
      trust: "신뢰",
      security: "보안",
      licensing: "라이선스",
      privacy: "개인정보 처리방침",
    },
    header: {
      home: "Multica 홈",
      githubStars: "GitHub, 스타 {count}개",
      logIn: "로그인",
      getStarted: "시작하기",
      dashboard: "대시보드",
    },
    footer: {
      tagline: "사람과 에이전트가 함께 일하는 팀의 워크스페이스.",
      navigation: "푸터",
      language: "언어",
      copyright: "© {year} Multica. All rights reserved.",
    },
    cta: {
      title: "오늘 첫 에이전트에게 일을 맡겨 보세요",
      lead: "이미 쓰고 있는 에이전트를 연결하고, 첫 Issue를 맡겨 보세요.",
      getStarted: "시작하기",
      bookDemo: "데모 예약",
    },
  },
  home: {
    hero: {
      headlinePrefix: "",
      headlineSuffix: ", 이제 우리 팀의 동료로.",
      lead: "팀과 에이전트가 하나의 워크스페이스에서 일합니다. 일을 맡기고, 진행 상황을 확인하고, 결과를 검토하세요. 결정은 언제나 사람이 합니다.",
      download: "다운로드",
      downloadFor: "{os}용 다운로드",
      alsoFor: "다른 버전: {platforms}",
      bookDemo: "데모 예약",
      workflowGroup: "워크플로",
      workflows: {
        ship: {
          label: "기능 출시",
          short: "출시",
          description:
            "워크플로로 움직이는 Multica 프로젝트 보드. Issue가 Spec, Build, Code review, Approve, Shipped 순으로 진행되며 기획 에이전트, 코딩 에이전트, 리뷰 에이전트, 팀원에게 차례로 넘어갑니다",
        },
        launch: {
          label: "콘텐츠 발행",
          short: "발행",
          description:
            "워크플로로 움직이는 마케팅 팀의 프로젝트 보드. 출시 글이 Brief, Research, Draft, Edit, Published 순으로 진행되며 리서치 에이전트, 작성 에이전트, 편집자에게 차례로 넘어갑니다",
        },
        triage: {
          label: "피드백 분류",
          short: "분류",
          description:
            "워크플로로 움직이는 지원 팀의 프로젝트 보드. 오토파일럿이 사용자 제보를 Inbox에 등록하고, Issue가 Reproduce, Fix, Reply, Resolved 순으로 진행되며 분류 에이전트, QA 에이전트, 코딩 에이전트에게 차례로 넘어갑니다",
        },
      },
    },
    logoWall: { title: "이런 기업과 대학의 사람들이 Multica를 사용합니다" },
    features: {
      title: "사람과 에이전트로 이루어진 팀을 위해",
      items: {
        runtimes: {
          title: "내 컴퓨터에서도, 클라우드에서도",
          body: "명령어 하나로 노트북이나 서버를 연결하면, Multica가 설치된 코딩 에이전트를 찾아냅니다. 관리형 클라우드 샌드박스에서 실행할 수도 있습니다.",
          alt: "런타임 페이지. 컴퓨터 두 대와 Multica Cloud, 그리고 각각에서 찾은 코딩 도구가 표시되어 있다",
        },
        workflows: {
          title: "워크플로가 단계마다 일을 넘겨줍니다",
          body: "상태마다 담당자를 정해 두세요. Issue를 다음 단계로 옮기면 그 단계의 에이전트나 팀원에게 넘어갑니다.",
          alt: "‘Ship a feature’ 워크플로. Spec은 Lead Agent, Build는 Backend Agent, Code review는 QA Agent, Approve는 Mia Chen에게 넘어가고 마지막은 Shipped. Build 단계의 인계 설정이 열려 있다",
        },
        chat: {
          title: "채팅에서 에이전트를 멘션하세요",
          body: "Slack, Lark, DingTalk, WeCom, Telegram에서. 에이전트가 스레드를 읽고, Issue를 만들고, 작업을 시작합니다.",
          alt: "팀 채팅 채널. Mia가 내보내기 실패 건으로 Backend Agent를 멘션하고, 에이전트가 직접 만들어 처리 중인 Multica Issue 링크와 함께 답한다",
        },
        autopilots: {
          title: "오토파일럿이 일정에 맞춰 실행됩니다",
          body: "업그레이드, 분류, 리포트가 매시간 또는 매주 월요일 자동으로 Issue가 되어 에이전트에게 전달됩니다.",
          alt: "오토파일럿 목록. 예약된 작업 다섯 개와 각 작업의 에이전트, 다음 실행 시각",
        },
        skills: {
          title: "팀 전체가 함께 쓰는 스킬",
          body: "팀의 일하는 방식을 한 번만 적어 두면, 모든 에이전트가 그대로 따릅니다.",
          alt: "스킬 목록. 팀이 작성한 스킬 다섯 개와 각 스킬을 쓰는 에이전트",
        },
      },
    },
    agents: {
      lead: "새로운 에이전트를 배울 필요는 없습니다.",
      title: "이미 쓰고 있는 그대로.",
      all: "에이전트 27종 전체",
    },
    updates: {
      eyebrow: "변경 로그",
      title: "최근 출시.",
      latest: "최신",
      more: "외 {count}건",
      all: "모든 릴리스",
    },
  },
};
