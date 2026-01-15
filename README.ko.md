# Multica

**Multiplexed Information and Computing Agent**

시각적 인터페이스를 통해 코딩 에이전트의 기능을 모든 사람에게 제공하는 네이티브 데스크톱 클라이언트.

[English](./README.md) | [简体中文](./README.zh-CN.md) | [繁體中文](./README.zh-TW.md) | [日本語](./README.ja.md) | 한국어

## 왜 "Multica"인가?

이 이름은 1964년에 만들어진 선구적인 운영 체제인 [Multics](https://en.wikipedia.org/wiki/Multics)(Multiplexed Information and Computing Service)에서 영감을 받았습니다. Multics는 널리 보급되지는 않았지만, 계층적 파일 시스템과 같은 현대 운영 체제의 기반을 마련했습니다. Unix 자체도 Multics에서 파생되었습니다(Uniplexed Information and Computing Service -> Unics -> Unix).

**메타포:** Multics가 다중 사용자 시분할 컴퓨팅 자원 문제를 해결하기 위해 만들어진 것처럼, Multica는 지식 근로자를 위한 다중 모델/다중 에이전트 협업 문제를 해결하기 위해 설계되었습니다.

## 해결하는 문제

코딩 에이전트(Claude Code, Codex, Gemini CLI 등)는 2025년에 매우 강력해져서 단순한 코드 작성을 훨씬 넘어서는 복잡한 작업을 해결할 수 있게 되었습니다. 그러나 95%의 지식 근로자들은 세 가지 핵심 장벽으로 인해 이러한 기능을 활용하지 못하고 있습니다:

**1. 상호작용 불일치**
- CLI 기반 도구는 터미널 개념, 파일 경로, 환경 변수에 대한 이해를 요구함
- 현재 도구들은 비즈니스 결과가 아닌 코드 출력(diff, 커밋, 린팅)에 초점을 맞춤
- 지식 근로자들이 관심 있는 것은 결과(차트, 보고서, 분석)이지, 그것을 생성하는 스크립트가 아님

**2. 로컬 환경 문제**
- 웹 기반 에이전트는 로컬 파일, 폴더 또는 네이티브 애플리케이션에 접근할 수 없음
- Python, Node.js 또는 기타 종속성 설정은 상당한 장벽임
- 모든 종속성을 처리하는 "바로 사용 가능한" 샌드박스 환경이 없음

**3. 개인정보 보호 및 신뢰**
- 민감한 비즈니스 데이터(재무 분석, 법률 문서, 의료 기록)는 타사 서버에 업로드할 수 없음
- 데이터는 로컬에 유지하고 인텔리전스는 클라우드에서 가져오는 모델이 필요함

Multica는 데이터를 로컬에 유지하면서 코딩 에이전트의 기능을 활용하는 시각적 네이티브 데스크톱 인터페이스를 제공하여 이 격차를 해소합니다.

## 기능

- 깔끔하고 직관적인 인터페이스를 갖춘 네이티브 macOS 애플리케이션
- [Agent Client Protocol (ACP)](https://github.com/anthropics/agent-client-protocol)를 통한 여러 AI 에이전트 지원
- 로컬 우선: 데이터가 절대 기기를 떠나지 않음
- 기록 및 재개 기능이 있는 세션 관리
- 파워 유저 및 테스트를 위한 내장 CLI

## 지원되는 에이전트

| 에이전트 | 명령어 | 설치 |
|-------|---------|---------|
| [OpenCode](https://github.com/opencode-ai/opencode) | `opencode acp` | `go install github.com/opencode-ai/opencode@latest` |
| [Codex CLI (ACP)](https://github.com/zed-industries/codex-acp) | `codex-acp` | `npm install -g codex-acp` |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | `gemini acp` | `npm install -g @google/gemini-cli` |

## 빠른 시작

```bash
# 종속성 설치
pnpm install

# 설치된 에이전트 확인
pnpm cli doctor

# 데스크톱 앱 시작
pnpm dev
```

## CLI

Multica에는 에이전트 테스트 및 상호작용을 위한 포괄적인 CLI가 포함되어 있습니다:

```bash
pnpm cli                          # 대화형 모드
pnpm cli prompt "메시지"           # 단일 프롬프트
pnpm cli sessions                 # 세션 목록
pnpm cli resume <id>              # 세션 재개
pnpm cli agents                   # 사용 가능한 에이전트 목록
pnpm cli doctor                   # 에이전트 설치 확인
```

### 대화형 모드

대화형 REPL 세션 시작:

```bash
pnpm cli
```

사용 가능한 명령어:

| 명령어 | 설명 |
|---------|-------------|
| `/help` | 도움말 표시 |
| `/new [cwd]` | 새 세션 생성 (기본값: 현재 디렉토리) |
| `/sessions` | 모든 세션 나열 |
| `/resume <id>` | ID 접두사로 세션 재개 |
| `/delete <id>` | 세션 삭제 |
| `/history` | 현재 세션의 메시지 기록 표시 |
| `/agent <name>` | 다른 에이전트로 전환 |
| `/agents` | 사용 가능한 에이전트 나열 |
| `/doctor` | 에이전트 설치 확인 |
| `/status` | 현재 상태 표시 |
| `/cancel` | 현재 요청 취소 |
| `/quit` | CLI 종료 |

### 단일 프롬프트

단일 프롬프트를 보내고 종료:

```bash
pnpm cli prompt "2+2는 무엇인가요?"
pnpm cli prompt "파일 목록" --cwd=/tmp
```

### 옵션

| 옵션 | 설명 |
|--------|-------------|
| `--cwd=PATH` | 에이전트의 작업 디렉토리 |
| `--log` | 세션 로그를 `logs/` 디렉토리에 저장 |
| `--log=PATH` | 세션 로그를 지정된 파일에 저장 |

## 개발

```bash
# 개발 모드로 Electron 앱 시작
pnpm dev

# 타입 체크
pnpm typecheck

# 테스트 실행
pnpm test
```

## 빌드

```bash
pnpm build:mac      # macOS
pnpm build:win      # Windows
pnpm build:linux    # Linux
```

## 아키텍처

```
Multica (Electron)
+-- 렌더러 프로세스 (React)
|   +-- UI 컴포넌트 (채팅, 설정 등)
|
+-- 메인 프로세스
|   +-- Conductor (에이전트 통신 조율)
|   |   +-- SessionStore (세션 영속성)
|   |   +-- ClientSideConnection (ACP SDK)
|   |         +-- AgentProcess (서브프로세스 관리)
|   |               +-- opencode/codex-acp/gemini (stdio)
|   |
|   +-- IPC 핸들러 (세션, 에이전트, 설정)
|
+-- Preload (contextBridge)
    +-- electronAPI (렌더러에 노출)
```

### 세션 관리

Multica는 ACP 위에 자체 세션 레이어를 유지합니다:

```
~/.multica/sessions/
+-- index.json              # 세션 목록 (빠른 로드)
+-- data/
    +-- {session-id}.json   # 전체 세션 데이터 + 업데이트
```

**주요 설계 결정:**
- **클라이언트 측 저장소**: Multica는 UI 표시를 위해 원시 `session/update` 데이터를 저장
- **에이전트 독립적**: 각 에이전트가 자체 내부 상태를 별도로 관리
- **재개 동작**: 새 ACP 세션을 생성하고 저장된 기록을 UI에 표시

## 라이선스

MIT
