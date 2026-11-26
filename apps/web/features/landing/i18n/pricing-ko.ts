import type { PricingDict } from "./types";

export const koPricingCopy: PricingDict = {
  eyebrow: "요금",
  title: "팀원 수만큼만 내고, 에이전트는 마음껏 쓰세요.",
  lead: "모든 요금제에서 내 런타임의 에이전트는 무제한이고, 직접 시작한 실행은 집계하지 않습니다. Claude Code, Codex 등 이미 쓰고 있는 코딩 에이전트를 그대로 연결하세요.",
  launchOffer: { badge: "출시 혜택", text: "{date}까지 연간 결제 시 첫해 30% 할인" },
  billing: { label: "결제 주기", yearly: "연간", monthly: "월간", save: "20% 절약" },
  recommended: "추천",
  soon: "곧 출시",
  included: "포함",
  notIncluded: "미포함",
  yearlyNote: "월간 결제 시 {price}",
  monthlyNote: "연간 결제 시 월 {price}",
  firstYear: "첫해 {price}",
  plans: {
    free: {
      name: "Free",
      audience: "개인과 이제 시작하는 작은 팀을 위한 요금제.",
      cadence: "평생 무료",
      cta: "무료로 시작하기",
      ctaNote: "카드 등록 필요 없음",
      includes: "포함 항목",
      features: [
        { label: "멤버 최대 5명" },
        { label: "에이전트, 런타임, 스쿼드, 스킬 무제한" },
        { label: "이슈, 프로젝트, 실행 기록 무제한" },
        { label: "오토파일럿 실행 월 200회" },
        { label: "파일 저장 공간 1 GB" },
        { label: "사용량 대시보드 30일" },
      ],
    },
    pro: {
      name: "Pro",
      audience: "매일 배포하는 팀을 위해 제한을 없앴습니다.",
      cadence: "멤버당 / 월",
      cta: "시작하기",
      ctaNote: "언제든 해지 가능",
      includes: "Free의 모든 기능, 그리고",
      features: [
        { label: "멤버 무제한" },
        { label: "좌석을 쓰지 않는 무료 게스트" },
        { label: "오토파일럿 실행 멤버당 월 1,000회" },
        { label: "멤버당 10 GB 저장 공간" },
        { label: "사용량 대시보드 90일" },
        { label: "Multica Cloud 크레딧", soon: true },
      ],
    },
    business: {
      name: "Business",
      audience: "팀이 커져도 누가 무엇을 보고 쓰는지 관리하세요.",
      cadence: "멤버당 / 월",
      cta: "14일 무료 체험",
      ctaNote: "카드 등록 필요 없음 ·",
      ctaNoteLink: "영업팀과 상담",
      includes: "Pro의 모든 기능, 그리고",
      features: [
        { label: "비공개 프로젝트" },
        { label: "멤버나 팀별로 에이전트와 런타임 공유" },
        { label: "팀 앱 계정과 승인 규칙" },
        { label: "Google 로그인 강제와 도메인 자동 참여" },
        { label: "멤버별 사용량과 비용 보고서" },
        { label: "오토파일럿 실행 멤버당 월 3,000회" },
        { label: "멤버당 100 GB 저장 공간" },
        { label: "사용량 대시보드 365일" },
      ],
    },
    enterprise: {
      name: "Enterprise",
      audience: "보안, 컴플라이언스, 셀프 호스팅이 필요할 때.",
      cadence: "연간 계약",
      cta: "영업팀에 문의",
      ctaNote: "Multica Cloud 또는 셀프 호스팅",
      includes: "Business의 모든 기능, 그리고",
      features: [
        { label: "SAML SSO와 SCIM" },
        { label: "감사 로그" },
        { label: "데이터 보존 및 삭제 정책" },
        { label: "IP 허용 목록" },
        { label: "맞춤 브랜딩과 도메인" },
        { label: "셀프 호스팅 배포" },
        { label: "청구서 결제와 전담 지원" },
      ],
    },
  },
  customPrice: "맞춤 견적",
  cloudCredits: {
    title: "Multica Cloud 크레딧",
    body: "내 컴퓨터 없이도 호스팅된 런타임과 모델에서 에이전트를 실행하세요. 크레딧은 원가에 가까운 가격이고 워크스페이스 안에서 공유되며, 셀프 호스팅 인스턴스에서도 구매할 수 있게 됩니다.",
  },
  selfHost: {
    title: "Multica를 직접 호스팅하시나요?",
    body: "커뮤니티 에디션은 조직 내부에서 무료로 쓸 수 있습니다. Enterprise는 SSO, 감사 로그, 맞춤 브랜딩, 셀프 호스팅 배포 지원을 더합니다.",
    contactSales: "영업팀에 문의",
    licensing: "라이선스",
  },
  compare: {
    title: "요금제 비교",
    lead: "에이전트는 모든 요금제에서 무제한입니다. Pro는 제한을 없애고, Business는 접근 권한과 관리 기능을 더합니다.",
    groups: [
      {
        title: "팀",
        rows: [
          { label: "멤버", cells: ["최대 5명", "무제한", "무제한", "무제한"] },
          { label: "게스트", hint: "보기와 댓글만, 무료, 좌석 미사용", cells: [false, true, true, true] },
          { label: "이슈, 프로젝트, 실행 기록", cells: ["무제한", "무제한", "무제한", "무제한"] },
          { label: "파일 저장 공간", cells: ["1 GB", "멤버당 10 GB", "멤버당 100 GB", "맞춤 견적"] },
        ],
      },
      {
        title: "에이전트와 자동화",
        rows: [
          { label: "에이전트, 런타임, 스쿼드, 스킬", cells: ["무제한", "무제한", "무제한", "무제한"] },
          { label: "직접 시작한 실행", cells: ["무제한", "무제한", "무제한", "무제한"] },
          {
            label: "오토파일럿 실행",
            hint: "워크스페이스 안에서 공유",
            cells: ["월 200회", "멤버당 월 1,000회", "멤버당 월 3,000회", "맞춤 견적"],
          },
          { label: "오토파일럿 추가 실행", cells: [false, "1,000회당 $10", "1,000회당 $10", "맞춤 견적"] },
          { label: "Multica Cloud 크레딧", soon: true, cells: [false, "포함", "더 많이 포함", "맞춤 견적"] },
        ],
      },
      {
        title: "분석",
        rows: [
          { label: "사용량 대시보드 기간", cells: ["30일", "90일", "365일", "맞춤 견적"] },
          { label: "멤버별 사용량과 비용, CSV 내보내기", cells: [false, false, true, true] },
        ],
      },
      {
        title: "접근 권한과 관리",
        rows: [
          { label: "비공개 프로젝트", cells: [false, false, true, true] },
          { label: "특정 멤버나 팀에 에이전트 공유", cells: [false, false, true, true] },
          { label: "특정 멤버에게 런타임 공유", cells: [false, false, true, true] },
          { label: "팀 앱 계정과 승인 규칙", cells: [false, false, true, true] },
        ],
      },
      {
        title: "보안과 관리자",
        rows: [
          { label: "관리자 역할", cells: [true, true, true, true] },
          { label: "Google 로그인 강제", cells: [false, false, true, true] },
          { label: "도메인 자동 참여", cells: [false, false, true, true] },
          { label: "SAML SSO와 SCIM", cells: [false, false, false, true] },
          { label: "감사 로그", cells: [false, false, false, true] },
          { label: "데이터 보존·삭제와 IP 허용 목록", cells: [false, false, false, true] },
          { label: "맞춤 브랜딩과 도메인", cells: [false, false, false, true] },
          { label: "셀프 호스팅 배포", cells: [false, false, false, true] },
        ],
      },
      {
        title: "결제와 지원",
        rows: [
          { label: "결제 방법", cells: ["—", "카드", "카드", "청구서와 구매 주문서"] },
          { label: "지원", cells: ["커뮤니티", "이메일", "우선 이메일", "전담 지원"] },
        ],
      },
    ],
  },
  faq: {
    title: "자주 묻는 질문",
    items: [
      {
        question: "에이전트도 멤버로 계산되나요?",
        answer: "아니요. 팀원 수만큼만 결제합니다. 에이전트, 런타임, 스쿼드, 스킬은 모든 요금제에서 무제한입니다.",
      },
      {
        question: "오토파일럿 실행은 무엇을 말하나요?",
        answer:
          "오토파일럿이 일정이나 트리거로 시작하는 실행 한 번 한 번입니다. 이슈 배정, 채팅, 에이전트 멘션처럼 직접 시작한 실행은 집계하지 않습니다. 실행 횟수는 워크스페이스 안에서 공유되며, Pro와 Business에서는 1,000회를 $10에 추가할 수 있습니다.",
      },
      {
        question: "게스트는 누구인가요?",
        answer:
          "진행 상황만 따라가면 되는 사람입니다. 게스트는 공유받은 프로젝트를 보고 댓글을 달 수 있으며, 좌석을 쓰지 않고 Pro와 Business에서 무료입니다.",
      },
      {
        question: "체험이 끝나면 어떻게 되나요?",
        answer:
          "새 워크스페이스는 카드 등록 없이 Business를 14일 동안 쓸 수 있습니다. 체험이 끝나면 요금제를 고르거나 Free로 전환합니다. 데이터는 삭제되지 않고, 이미 설정한 Business 기능은 그대로 유지되며 업그레이드 후 다시 바꿀 수 있습니다.",
      },
      {
        question: "출시 혜택은 어떻게 받나요?",
        answer: "{date}까지 Pro나 Business를 연간 결제로 구독하면 첫해 30% 할인됩니다. 갱신은 정상 연간 요금으로 진행됩니다.",
      },
      {
        question: "Multica를 직접 호스팅할 수 있나요?",
        answer:
          "네. 커뮤니티 에디션은 조직 내부에서 무료로 쓸 수 있습니다. Enterprise는 SSO, 감사 로그, 맞춤 브랜딩, 셀프 호스팅 배포 지원을 더합니다.",
      },
    ],
  },
};
